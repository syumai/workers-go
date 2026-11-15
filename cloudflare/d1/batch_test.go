//go:build js && wasm

package d1

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

const (
	insertUser = "INSERT INTO users (id) VALUES (?)"
	insertCred = "INSERT INTO creds (user_id, pk) VALUES (?, ?)"
	selectUser = "SELECT id, name FROM users WHERE id = ?"
)

// insertInto returns an effect that records the first bound arg (as text)
// as a row of table.
func insertInto(table string) func(*fakeStore, []js.Value) error {
	return func(s *fakeStore, args []js.Value) error {
		s.insert(table, args[0].String())
		return nil
	}
}

func TestBatch(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{
		insertUser: {lastRowID: p64(1), changes: p64(1), effect: insertInto("users")},
		insertCred: {lastRowID: p64(5), changes: p64(1), effect: insertInto("creds")},
		selectUser: {
			lastRowID: p64(0), changes: p64(0),
			columns: []string{"id", "name"},
			rows:    [][]any{{int64(1), "ann"}, {int64(2), nil}},
		},
	})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db,
		Statement{insertUser, []any{"u1"}},
		Statement{insertCred, []any{"u1", []byte{1, 2}}},
		Statement{selectUser, []any{"u1"}},
	)
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(res))
	}
	if id, err := res[1].LastInsertId(); err != nil || id != 5 {
		t.Errorf("res[1].LastInsertId() = (%v, %v), want (5, nil)", id, err)
	}
	if n, err := res[0].RowsAffected(); err != nil || n != 1 {
		t.Errorf("res[0].RowsAffected() = (%v, %v), want (1, nil)", n, err)
	}
	if got := strings.Join(res[2].Columns, ","); got != "id,name" {
		t.Errorf("res[2].Columns = %q, want id,name", got)
	}
	if len(res[2].Rows) != 2 ||
		res[2].Rows[0][0] != int64(1) || res[2].Rows[0][1] != "ann" ||
		res[2].Rows[1][0] != int64(2) || res[2].Rows[1][1] != nil {
		t.Errorf("res[2].Rows = %#v", res[2].Rows)
	}
	if res[0].Columns != nil || res[0].Rows != nil {
		t.Errorf("res[0] Columns/Rows = %#v/%#v, want nil", res[0].Columns, res[0].Rows)
	}

	// Exactly one batch() call carrying all three statements: the point of
	// Batch is a single round trip.
	if got := fake.batchSizesRecorded(); len(got) != 1 || got[0] != 3 {
		t.Errorf("batch calls = %v, want [3]", got)
	}
	// Nothing ran outside batch().
	if got := fake.store.table("users"); len(got) != 1 || got[0] != "u1" {
		t.Errorf("users = %v, want [u1]", got)
	}
	if got := fake.store.table("creds"); len(got) != 1 || got[0] != "u1" {
		t.Errorf("creds = %v, want [u1]", got)
	}
}

// TestBatch_blobArg is the regression test for the spike's known gap: a
// []byte inside a batched statement must reach bind() as a Uint8Array
// rather than failing with "ValueOf: invalid value".
func TestBatch_blobArg(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{
		insertCred: {changes: p64(1)},
	})
	db := openFakeDB(t, fake)

	want := []byte{0x00, 0x01, 0xff, 'h', 'i'}
	if _, err := Batch(context.Background(), db,
		Statement{insertCred, []any{"u1", want}},
	); err != nil {
		t.Fatalf("Batch() error = %v", err)
	}

	calls := fake.callsRecorded()
	if len(calls) != 1 {
		t.Fatalf("bind() calls = %d, want 1", len(calls))
	}
	arg := calls[0].args[1]
	if !arg.InstanceOf(js.Global().Get("Uint8Array")) {
		t.Fatalf("bind() args[1] is not a Uint8Array: %v", arg)
	}
	if got := jstest.Bytes(t, arg); !bytes.Equal(got, want) {
		t.Errorf("bind() args[1] = %v, want %v", got, want)
	}
}

// TestBatch_nilBlobBindsEmptyBlob pins that a nil []byte is bound as an
// empty BLOB, the same as Exec/Query do.
func TestBatch_nilBlobBindsEmptyBlob(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{insertCred: {changes: p64(1)}})
	db := openFakeDB(t, fake)

	if _, err := Batch(context.Background(), db,
		Statement{insertCred, []any{"u1", []byte(nil)}},
	); err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	arg := fake.callsRecorded()[0].args[1]
	if !arg.InstanceOf(js.Global().Get("Uint8Array")) || arg.Get("length").Int() != 0 {
		t.Errorf("bind() args[1] = %v, want an empty Uint8Array", arg)
	}
}

