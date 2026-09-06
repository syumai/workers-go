//go:build js && wasm

package d1

import (
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

	// columns and rows back raw({columnNames:true}). Each row element may
	// be nil, an int64, a float64, a string, or a js.Value (e.g. a
	// Uint8Array for a BLOB column).
	columns []string
	rows    [][]any

	// err, if non-empty, makes both run() and raw() reject with an Error
	// carrying this message instead of resolving.
	err string
}

func (r fakeResult) runValue() js.Value {
	meta := jsutil.NewObject()
	if r.lastRowID != nil {
		meta.Set("last_row_id", *r.lastRowID)
	}
	if r.changes != nil {
		meta.Set("changes", *r.changes)
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

	mu           sync.Mutex
	calls        []fakeBindCall
	prepareCalls []string
}

// newFakeD1 creates a fakeD1 with a fixed "this query resolves to this
// result" mapping. Queries not present in results reject when bound.
func newFakeD1(t testing.TB, results map[string]fakeResult) *fakeD1 {
	t.Helper()
	return &fakeD1{t: t, results: results}
}

// calls returns the bind() calls recorded so far, in order.
func (f *fakeD1) callsRecorded() []fakeBindCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeBindCall(nil), f.calls...)
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
	return obj
}

func (f *fakeD1) stmtValue(query string) js.Value {
	obj := jsutil.NewObject()
	obj.Set("bind", jstest.Func(f.t, func(_ js.Value, args []js.Value) any {
		f.recordBind(query, append([]js.Value(nil), args...))
		return f.boundValue(query)
	}))
	return obj
}

func (f *fakeD1) boundValue(query string) js.Value {
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
		if res.err != "" {
			return jstest.Rejected(res.err)
		}
		return jstest.Resolved(res.runValue())
	}))
	obj.Set("raw", jstest.Func(f.t, func(_ js.Value, _ []js.Value) any {
		res, ok := lookup()
		if !ok {
			return jstest.Rejected(fmt.Sprintf("fake_d1: no result registered for query %q", query))
		}
		if res.err != "" {
			return jstest.Rejected(res.err)
		}
		return jstest.Resolved(res.rawValue())
	}))
	return obj
}
