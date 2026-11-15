//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// d1Request mirrors testdata/workers/kitchensink/d1.go's d1Request: a SQL
// statement plus its positional args. Args are encoded as plain JSON
// values, except for []byte which the fixture requires as
// {"base64": "..."} (see blobArg below).
type d1Request struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

// blobArg wraps a []byte argument in the shape the fixture's d1Arg decoder
// expects, so BLOB values can round-trip through D1.
func blobArg(b []byte) map[string]string {
	return map[string]string{"base64": base64.StdEncoding.EncodeToString(b)}
}

type d1ExecResponse struct {
	LastInsertID int64 `json:"last_insert_id"`
	RowsAffected int64 `json:"rows_affected"`
}

type d1QueryResponse struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// d1Exec posts to /d1/exec and decodes the response, failing the test on
// any non-200 status or malformed JSON.
func d1Exec(t *testing.T, w *worker, sql string, args ...any) d1ExecResponse {
	t.Helper()
	return d1ExecStatus(t, w, http.StatusOK, sql, args...)
}

// d1ExecStatus is like d1Exec but lets the caller assert a specific status
// code (e.g. to check that a malformed statement fails).
func d1ExecStatus(t *testing.T, w *worker, wantStatus int, sql string, args ...any) d1ExecResponse {
	t.Helper()
	body := encodeD1Request(t, sql, args)
	resp, respBody := w.Do(t, http.MethodPost, "/d1/exec", nil, bytes.NewReader(body))
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST /d1/exec status = %d, want %d (body = %q)", resp.StatusCode, wantStatus, respBody)
	}
	var got d1ExecResponse
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal([]byte(respBody), &got); err != nil {
			t.Fatalf("failed to unmarshal exec response %q: %v", respBody, err)
		}
	}
	return got
}

func d1Query(t *testing.T, w *worker, sql string, args ...any) d1QueryResponse {
	t.Helper()
	body := encodeD1Request(t, sql, args)
	resp, respBody := w.Do(t, http.MethodPost, "/d1/query", nil, bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /d1/query status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, respBody)
	}
	var got d1QueryResponse
	if err := json.Unmarshal([]byte(respBody), &got); err != nil {
		t.Fatalf("failed to unmarshal query response %q: %v", respBody, err)
	}
	return got
}

func encodeD1Request(t *testing.T, sql string, args []any) []byte {
	t.Helper()
	b, err := json.Marshal(d1Request{SQL: sql, Args: args})
	if err != nil {
		t.Fatalf("failed to marshal d1 request: %v", err)
	}
	return b
}

func testD1CreateInsertQuery(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_widgets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		)`)

		insertResult := d1Exec(t, w, "INSERT INTO e2e_widgets (name) VALUES (?)", "gizmo")
		if insertResult.RowsAffected != 1 {
			t.Errorf("RowsAffected = %d, want 1", insertResult.RowsAffected)
		}
		if insertResult.LastInsertID == 0 {
			t.Errorf("LastInsertID = 0, want nonzero")
		}

		queryResult := d1Query(t, w, "SELECT id, name FROM e2e_widgets WHERE id = ?", insertResult.LastInsertID)
		if !equalStringSlices(queryResult.Columns, []string{"id", "name"}) {
			t.Fatalf("Columns = %v, want [id name]", queryResult.Columns)
		}
		if len(queryResult.Rows) != 1 {
			t.Fatalf("len(Rows) = %d, want 1", len(queryResult.Rows))
		}
		row := queryResult.Rows[0]
		if got, want := int64(row[0].(float64)), insertResult.LastInsertID; got != want {
			t.Errorf("row[0] (id) = %v, want %v", got, want)
		}
		if got := row[1]; got != "gizmo" {
			t.Errorf("row[1] (name) = %v, want %q", got, "gizmo")
		}
	}
}

func testD1BlobRoundtrip(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_blobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data BLOB NOT NULL
		)`)

		want := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'h', 'i'}
		insertResult := d1Exec(t, w, "INSERT INTO e2e_blobs (data) VALUES (?)", blobArg(want))

		queryResult := d1Query(t, w, "SELECT data FROM e2e_blobs WHERE id = ?", insertResult.LastInsertID)
		if len(queryResult.Rows) != 1 {
			t.Fatalf("len(Rows) = %d, want 1", len(queryResult.Rows))
		}
		gotB64, ok := queryResult.Rows[0][0].(string)
		if !ok {
			t.Fatalf("row[0] (data) = %#v (%T), want base64 string", queryResult.Rows[0][0], queryResult.Rows[0][0])
		}
		got, err := base64.StdEncoding.DecodeString(gotB64)
		if err != nil {
			t.Fatalf("failed to decode base64 blob %q: %v", gotB64, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("data = %x, want %x", got, want)
		}
	}
}

