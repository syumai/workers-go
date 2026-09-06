//go:build js && wasm

package d1

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func p64(v int64) *int64 { return &v }

func TestOpenConnector_undefined(t *testing.T) {
	jstest.SetEnv(t, map[string]any{})

	_, err := OpenConnector("DB")
	if !errors.Is(err, ErrDatabaseNotFound) {
		t.Fatalf("OpenConnector() error = %v, want %v", err, ErrDatabaseNotFound)
	}
}

func openFakeDB(t *testing.T, fake *fakeD1) *sql.DB {
	t.Helper()
	jstest.SetEnv(t, map[string]any{"DB": fake.value()})

	db, err := sql.Open("d1", "DB")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() error = %v", err)
		}
	})
	return db
}

func TestDB_Exec(t *testing.T) {
	const query = "INSERT INTO t (id, name, data) VALUES (?, ?, ?)"
	fake := newFakeD1(t, map[string]fakeResult{
		query: {lastRowID: p64(7), changes: p64(1)},
	})
	db := openFakeDB(t, fake)

	res, err := db.Exec(query, 1, "a", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error = %v", err)
	}
	if id != 7 {
		t.Errorf("LastInsertId() = %v, want 7", id)
	}

	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() error = %v", err)
	}
	if n != 1 {
		t.Errorf("RowsAffected() = %v, want 1", n)
	}

	calls := fake.callsRecorded()
	if len(calls) != 1 {
		t.Fatalf("bind() calls = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.query != query {
		t.Errorf("bind() query = %q, want %q", call.query, query)
	}
	if len(call.args) != 3 {
		t.Fatalf("bind() args = %d, want 3", len(call.args))
	}
	if got := call.args[0].Int(); got != 1 {
		t.Errorf("bind() args[0] = %v, want 1", got)
	}
	if got := call.args[1].String(); got != "a" {
		t.Errorf("bind() args[1] = %v, want %q", got, "a")
	}
	if got := jstest.Bytes(t, call.args[2]); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("bind() args[2] = %v, want %v", got, []byte{1, 2, 3})
	}
}

func TestDB_Query(t *testing.T) {
	const query = "SELECT id, name, score, data FROM t"
	fake := newFakeD1(t, map[string]fakeResult{
		query: {
			columns: []string{"id", "name", "score", "data"},
			rows: [][]any{
				{int64(1), nil, 1.5, jstest.Uint8Array([]byte{9, 8, 7})},
			},
		},
	})
	db := openFakeDB(t, fake)

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns() error = %v", err)
	}
	wantCols := []string{"id", "name", "score", "data"}
	if len(cols) != len(wantCols) {
		t.Fatalf("Columns() = %v, want %v", cols, wantCols)
	}
	for i := range cols {
		if cols[i] != wantCols[i] {
			t.Errorf("Columns()[%d] = %v, want %v", i, cols[i], wantCols[i])
		}
	}

	if !rows.Next() {
		t.Fatalf("Next() = false, want true (expected a row): %v", rows.Err())
	}

	var (
		id    int64
		name  sql.NullString
		score float64
		data  []byte
	)
	if err := rows.Scan(&id, &name, &score, &data); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if id != 1 {
		t.Errorf("id = %v, want 1", id)
	}
	if name.Valid {
		t.Errorf("name.Valid = true, want false (NULL): %+v", name)
	}
	if score != 1.5 {
		t.Errorf("score = %v, want 1.5", score)
	}
	if !bytes.Equal(data, []byte{9, 8, 7}) {
		t.Errorf("data = %v, want %v", data, []byte{9, 8, 7})
	}

	if rows.Next() {
		t.Fatalf("Next() = true, want false (only one row)")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

func TestDB_Begin_unsupported(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{})
	db := openFakeDB(t, fake)

	if _, err := db.Begin(); err == nil {
		t.Fatalf("Begin() error = nil, want an error")
	}
}

func TestDB_Prepare_reuse(t *testing.T) {
	const query = "UPDATE t SET name = ? WHERE id = ?"
	fake := newFakeD1(t, map[string]fakeResult{
		query: {changes: p64(1)},
	})
	db := openFakeDB(t, fake)

	stmt, err := db.Prepare(query)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer stmt.Close()

	for i, name := range []string{"first", "second"} {
		res, err := stmt.Exec(name, i+1)
		if err != nil {
			t.Fatalf("Exec() [%d] error = %v", i, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			t.Fatalf("RowsAffected() [%d] = (%v, %v), want (1, nil)", i, n, err)
		}
	}

	calls := fake.callsRecorded()
	if len(calls) != 2 {
		t.Fatalf("bind() calls = %d, want 2", len(calls))
	}
	for i, call := range calls {
		if call.query != query {
			t.Errorf("bind() calls[%d].query = %q, want %q", i, call.query, query)
		}
	}

	// The underlying JS statement (returned by prepare()) is reused across
	// both Exec calls, matching database/sql's *Stmt contract.
	if prepared := fake.prepareCallsRecorded(); len(prepared) != 1 {
		t.Errorf("prepare() calls = %v, want exactly 1 call", prepared)
	}
}
