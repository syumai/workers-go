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
//
// isEUCountry is normalized to the documented string form ("1" or ""):
// workers-types types it as the literal "1", but workerd's local cf
// object sends a boolean.
func FromJS(v js.Value) (IncomingRequestCFProperties, error) {
	out, err := incomingRequestCFPropertiesFromJS(v)
	if err != nil {
		return IncomingRequestCFProperties{}, err
	}
	// workers-types types isEUCountry as the string literal "1" (present
	// only when the request originated in the EU). workerd's local cf
	// object sends a boolean instead, and js.Value.String on a boolean is
	// the debug text "<boolean: false>" / "<boolean: true>". Map a boolean
	// back onto the documented string so callers can keep checking == "1".
	out.IsEUCountry = isEUCountryFromJS(v.Get("isEUCountry"))
	return out, nil
}

// isEUCountryFromJS decodes the isEUCountry property into the documented
// string form: "1" when the request originated in the EU, otherwise "".
func isEUCountryFromJS(v js.Value) string {
	switch v.Type() {
	case js.TypeString:
		return v.String()
	case js.TypeBoolean:
		if v.Bool() {
			return "1"
		}
		return ""
	default:
		return ""
	}
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
