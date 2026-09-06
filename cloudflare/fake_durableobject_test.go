//go:build js && wasm

package cloudflare

import (
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeDurableObjectNamespace is a JS object shaped like a Workers
// DurableObjectNamespace binding (idFromName / get), built entirely via
// syscall/js. get() always returns the same stub, whose fetch() records the
// request it receives and resolves with a canned Response.
type fakeDurableObjectNamespace struct {
	val js.Value

	lastIdFromName string
	lastFetchReq   js.Value
}

// newFakeDurableObjectNamespace builds a fakeDurableObjectNamespace whose
// stub's fetch() always resolves with a Response built from status and body.
func newFakeDurableObjectNamespace(t testing.TB, status int, body []byte) *fakeDurableObjectNamespace {
	t.Helper()

	f := &fakeDurableObjectNamespace{}

	idFromNameFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		f.lastIdFromName = args[0].String()
		return js.ValueOf(map[string]any{"name": args[0].String()})
	})

	fetchFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		f.lastFetchReq = args[0]
		return jstest.Resolved(jstest.Response(t, status, body, nil))
	})
	stub := jsutil.NewObject()
	stub.Set("fetch", fetchFn)

	getFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		return stub
	})

	obj := jsutil.NewObject()
	obj.Set("idFromName", idFromNameFn)
	obj.Set("get", getFn)
	f.val = obj
	return f
}