func testD1NullRealText(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_mixed (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			maybe_null TEXT,
			ratio REAL NOT NULL,
			label TEXT NOT NULL
		)`)

		insertResult := d1Exec(t, w, "INSERT INTO e2e_mixed (maybe_null, ratio, label) VALUES (?, ?, ?)", nil, 3.5, "plain text")

		queryResult := d1Query(t, w, "SELECT maybe_null, ratio, label FROM e2e_mixed WHERE id = ?", insertResult.LastInsertID)
		if len(queryResult.Rows) != 1 {
			t.Fatalf("len(Rows) = %d, want 1", len(queryResult.Rows))
		}
		row := queryResult.Rows[0]

		// D1's client-side type conversion maps SQL NULL to JS null, which
		// convertRowColumnValueToAny (cloudflare/d1/rows.go) turns into a Go
		// nil -- this pins that real-runtime behavior rather than assuming
		// it.
		if row[0] != nil {
			t.Errorf("row[0] (maybe_null) = %#v, want nil", row[0])
		}
		if got, want := row[1], 3.5; got != want {
			t.Errorf("row[1] (ratio) = %v, want %v", got, want)
		}
		if got, want := row[2], "plain text"; got != want {
			t.Errorf("row[2] (label) = %v, want %q", got, want)
		}
	}
}

type d1BatchStatement struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

type d1BatchResult struct {
	LastInsertID int64    `json:"last_insert_id"`
	RowsAffected int64    `json:"rows_affected"`
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	Meta         struct {
		RowsRead    int64 `json:"RowsRead"`
		RowsWritten int64 `json:"RowsWritten"`
		Changes     int64 `json:"Changes"`
		ChangedDB   bool  `json:"ChangedDB"`
	} `json:"meta"`
}

type d1BatchResponse struct {
	Results []d1BatchResult `json:"results"`
}

// d1Batch posts the statements to /d1/batch and returns the raw response,
// leaving status handling to the caller.
func d1Batch(t *testing.T, w *worker, stmts ...d1BatchStatement) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"statements": stmts})
	if err != nil {
		t.Fatalf("failed to marshal d1 batch request: %v", err)
	}
	resp, respBody := w.Do(t, http.MethodPost, "/d1/batch", nil, bytes.NewReader(body))
	return resp.StatusCode, respBody
}

func testD1Batch(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_batch_users (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL
		)`)
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_batch_creds (
			user_id TEXT NOT NULL REFERENCES e2e_batch_users(id),
			pk BLOB NOT NULL
		)`)

		blob := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'h', 'i'}
		status, body := d1Batch(t, w,
			d1BatchStatement{SQL: "INSERT INTO e2e_batch_users (id, name) VALUES (?, ?)", Args: []any{"ok-1", "ann"}},
			d1BatchStatement{SQL: "INSERT INTO e2e_batch_creds (user_id, pk) VALUES (?, ?)", Args: []any{"ok-1", blobArg(blob)}},
			d1BatchStatement{SQL: "SELECT id, name FROM e2e_batch_users WHERE id = ?", Args: []any{"ok-1"}},
			d1BatchStatement{SQL: "SELECT pk FROM e2e_batch_creds WHERE user_id = ?", Args: []any{"ok-1"}},
		)
		if status != http.StatusOK {
			t.Fatalf("POST /d1/batch status = %d, want 200 (body = %q)", status, body)
		}
		var got d1BatchResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal batch response %q: %v", body, err)
		}
		if len(got.Results) != 4 {
			t.Fatalf("len(Results) = %d, want 4 (body = %q)", len(got.Results), body)
		}
		if got.Results[0].RowsAffected != 1 {
			t.Errorf("Results[0].RowsAffected = %d, want 1", got.Results[0].RowsAffected)
		}
		if got.Results[1].RowsAffected != 1 {
			t.Errorf("Results[1].RowsAffected = %d, want 1", got.Results[1].RowsAffected)
		}

		// D1's per-statement meta is surfaced. Each INSERT writes at least
		// the row itself, and a SELECT reads at least the row it returns.
		for i := 0; i < 2; i++ {
			m := got.Results[i].Meta
			if m.RowsWritten < 1 || m.Changes != 1 || !m.ChangedDB {
				t.Errorf("Results[%d].Meta = %+v, want RowsWritten >= 1, Changes = 1, ChangedDB", i, m)
			}
		}
		if m := got.Results[2].Meta; m.RowsRead < 1 || m.RowsWritten != 0 {
			t.Errorf("Results[2].Meta = %+v, want RowsRead >= 1, RowsWritten = 0", m)
		}

		// A SELECT inside the batch sees the batch's earlier writes.
		sel := got.Results[2]
		if !equalStringSlices(sel.Columns, []string{"id", "name"}) {
			t.Errorf("Results[2].Columns = %v, want [id name]", sel.Columns)
		}
		if len(sel.Rows) != 1 || sel.Rows[0][0] != "ok-1" || sel.Rows[0][1] != "ann" {
			t.Errorf("Results[2].Rows = %v, want [[ok-1 ann]]", sel.Rows)
		}

		// The BLOB bound inside the batch round-trips byte for byte.
		blobRows := got.Results[3].Rows
		if len(blobRows) != 1 {
			t.Fatalf("Results[3].Rows = %v, want 1 row", blobRows)
		}
		b64, ok := blobRows[0][0].(string)
		if !ok {
			t.Fatalf("Results[3] pk = %#v (%T), want base64 string", blobRows[0][0], blobRows[0][0])
		}
		gotBlob, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("failed to decode base64 blob %q: %v", b64, err)
		}
		if !bytes.Equal(gotBlob, blob) {
			t.Errorf("pk = %x, want %x", gotBlob, blob)
		}

		// The writes were committed and are visible outside the batch.
		q := d1Query(t, w, "SELECT COUNT(*) FROM e2e_batch_users WHERE id = ?", "ok-1")
		if len(q.Rows) != 1 || q.Rows[0][0] != float64(1) {
			t.Errorf("committed user count = %v, want 1", q.Rows)
		}
	}
}

// testD1BatchRollback is the core guarantee of D1Database.batch(): when the
// second statement fails, the first statement's write is rolled back, the
// third never runs, and the caller gets an error.
func testD1BatchRollback(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		d1Exec(t, w, `CREATE TABLE IF NOT EXISTS e2e_batch_rollback (
			id TEXT PRIMARY KEY
		)`)

		count := func(id string) float64 {
			t.Helper()
			q := d1Query(t, w, "SELECT COUNT(*) FROM e2e_batch_rollback WHERE id = ?", id)
			if len(q.Rows) != 1 {
				t.Fatalf("COUNT rows = %v, want 1 row", q.Rows)
			}
			n, ok := q.Rows[0][0].(float64)
			if !ok {
				t.Fatalf("COUNT = %#v (%T), want number", q.Rows[0][0], q.Rows[0][0])
			}
			return n
		}

		const insert = "INSERT INTO e2e_batch_rollback (id) VALUES (?)"

		t.Run("failing_second_statement", func(t *testing.T) {
			// Statement 2 targets a table that does not exist.
			status, body := d1Batch(t, w,
				d1BatchStatement{SQL: insert, Args: []any{"rb-first"}},
				d1BatchStatement{SQL: "INSERT INTO e2e_batch_no_such_table (id) VALUES (?)", Args: []any{"rb-second"}},
				d1BatchStatement{SQL: insert, Args: []any{"rb-third"}},
			)
			if status != http.StatusInternalServerError {
				t.Fatalf("POST /d1/batch status = %d, want 500 (body = %q)", status, body)
			}
			if !strings.Contains(body, "no such table") {
				t.Errorf("error body = %q, want it to mention the failing statement (no such table)", body)
			}
			for _, id := range []string{"rb-first", "rb-third"} {
				if n := count(id); n != 0 {
					t.Errorf("row %q count = %v, want 0: batch was not rolled back", id, n)
				}
			}
		})

		t.Run("control_without_batch_first_write_persists", func(t *testing.T) {
			// The same statements as constraint_violation_second_statement,
			// run one at a time through database/sql. There is no implicit
			// transaction here, so the first INSERT commits even though
			// the second fails. This proves the empty table asserted in
			// the batch subtests is due to batch() rolling back, not to the
			// failure itself (or the setup) removing the row.
			d1Exec(t, w, insert, "rb-control")
			d1ExecStatus(t, w, http.StatusInternalServerError, insert, "rb-control")
			if n := count("rb-control"); n != 1 {
				t.Errorf("row count = %v, want 1: the non-batch first INSERT should have committed", n)
			}
		})

		t.Run("constraint_violation_second_statement", func(t *testing.T) {
			// Same shape, but the failure is a UNIQUE constraint on the
			// second INSERT: the id it duplicates comes from the first
			// statement of the same batch.
			status, body := d1Batch(t, w,
				d1BatchStatement{SQL: insert, Args: []any{"rb-dup"}},
				d1BatchStatement{SQL: insert, Args: []any{"rb-dup"}},
			)
			if status != http.StatusInternalServerError {
				t.Fatalf("POST /d1/batch status = %d, want 500 (body = %q)", status, body)
			}
			if n := count("rb-dup"); n != 0 {
				t.Errorf("row count = %v, want 0: first INSERT survived a failed batch", n)
			}
		})

		t.Run("pre_existing_rows_untouched", func(t *testing.T) {
			// A failed batch must not disturb data committed earlier.
			d1Exec(t, w, insert, "rb-existing")
			status, body := d1Batch(t, w,
				d1BatchStatement{SQL: "DELETE FROM e2e_batch_rollback WHERE id = ?", Args: []any{"rb-existing"}},
				d1BatchStatement{SQL: insert, Args: []any{"rb-existing-dup"}},
				d1BatchStatement{SQL: insert, Args: []any{"rb-existing-dup"}},
			)
			if status != http.StatusInternalServerError {
				t.Fatalf("POST /d1/batch status = %d, want 500 (body = %q)", status, body)
			}
			if n := count("rb-existing"); n != 1 {
				t.Errorf("pre-existing row count = %v, want 1: DELETE was not rolled back", n)
			}
			if n := count("rb-existing-dup"); n != 0 {
				t.Errorf("rb-existing-dup count = %v, want 0", n)
			}
		})

		t.Run("later_batch_still_works", func(t *testing.T) {
			// The failure did not poison the connection or leave a
			// transaction open.
			status, body := d1Batch(t, w,
				d1BatchStatement{SQL: insert, Args: []any{"rb-after"}},
				d1BatchStatement{SQL: insert, Args: []any{"rb-after-2"}},
			)
			if status != http.StatusOK {
				t.Fatalf("POST /d1/batch status = %d, want 200 (body = %q)", status, body)
			}
			if n := count("rb-after"); n != 1 {
				t.Errorf("rb-after count = %v, want 1", n)
			}
			if n := count("rb-after-2"); n != 1 {
				t.Errorf("rb-after-2 count = %v, want 1", n)
			}
		})
	}
}