func TestBatch_blobResult(t *testing.T) {
	const q = "SELECT pk FROM creds"
	fake := newFakeD1(t, map[string]fakeResult{
		q: {
			columns: []string{"pk"},
			rows:    [][]any{{jstest.Uint8Array([]byte{9, 8, 7})}},
		},
	})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db, Statement{Query: q})
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	got, ok := res[0].Rows[0][0].([]byte)
	if !ok || !bytes.Equal(got, []byte{9, 8, 7}) {
		t.Errorf("Rows[0][0] = %#v, want []byte{9,8,7}", res[0].Rows[0][0])
	}
}

// TestBatch_failedSecondStatementRollsBackFirst is the core behavior of
// batch(): when the second statement fails, the first statement's write is
// undone, the third never runs, and the caller gets an error and no results.
func TestBatch_failedSecondStatementRollsBackFirst(t *testing.T) {
	const bad = "INSERT INTO nope (id) VALUES (?)"
	var thirdRan bool
	const third = "INSERT INTO users (id) VALUES (?) /* third */"
	fake := newFakeD1(t, map[string]fakeResult{
		insertUser: {lastRowID: p64(1), changes: p64(1), effect: insertInto("users")},
		bad: {effect: func(*fakeStore, []js.Value) error {
			return errors.New("D1_ERROR: no such table: nope: SQLITE_ERROR")
		}},
		third: {changes: p64(1), effect: func(s *fakeStore, _ []js.Value) error {
			thirdRan = true
			s.insert("users", "third")
			return nil
		}},
	})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db,
		Statement{insertUser, []any{"u1"}},
		Statement{bad, []any{"x"}},
		Statement{third, []any{"u3"}},
	)
	if err == nil {
		t.Fatalf("Batch() error = nil, want an error (results = %#v)", res)
	}
	if !strings.Contains(err.Error(), "no such table: nope") {
		t.Errorf("Batch() error = %v, want it to carry D1's message", err)
	}
	if res != nil {
		t.Errorf("Batch() results = %#v, want nil on failure", res)
	}

	// All-or-nothing: the first INSERT must have been rolled back...
	if got := fake.store.table("users"); len(got) != 0 {
		t.Errorf("users = %v, want empty: first statement was not rolled back", got)
	}
	// ...and execution stopped at the failing statement.
	if thirdRan {
		t.Errorf("third statement ran after the second failed")
	}
	// It was still a single batch() round trip.
	if got := fake.batchSizesRecorded(); len(got) != 1 || got[0] != 3 {
		t.Errorf("batch calls = %v, want [3]", got)
	}

	// The same writes with a working second statement do commit, proving the
	// empty table above is due to rollback and not a broken fake.
	if _, err := Batch(context.Background(), db,
		Statement{insertUser, []any{"u1"}},
		Statement{insertUser, []any{"u2"}},
	); err != nil {
		t.Fatalf("Batch() with valid statements error = %v", err)
	}
	if got := fake.store.table("users"); len(got) != 2 {
		t.Errorf("users = %v, want 2 rows after a successful batch", got)
	}
}

func TestBatch_errorOnFirstStatement(t *testing.T) {
	const bad = "INSERT INTO nope VALUES (1)"
	fake := newFakeD1(t, map[string]fakeResult{bad: {err: "D1_ERROR: no such table"}})
	db := openFakeDB(t, fake)

	_, err := Batch(context.Background(), db, Statement{Query: bad})
	if err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("Batch() error = %v, want no such table", err)
	}
}

// TestBatch_unsupportedArgIsRejectedBeforeSending checks that an argument
// with no JS form is reported as an error naming the statement, without
// panicking and without calling batch() (so no statement runs).
func TestBatch_unsupportedArgIsRejectedBeforeSending(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{
		insertUser: {changes: p64(1), effect: insertInto("users")},
		insertCred: {changes: p64(1), effect: insertInto("creds")},
	})
	db := openFakeDB(t, fake)

	_, err := Batch(context.Background(), db,
		Statement{insertUser, []any{"u1"}},
		Statement{insertCred, []any{"u1", make(chan int)}},
	)
	if err == nil {
		t.Fatal("Batch() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "batch statement 1") {
		t.Errorf("Batch() error = %v, want it to name statement 1", err)
	}
	if got := fake.batchSizesRecorded(); len(got) != 0 {
		t.Errorf("batch calls = %v, want none", got)
	}
	if got := fake.store.table("users"); len(got) != 0 {
		t.Errorf("users = %v, want empty", got)
	}
}

