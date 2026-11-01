//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func postNotebook(t *testing.T, w *worker, path, name string, payload any) (int, string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	q := url.Values{}
	q.Set("name", name)
	full := path
	if strings.Contains(path, "?") {
		full += "&" + q.Encode()
	} else {
		full += "?" + q.Encode()
	}
	resp, respBody := w.Do(t, http.MethodPost, full, http.Header{
		"Content-Type": []string{"application/json"},
	}, bytes.NewReader(body))
	return resp.StatusCode, respBody
}

func postNotebookOK(t *testing.T, w *worker, path, name string, payload any, dest any) {
	t.Helper()
	status, body := postNotebook(t, w, path, name, payload)
	if status != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d (body = %q)", path, status, http.StatusOK, body)
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal([]byte(body), dest); err != nil {
		t.Fatalf("unmarshal %s response %q: %v", path, body, err)
	}
}

type sqlPayload struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

func blobArgJSON(b []byte) map[string]string {
	return map[string]string{"base64": base64.StdEncoding.EncodeToString(b)}
}

func boolArgJSON(v bool) map[string]bool {
	return map[string]bool{"bool": v}
}

type doSQLResponse struct {
	Columns      []string         `json:"columns"`
	Rows         []map[string]any `json:"rows"`
	Row          map[string]any   `json:"row"`
	RowsRead     float64          `json:"rows_read"`
	RowsWritten  float64          `json:"rows_written"`
	DatabaseSize float64          `json:"database_size"`
}

func testDurableObjectSQL(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		const name = "e2e-sql"
		const create = `CREATE TABLE e2e_items (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			note TEXT,
			qty REAL NOT NULL,
			flag INTEGER NOT NULL,
			blob BLOB
		)`
		blob := []byte{0x00, 0x01, 0xff, 'h', 'i'}

		var created doSQLResponse
		postNotebookOK(t, w, "/notebook/sql", name, sqlPayload{SQL: create}, &created)
		if created.DatabaseSize <= 0 {
			t.Errorf("database_size after create = %v, want > 0", created.DatabaseSize)
		}

		var inserted doSQLResponse
		postNotebookOK(t, w, "/notebook/sql", name, sqlPayload{
			SQL:  `INSERT INTO e2e_items (id, name, note, qty, flag, blob) VALUES (?, ?, ?, ?, ?, ?)`,
			Args: []any{1, "widget", nil, 1.5, boolArgJSON(true), blobArgJSON(blob)},
		}, &inserted)
		if inserted.RowsWritten != 1 {
			t.Errorf("insert rows_written = %v, want 1", inserted.RowsWritten)
		}

		postNotebookOK(t, w, "/notebook/sql", name, sqlPayload{
			SQL:  `INSERT INTO e2e_items (id, name, note, qty, flag, blob) VALUES (?, ?, ?, ?, ?, ?)`,
			Args: []any{2, "gadget", "present", 2, boolArgJSON(false), nil},
		}, &doSQLResponse{})

		var got doSQLResponse
		postNotebookOK(t, w, "/notebook/sql", name, sqlPayload{
			SQL: `SELECT id, name, note, qty, flag, blob FROM e2e_items ORDER BY id`,
		}, &got)
		if !equalStringSlices(got.Columns, []string{"id", "name", "note", "qty", "flag", "blob"}) {
			t.Fatalf("columns = %#v, want [id name note qty flag blob]", got.Columns)
		}
		if len(got.Rows) != 2 {
			t.Fatalf("len(rows) = %d, want 2 (%#v)", len(got.Rows), got.Rows)
		}
		if got.RowsRead < 2 {
			t.Errorf("rows_read = %v, want >= 2", got.RowsRead)
		}
		if got.DatabaseSize <= 0 {
			t.Errorf("database_size = %v, want > 0", got.DatabaseSize)
		}
		// A JS boolean bind is stored as the text "true" / "false". SqlStorage
		// has no boolean affinity; the driver forwards the bool unchanged.
		assertSQLRow(t, got.Rows[0], map[string]any{
			"id": float64(1), "name": "widget", "note": nil, "qty": 1.5, "flag": "true", "blob": blob,
		})
		assertSQLRow(t, got.Rows[1], map[string]any{
			"id": float64(2), "name": "gadget", "note": "present", "qty": float64(2), "flag": "false", "blob": nil,
		})

		var one doSQLResponse
		postNotebookOK(t, w, "/notebook/sql?read=one", name, sqlPayload{
			SQL:  `SELECT id, name, note FROM e2e_items WHERE id = ?`,
			Args: []any{1},
		}, &one)
		if !equalStringSlices(one.Columns, []string{"id", "name", "note"}) {
			t.Fatalf("one columns = %#v", one.Columns)
		}
		assertSQLRow(t, one.Row, map[string]any{"id": float64(1), "name": "widget", "note": nil})

		var none doSQLResponse
		postNotebookOK(t, w, "/notebook/sql", name, sqlPayload{
			SQL:  `SELECT id, name FROM e2e_items WHERE id = ?`,
			Args: []any{-1},
		}, &none)
		if !equalStringSlices(none.Columns, []string{"id", "name"}) {
			t.Fatalf("empty columns = %#v", none.Columns)
		}
		if len(none.Rows) != 0 {
			t.Fatalf("empty query rows = %#v, want none", none.Rows)
		}
	}
}

