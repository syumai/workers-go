//go:build js && wasm

package d1

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeResult is the result registered for one query on a fakeD1. It backs
// both bind(...).run() (meta.last_row_id / meta.changes) and
// bind(...).raw({columnNames:true}) (columns + rows).
type fakeResult struct {
	// lastRowID / changes become meta.last_row_id / meta.changes. A nil
	// pointer leaves the corresponding meta field undefined.
	lastRowID *int64
	changes   *int64

	// meta, if non-nil, is merged into the meta object after lastRowID and
	// changes, to supply any other D1 meta field (rows_written, ...).
	meta map[string]any

	// columns and rows back raw({columnNames:true}). Each row element may
	// be nil, an int64, a float64, a string, or a js.Value (e.g. a
	// Uint8Array for a BLOB column).
	columns []string
	rows    [][]any

	// err, if non-empty, makes both run() and raw() reject with an Error
	// carrying this message instead of resolving.
	err string

	// effect, if non-nil, is the statement's side effect on the fake's
	// store. It runs when the statement executes (run()/raw(), or in order
	// inside batch()) with the bound args, and may return an error to make
	// the statement fail like a SQL constraint violation. Unlike err, it
	// lets a test observe that an earlier statement in a failed batch was
	// rolled back.
	effect func(store *fakeStore, args []js.Value) error
}

// fakeStore is the fake's database state: a set of named row sets that
// effects append to. batch() snapshots it before the first statement and
// restores the snapshot if any statement fails, which is D1's documented
// batch semantics (all statements run in one implicit transaction).
type fakeStore struct {
	mu   sync.Mutex
	rows map[string][]string
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string][]string{}} }

func (s *fakeStore) insert(table, row string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[table] = append(s.rows[table], row)
}

// table returns a copy of the rows in table.
func (s *fakeStore) table(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.rows[name]...)
}

func (s *fakeStore) snapshot() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]string, len(s.rows))
	for k, v := range s.rows {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (s *fakeStore) restore(snap map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = snap
}

func (r fakeResult) runValue() js.Value {
	meta := jsutil.NewObject()
	if r.lastRowID != nil {
		meta.Set("last_row_id", *r.lastRowID)
	}
	if r.changes != nil {
		meta.Set("changes", *r.changes)
	}
	for k, v := range r.meta {
		meta.Set(k, v)
	}
	obj := jsutil.NewObject()
	obj.Set("meta", meta)
	return obj
}

func (r fakeResult) rawValue() js.Value {
	rowsData := make([]any, 0, len(r.rows)+1)
	cols := make([]any, len(r.columns))
	for i, c := range r.columns {
		cols[i] = c
	}
	rowsData = append(rowsData, cols)
	for _, row := range r.rows {
		rowsData = append(rowsData, append([]any(nil), row...))
	}
	return js.ValueOf(rowsData)
}

// fakeBindCall records one bind(...args) invocation: the query it was
// prepared with and the (unmodified, live) JS values passed to bind.
type fakeBindCall struct {
	query string
	args  []js.Value
}

// fakeD1 is a fake D1Database binding implementing
// prepare(query) -> { bind(...args) -> { run(), raw({columnNames}) } },
// with a per-query result registered up front by the test.
type fakeD1 struct {
	t       testing.TB
	results map[string]fakeResult
	// store is the state fakeResult.effect functions act on.
	store *fakeStore

	mu           sync.Mutex
	calls        []fakeBindCall
	prepareCalls []string
	batchSizes   []int
}

// newFakeD1 creates a fakeD1 with a fixed "this query resolves to this
// result" mapping. Queries not present in results reject when bound.
func newFakeD1(t testing.TB, results map[string]fakeResult) *fakeD1 {
	t.Helper()
	return &fakeD1{t: t, results: results, store: newFakeStore()}
}

// calls returns the bind() calls recorded so far, in order.
func (f *fakeD1) callsRecorded() []fakeBindCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeBindCall(nil), f.calls...)
}

// batchSizesRecorded returns the number of statements in each batch() call so
// far, in order.
func (f *fakeD1) batchSizesRecorded() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.batchSizes...)
}

func (f *fakeD1) recordBind(query string, args []js.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeBindCall{query: query, args: args})
}

