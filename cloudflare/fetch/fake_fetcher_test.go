//go:build js && wasm

package fetch

import (
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeFetcher is a JS object shaped like a Workers service binding: it has a
// fetch(request, init) method, built entirely via syscall/js. It records
// every call it receives and resolves with a single canned Response.
type fakeFetcher struct {
	val js.Value

	mu       sync.Mutex
	requests []js.Value
	inits    []js.Value
}

// newFakeFetcher builds a fakeFetcher whose fetch() always resolves with
// res.
func newFakeFetcher(t testing.TB, res js.Value) *fakeFetcher {
	t.Helper()

	f := &fakeFetcher{}
	fetchFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		req := args[0]
		init := js.Undefined()
		if len(args) > 1 {
			init = args[1]
		}

		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.inits = append(f.inits, init)
		f.mu.Unlock()

		return jstest.Resolved(res)
	})

	obj := jsutil.NewObject()
	obj.Set("fetch", fetchFn)
	f.val = obj
	return f
}

// lastRequest returns the JS Request passed to the most recent fetch() call.
func (f *fakeFetcher) lastRequest() js.Value {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return js.Undefined()
	}
	return f.requests[len(f.requests)-1]
}

// lastInit returns the RequestInit object passed to the most recent fetch()
// call (undefined if fetch() was never called with one).
func (f *fakeFetcher) lastInit() js.Value {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.inits) == 0 {
		return js.Undefined()
	}
	return f.inits[len(f.inits)-1]
}
