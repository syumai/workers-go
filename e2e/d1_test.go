//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
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