func assertSQLRow(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for k, wantV := range want {
		gotV, ok := got[k]
		if !ok {
			t.Errorf("row[%q] missing (row = %#v)", k, got)
			continue
		}
		switch wantBlob := wantV.(type) {
		case []byte:
			s, ok := gotV.(string)
			if !ok {
				t.Errorf("row[%q] = %#v (%T), want base64 blob", k, gotV, gotV)
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				t.Errorf("row[%q] base64: %v", k, err)
				continue
			}
			if !bytes.Equal(decoded, wantBlob) {
				t.Errorf("row[%q] = %x, want %x", k, decoded, wantBlob)
			}
		default:
			if gotV != wantV {
				t.Errorf("row[%q] = %#v (%T), want %#v (%T)", k, gotV, gotV, wantV, wantV)
			}
		}
	}
}

type doDBExecResponse struct {
	RowsAffected      int64  `json:"rows_affected"`
	LastInsertIDError string `json:"last_insert_id_error"`
}

type doDBQueryResponse struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func testDurableObjectOpenDB(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		const name = "e2e-db"
		const create = `CREATE TABLE e2e_items (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			note TEXT,
			qty REAL NOT NULL,
			flag INTEGER NOT NULL,
			blob BLOB,
			at TEXT
		)`
		blob := []byte{0x10, 0x20, 0x30}
		at := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)

		postNotebookOK(t, w, "/notebook/db/exec", name, sqlPayload{SQL: create}, &doDBExecResponse{})

		var inserted doDBExecResponse
		postNotebookOK(t, w, "/notebook/db/exec", name, sqlPayload{
			SQL: `INSERT INTO e2e_items (id, name, note, qty, flag, blob, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{
				1, "widget", nil, 1.5, boolArgJSON(true), blobArgJSON(blob),
				map[string]string{"time": at.Format(time.RFC3339Nano)},
			},
		}, &inserted)
		if inserted.RowsAffected != 1 {
			t.Errorf("rows_affected = %d, want 1", inserted.RowsAffected)
		}
		if !strings.Contains(inserted.LastInsertIDError, "LastInsertId is not supported") {
			t.Errorf("last_insert_id_error = %q, want the unsupported-LastInsertId error", inserted.LastInsertIDError)
		}

		var got doDBQueryResponse
		postNotebookOK(t, w, "/notebook/db/query", name, sqlPayload{
			SQL:  `SELECT id, name, note, qty, flag, blob, at FROM e2e_items WHERE name = ?`,
			Args: []any{"widget"},
		}, &got)
		if !equalStringSlices(got.Columns, []string{"id", "name", "note", "qty", "flag", "blob", "at"}) {
			t.Fatalf("columns = %#v", got.Columns)
		}
		if len(got.Rows) != 1 {
			t.Fatalf("len(rows) = %d, want 1 (%#v)", len(got.Rows), got.Rows)
		}
		row := got.Rows[0]
		assertDBCell(t, row, 0, float64(1))
		assertDBCell(t, row, 1, "widget")
		assertDBCell(t, row, 2, nil)
		assertDBCell(t, row, 3, 1.5)
		assertDBCell(t, row, 4, "true")
		assertDBCell(t, row, 5, blob)
		assertDBCell(t, row, 6, at.UTC().Format(time.RFC3339Nano))
	}
}

func assertDBCell(t *testing.T, row []any, i int, want any) {
	t.Helper()
	if i >= len(row) {
		t.Errorf("row has %d cells, want index %d", len(row), i)
		return
	}
	got := row[i]
	if blob, ok := want.([]byte); ok {
		s, ok := got.(string)
		if !ok {
			t.Errorf("row[%d] = %#v (%T), want base64 blob", i, got, got)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Errorf("row[%d] base64: %v", i, err)
			return
		}
		if !bytes.Equal(decoded, blob) {
			t.Errorf("row[%d] = %x, want %x", i, decoded, blob)
		}
		return
	}
	if got != want {
		t.Errorf("row[%d] = %#v (%T), want %#v (%T)", i, got, got, want, want)
	}
}

type doTxnResponse struct {
	Result   string             `json:"result"`
	Error    string             `json:"error"`
	Values   map[string]*string `json:"values"`
	SQLCount *int               `json:"sql_count"`
}

func testDurableObjectTransaction(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("commit", func(t *testing.T) {
			var got doTxnResponse
			postNotebookOK(t, w, "/notebook/txn", "e2e-txn-commit", map[string]any{
				"mode": "commit",
				"puts": []map[string]string{{"key": "color", "value": "blue"}},
			}, &got)
			if got.Error != "" {
				t.Fatalf("error = %q, want empty", got.Error)
			}
			if got.Result != "committed" {
				t.Errorf("result = %q, want committed", got.Result)
			}
			if got.Values["color"] == nil || *got.Values["color"] != "blue" {
				t.Errorf("values[color] = %v, want blue", got.Values["color"])
			}
		})

		t.Run("rollback on error", func(t *testing.T) {
			var got doTxnResponse
			postNotebookOK(t, w, "/notebook/txn", "e2e-txn-error", map[string]any{
				"mode": "error",
				"puts": []map[string]string{{"key": "color", "value": "red"}},
			}, &got)
			if !strings.Contains(got.Error, "intentional rollback") {
				t.Fatalf("error = %q, want the intentional rollback", got.Error)
			}
			if got.Values["color"] != nil {
				t.Errorf("values[color] = %q, want absent after rollback", *got.Values["color"])
			}
		})

		t.Run("rollback", func(t *testing.T) {
			var got doTxnResponse
			postNotebookOK(t, w, "/notebook/txn", "e2e-txn-rollback", map[string]any{
				"mode": "rollback",
				"puts": []map[string]string{{"key": "color", "value": "green"}},
			}, &got)
			if got.Error == "" {
				t.Fatal("error is empty, want rollback to fail the transaction")
			}
			if got.Values["color"] != nil {
				t.Errorf("values[color] = %q, want absent after rollback", *got.Values["color"])
			}
		})

		t.Run("sql commit", func(t *testing.T) {
			var got doTxnResponse
			postNotebookOK(t, w, "/notebook/txn", "e2e-txn-sql-commit", map[string]any{
				"mode": "sql-commit",
				"id":   1,
				"name": "kept",
			}, &got)
			if got.Error != "" {
				t.Fatalf("error = %q, want empty", got.Error)
			}
			if got.SQLCount == nil || *got.SQLCount != 1 {
				t.Fatalf("sql_count = %v, want 1", got.SQLCount)
			}
		})

		t.Run("sql rollback on error", func(t *testing.T) {
			var got doTxnResponse
			postNotebookOK(t, w, "/notebook/txn", "e2e-txn-sql-error", map[string]any{
				"mode": "sql-error",
				"id":   1,
				"name": "dropped",
			}, &got)
			if !strings.Contains(got.Error, "intentional rollback") {
				t.Fatalf("error = %q, want the intentional rollback", got.Error)
			}
			if got.SQLCount == nil || *got.SQLCount != 0 {
				t.Fatalf("sql_count = %v, want 0", got.SQLCount)
			}
		})
	}
}

func testDurableObjectBlockConcurrencyWhile(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		var got struct {
			Result string `json:"result"`
			Inside string `json:"inside"`
			Stored string `json:"stored"`
		}
		postNotebookOK(t, w, "/notebook/block", "e2e-block", map[string]string{
			"key":   "gate",
			"value": "held",
		}, &got)
		if got.Result != "held" || got.Inside != "held" || got.Stored != "held" {
			t.Fatalf("blockConcurrencyWhile = %+v, want result/inside/stored held", got)
		}
	}
}

func testDurableObjectStorage(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		const name = "e2e-storage"
		postOp := func(op map[string]any, dest any) {
			t.Helper()
			postNotebookOK(t, w, "/notebook/storage", name, op, dest)
		}

		var missing struct {
			OK    bool   `json:"ok"`
			Value string `json:"value"`
		}
		postOp(map[string]any{"op": "get", "key": "missing"}, &missing)
		if missing.OK {
			t.Fatalf("get missing ok = true, want false")
		}

		postOp(map[string]any{"op": "put", "key": "k", "value": "v"}, &map[string]any{})
		var got struct {
			OK    bool   `json:"ok"`
			Value string `json:"value"`
		}
		postOp(map[string]any{"op": "get", "key": "k"}, &got)
		if !got.OK || got.Value != "v" {
			t.Fatalf("get = %+v, want ok value v", got)
		}

		postOp(map[string]any{"op": "putMultiple", "entries": map[string]string{"p:a": "1", "p:b": "2", "q:c": "3"}}, &map[string]any{})
		var multi struct {
			Values map[string]string `json:"values"`
		}
		postOp(map[string]any{"op": "getMultiple", "keys": []string{"p:a", "p:b", "nope"}}, &multi)
		if multi.Values["p:a"] != "1" || multi.Values["p:b"] != "2" {
			t.Fatalf("getMultiple = %#v", multi.Values)
		}
		if _, ok := multi.Values["nope"]; ok {
			t.Fatalf("getMultiple included missing key: %#v", multi.Values)
		}

		var listed struct {
			Values map[string]string `json:"values"`
		}
		postOp(map[string]any{"op": "list", "prefix": "p:"}, &listed)
		if len(listed.Values) != 2 || listed.Values["p:a"] != "1" || listed.Values["p:b"] != "2" {
			t.Fatalf("list prefix p: = %#v", listed.Values)
		}

		var deleted struct {
			Deleted bool `json:"deleted"`
		}
		postOp(map[string]any{"op": "delete", "key": "k"}, &deleted)
		if !deleted.Deleted {
			t.Fatal("delete existing key = false, want true")
		}
		postOp(map[string]any{"op": "get", "key": "k"}, &missing)
		if missing.OK {
			t.Fatal("get after delete still present")
		}

		var deletedN struct {
			Deleted float64 `json:"deleted"`
		}
		postOp(map[string]any{"op": "deleteMultiple", "keys": []string{"p:a", "p:b"}}, &deletedN)
		if deletedN.Deleted != 2 {
			t.Fatalf("deleteMultiple = %v, want 2", deletedN.Deleted)
		}
		postOp(map[string]any{"op": "get", "key": "p:a"}, &missing)
		if missing.OK {
			t.Fatal("get p:a after deleteMultiple still present")
		}
		// A fresh map: encoding/json keeps keys already present when
		// unmarshalling into a non-nil map.
		afterMulti := struct {
			Values map[string]string `json:"values"`
		}{}
		postOp(map[string]any{"op": "list"}, &afterMulti)
		if len(afterMulti.Values) != 1 || afterMulti.Values["q:c"] != "3" {
			t.Fatalf("list after deleteMultiple = %#v, want only q:c", afterMulti.Values)
		}

		postOp(map[string]any{"op": "sync"}, &map[string]any{})

		var setAlarm struct {
			Scheduled int64 `json:"scheduled"`
		}
		postOp(map[string]any{"op": "setAlarm"}, &setAlarm)
		var alarm struct {
			OK        bool  `json:"ok"`
			Scheduled int64 `json:"scheduled"`
		}
		postOp(map[string]any{"op": "getAlarm"}, &alarm)
		if !alarm.OK || alarm.Scheduled != setAlarm.Scheduled {
			t.Fatalf("getAlarm = %+v, want ok scheduled %d", alarm, setAlarm.Scheduled)
		}
		postOp(map[string]any{"op": "deleteAlarm"}, &map[string]any{})
		postOp(map[string]any{"op": "getAlarm"}, &alarm)
		if alarm.OK {
			t.Fatalf("getAlarm after delete = %+v, want ok false", alarm)
		}

		postOp(map[string]any{"op": "put", "key": "z", "value": "gone"}, &map[string]any{})
		postOp(map[string]any{"op": "deleteAll"}, &map[string]any{})
		postOp(map[string]any{"op": "get", "key": "z"}, &missing)
		if missing.OK {
			t.Fatal("get after deleteAll still present")
		}
		afterAll := struct {
			Values map[string]string `json:"values"`
		}{}
		postOp(map[string]any{"op": "list", "noCache": true}, &afterAll)
		if len(afterAll.Values) != 0 {
			t.Fatalf("list after deleteAll = %#v, want empty", afterAll.Values)
		}
	}
}
