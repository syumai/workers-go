//go:build js && wasm

package cf

import (
	"fmt"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/internal/runtimecontext"
)

// FromJS decodes v — a JS object shaped like IncomingRequestCFProperties,
// typically the `cf` property of the Fetch API Request the runtime is
// currently handling — into an IncomingRequestCFProperties.
//
// Prefer FromRequest when you have a *http.Request from a workers.Serve
// handler; use FromJS directly only when you already hold the raw JS
// value (for example, a Request obtained some other way).
func FromJS(v js.Value) (IncomingRequestCFProperties, error) {
	return incomingRequestCFPropertiesFromJS(v)
}

// FromRequest extracts and decodes Cloudflare's edge request metadata (the
// `cf` object on the underlying Fetch API Request) for req.
//
// This only works for a req produced by workers.Serve / ServeNonBlock: the
// framework stashes the original JS Request object on the request's
// context (see internal/runtimecontext), and FromRequest reads it back out
// via req.Context(). A *http.Request built by hand — as in most unit
// tests — has no such JS Request attached and FromRequest returns an
// error; use FromJS directly in that case if you have a raw js.Value to
// decode instead.
func FromRequest(req *http.Request) (out IncomingRequestCFProperties, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cf: FromRequest: %v", r)
		}
	}()
	triggerObj := runtimecontext.MustExtractTriggerObj(req.Context())
	return FromJS(triggerObj.Get("cf"))
}