func TestBatch_empty(t *testing.T) {
	fake := newFakeD1(t, nil)
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db)
	if err != nil || res != nil {
		t.Fatalf("Batch() = (%v, %v), want (nil, nil)", res, err)
	}
	if got := fake.batchSizesRecorded(); len(got) != 0 {
		t.Errorf("batch calls = %v, want none", got)
	}
}

func TestBatch_canceledContext(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{insertUser: {changes: p64(1)}})
	db := openFakeDB(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Batch(ctx, db, Statement{insertUser, []any{"u1"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Batch() error = %v, want context.Canceled", err)
	}
	if got := fake.batchSizesRecorded(); len(got) != 0 {
		t.Errorf("batch calls = %v, want none", got)
	}
}

func TestBatch_missingMeta(t *testing.T) {
	// A result without last_row_id/changes surfaces the same error as the
	// database/sql driver's result rather than a silent zero.
	fake := newFakeD1(t, map[string]fakeResult{insertUser: {}})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db, Statement{insertUser, []any{"u1"}})
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	if _, err := res[0].LastInsertId(); err == nil {
		t.Error("LastInsertId() error = nil, want an error")
	}
	if _, err := res[0].RowsAffected(); err == nil {
		t.Error("RowsAffected() error = nil, want an error")
	}
}

func TestBatch_meta(t *testing.T) {
	fake := newFakeD1(t, map[string]fakeResult{
		insertUser: {
			lastRowID: p64(9), changes: p64(1),
			meta: map[string]any{
				"duration":          0.5,
				"size_after":        8192,
				"rows_read":         2,
				"rows_written":      3,
				"changed_db":        true,
				"served_by_region":  "WNAM",
				"served_by_colo":    "SJC",
				"served_by_primary": true,
				"total_attempts":    2,
				"timings":           map[string]any{"sql_duration_ms": 0.25},
			},
		},
		// Only some fields reported: the rest must be zero, not an error.
		selectUser: {
			changes: p64(0),
			meta:    map[string]any{"rows_read": 4},
			columns: []string{"id", "name"},
			rows:    [][]any{{int64(1), "ann"}},
		},
	})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db,
		Statement{insertUser, []any{"u1"}},
		Statement{selectUser, []any{"u1"}},
	)
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}

	want := Meta{
		Duration: 0.5, SizeAfter: 8192, RowsRead: 2, RowsWritten: 3,
		LastRowID: 9, ChangedDB: true, Changes: 1,
		ServedByRegion: "WNAM", ServedByColo: "SJC", ServedByPrimary: true,
		SQLDurationMs: 0.25, TotalAttempts: 2,
	}
	if res[0].Meta != want {
		t.Errorf("res[0].Meta = %+v, want %+v", res[0].Meta, want)
	}
	if got, want := res[1].Meta, (Meta{RowsRead: 4}); got != want {
		t.Errorf("res[1].Meta = %+v, want %+v", got, want)
	}
}

func TestBatch_metaMissing(t *testing.T) {
	// No meta at all: Meta is the zero value.
	fake := newFakeD1(t, map[string]fakeResult{insertUser: {}})
	db := openFakeDB(t, fake)

	res, err := Batch(context.Background(), db, Statement{insertUser, []any{"u1"}})
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	if res[0].Meta != (Meta{}) {
		t.Errorf("res[0].Meta = %+v, want zero", res[0].Meta)
	}
}

func TestBatch_notD1Driver(t *testing.T) {
	// Batch must refuse a *sql.DB from another driver instead of panicking
	// on a failed type assertion.
	sql.Register("d1-batch-test-other", otherDriver{})
	db, err := sql.Open("d1-batch-test-other", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = Batch(context.Background(), db, Statement{Query: "SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "d1 driver") {
		t.Fatalf("Batch() error = %v, want a d1 driver error", err)
	}
}

func TestBatch_statementIndexInPrepareError(t *testing.T) {
	// Sanity check that errors wrap with %w so callers can use errors.Is.
	fake := newFakeD1(t, map[string]fakeResult{insertCred: {}})
	db := openFakeDB(t, fake)
	_, err := Batch(context.Background(), db, Statement{insertCred, []any{func() {}}})
	if err == nil {
		t.Fatal("Batch() error = nil, want an error")
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("Batch() error %q does not wrap its cause", fmt.Sprint(err))
	}
}

// otherDriver is a database/sql driver that is not the d1 driver.
type otherDriver struct{}

func (otherDriver) Open(string) (driver.Conn, error) { return otherConn{}, nil }

type otherConn struct{}

func (otherConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (otherConn) Close() error                        { return nil }
func (otherConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