// prepareCallsRecorded returns the queries passed to prepare() so far, in
// order (one entry per call, even for a repeated query).
func (f *fakeD1) prepareCallsRecorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prepareCalls...)
}

func (f *fakeD1) recordPrepare(query string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls = append(f.prepareCalls, query)
}

// value returns the JS D1Database object.
func (f *fakeD1) value() js.Value {
	obj := jsutil.NewObject()
	obj.Set("prepare", jstest.Func(f.t, func(_ js.Value, args []js.Value) any {
		query := args[0].String()
		f.recordPrepare(query)
		return f.stmtValue(query)
	}))
	obj.Set("batch", jstest.Func(f.t, func(_ js.Value, args []js.Value) any {
		return f.batch(args[0])
	}))
	return obj
}

// batch implements D1Database.batch(statements): statements run in order
// against the store inside one implicit transaction. The first failing
// statement rejects the returned promise and every earlier statement's
// effect is rolled back, mirroring D1.
func (f *fakeD1) batch(stmts js.Value) js.Value {
	n := stmts.Length()
	f.mu.Lock()
	f.batchSizes = append(f.batchSizes, n)
	f.mu.Unlock()

	snap := f.store.snapshot()
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		stmt := stmts.Index(i)
		query := stmt.Get("__query").String()
		res, ok := f.results[query]
		if !ok {
			f.store.restore(snap)
			return jstest.Rejected(fmt.Sprintf("fake_d1: no result registered for query %q", query))
		}
		bound := stmt.Get("__args")
		boundArgs := make([]js.Value, bound.Length())
		for j := range boundArgs {
			boundArgs[j] = bound.Index(j)
		}
		if err := res.execute(f.store, boundArgs); err != nil {
			f.store.restore(snap)
			return jstest.Rejected(err.Error())
		}
		out = append(out, res.batchValue())
	}
	return jstest.Resolved(js.ValueOf(out))
}

func (f *fakeD1) stmtValue(query string) js.Value {
	obj := jsutil.NewObject()
	obj.Set("bind", jstest.Func(f.t, func(_ js.Value, args []js.Value) any {
		bound := append([]js.Value(nil), args...)
		f.recordBind(query, bound)
		v := f.boundValue(query, bound)
		// batch() receives only the bound statement objects, so tag them
		// with what batch() needs to execute them.
		v.Set("__query", query)
		v.Set("__args", js.ValueOf(argsToAny(bound)))
		return v
	}))
	return obj
}

func argsToAny(args []js.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

// batchValue builds one D1Result ({success, meta, results}) where results is
// an array of {column: value} objects, as the real batch() returns.
func (r fakeResult) batchValue() js.Value {
	obj := r.runValue()
	obj.Set("success", true)
	rows := make([]any, 0, len(r.rows))
	for _, row := range r.rows {
		o := jsutil.NewObject()
		for i, c := range r.columns {
			o.Set(c, row[i])
		}
		rows = append(rows, o)
	}
	obj.Set("results", js.ValueOf(rows))
	return obj
}

// execute applies the statement's effect, if any, and reports its error.
func (r fakeResult) execute(store *fakeStore, args []js.Value) error {
	if r.err != "" {
		return errors.New(r.err)
	}
	if r.effect != nil {
		return r.effect(store, args)
	}
	return nil
}

func (f *fakeD1) boundValue(query string, args []js.Value) js.Value {
	lookup := func() (fakeResult, bool) {
		res, ok := f.results[query]
		return res, ok
	}
	obj := jsutil.NewObject()
	obj.Set("run", jstest.Func(f.t, func(_ js.Value, _ []js.Value) any {
		res, ok := lookup()
		if !ok {
			return jstest.Rejected(fmt.Sprintf("fake_d1: no result registered for query %q", query))
		}
		if err := res.execute(f.store, args); err != nil {
			return jstest.Rejected(err.Error())
		}
		return jstest.Resolved(res.runValue())
	}))
	obj.Set("raw", jstest.Func(f.t, func(_ js.Value, _ []js.Value) any {
		res, ok := lookup()
		if !ok {
			return jstest.Rejected(fmt.Sprintf("fake_d1: no result registered for query %q", query))
		}
		if err := res.execute(f.store, args); err != nil {
			return jstest.Rejected(err.Error())
		}
		return jstest.Resolved(res.rawValue())
	}))
	return obj
}
