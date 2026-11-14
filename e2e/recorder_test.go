//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// recorderDescription mirrors callee.mjs's describe(): the JS type name
// workerd saw for a value, plus the value itself where it is JSON-safe.
type recorderDescription struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// valueOutcome is what one Go value, placed in a map[string]any / ...any
// position of a generated binding, becomes on the JS side.
type valueOutcome struct {
	// jsType is the JS type name the fake must observe ("String",
	// "Uint8Array", "Array", ...).
	jsType string
}

// recorderMatrix is the CORRECT behavior of every generated site that takes
// an `any`-typed value, one row per Go value. Every site shares the same
// conversion, so one table covers Vectorize (metadata/filter),
// DispatchNamespace.Get (args + outbound) and Workers AI GatewayOptions
// (metadata).

var recorderMatrix = map[string]valueOutcome{
	"str":    {jsType: "String"},
	"num":    {jsType: "Number"},
	"int":    {jsType: "Number"}, // JS has one number type; Go ints cross as doubles.
	"bool":   {jsType: "Boolean"},
	"nil":    {jsType: "Null"},
	"anys":   {jsType: "Array"},  // []any is converted natively by js.ValueOf.
	"nested": {jsType: "Object"}, // map[string]any likewise.
	// A js.Value is passed through untouched.
	"jsvalue": {jsType: "String"},

	// []byte must reach JS as a Uint8Array (SqlStorage and D1 both store a
	// Uint8Array as a BLOB).
	"bytes": {jsType: "Uint8Array"},
	// A []byte nested inside a map must be converted too, not left to panic.
	"nestedby": {jsType: "Object"},
	// Vectorize metadata documents string arrays as a valid value.
	"strs": {jsType: "Array"},
	// time.Time -> Date is the only sensible JS representation; a Date
	// round-trips through workerd RPC as a Date.
	"time": {jsType: "Date"},
}

// recorderSites are the generated entry points that convert an any-typed
// value, with the path the fixture exposes for each and a function that
// extracts the converted value from the fake's description.
var recorderSites = []struct {
	name    string
	path    string
	extract func(d recorderDescription, key string) (recorderDescription, bool)
}{
	{"vectorize_insert_metadata", "/vectorize/insert", func(d recorderDescription, k string) (recorderDescription, bool) {
		// [ { id, values, metadata: { <k>: ... } } ]
		return dig(d, "0", "metadata", k)
	}},
	{"vectorize_query_filter", "/vectorize/query", func(d recorderDescription, k string) (recorderDescription, bool) {
		// { vector, options: { filter: { <k>: ... } } }
		return dig(d, "options", "filter", k)
	}},
	{"dispatch_get_args", "/dispatch/get", func(d recorderDescription, k string) (recorderDescription, bool) {
		return dig(d, "args", k)
	}},
	{"dispatch_get_outbound", "/dispatch/get", func(d recorderDescription, k string) (recorderDescription, bool) {
		return dig(d, "options", "outbound", k)
	}},
	{"ai_gateway_metadata", "/ai/gateway", func(d recorderDescription, k string) (recorderDescription, bool) {
		return dig(d, "options", "gateway", "metadata", k)
	}},
}

// dig walks a describe() tree: "0" indexes an Array, any other step reads an
// Object field.
func dig(d recorderDescription, path ...string) (recorderDescription, bool) {
	cur := d
	for _, step := range path {
		switch cur.Type {
		case "Array":
			var items []recorderDescription
			if err := json.Unmarshal(cur.Value, &items); err != nil {
				return recorderDescription{}, false
			}
			var idx int
			if _, err := fmt.Sscanf(step, "%d", &idx); err != nil || idx >= len(items) {
				return recorderDescription{}, false
			}
			cur = items[idx]
		case "Object":
			var fields map[string]recorderDescription
			if err := json.Unmarshal(cur.Value, &fields); err != nil {
				return recorderDescription{}, false
			}
			next, ok := fields[step]
			if !ok {
				return recorderDescription{}, false
			}
			cur = next
		default:
			return recorderDescription{}, false
		}
	}
	return cur, true
}

// TestRecorderAnyConversion drives each generated any-typed site with each Go
// value kind in recorderMatrix and checks the JS type workerd received.
//
// Vectorize, dispatch namespaces and Workers AI cannot run under wrangler dev
// without a Cloudflare account, so the fixture binds them to fakes that
// report what they received. This does not prove the real service accepts the
// value -- only what crosses the binding boundary -- but that boundary is
// exactly where the generated Go -> JS conversion happens.
func TestRecorderAnyConversion(t *testing.T) {
	w := startWranglerWith(t, "recorder", wranglerOptions{extraConfigNames: []string{"callee.jsonc"}})

	for _, site := range recorderSites {
		t.Run(site.name, func(t *testing.T) {
			for key, want := range recorderMatrix {
				t.Run(key, func(t *testing.T) {
					resp, body := w.Get(t, site.path+"?only="+key)
					if resp.StatusCode != 200 {
						t.Fatalf("status = %d (body = %q)", resp.StatusCode, body)
					}

					var failure struct {
						GoError string `json:"go_error"`
					}
					_ = json.Unmarshal([]byte(body), &failure)
					if failure.GoError != "" {
						t.Fatalf("generated call failed: %s", failure.GoError)
					}

					var d recorderDescription
					if err := json.Unmarshal([]byte(body), &d); err != nil {
						t.Fatalf("unmarshal %q: %v", body, err)
					}
					got, ok := site.extract(d, key)
					if !ok {
						t.Fatalf("could not find %q in the fake's description: %s", key, body)
					}
					if got.Type != want.jsType {
						t.Errorf("JS type = %s, want %s", got.Type, want.jsType)
					}
				})
			}
		})
	}
}

// TestRecorderUnconvertibleValue checks the failure mode for a value with no
// JS representation (a chan, or a func nested in a slice in a map). The
// generated struct toJS methods run before jsrt.Call, so a panic there is not
// recovered and used to take the whole request down with an empty 500. Every
// site must instead return an error from the generated call, and never reach
// the fake service.
func TestRecorderUnconvertibleValue(t *testing.T) {
	w := startWranglerWith(t, "recorder", wranglerOptions{extraConfigNames: []string{"callee.jsonc"}})

	for _, site := range recorderSites {
		t.Run(site.name, func(t *testing.T) {
			for _, key := range []string{"unconvertible", "unconvertible_nested"} {
				t.Run(key, func(t *testing.T) {
					resp, body := w.Get(t, site.path+"?only="+key)
					if resp.StatusCode != 200 {
						t.Fatalf("status = %d (body = %q): the call must return an error, not crash the request", resp.StatusCode, body)
					}
					var got struct {
						GoError string `json:"go_error"`
					}
					if err := json.Unmarshal([]byte(body), &got); err != nil {
						t.Fatalf("unmarshal %q: %v", body, err)
					}
					if got.GoError == "" {
						t.Fatalf("generated call accepted an unconvertible value: %s", body)
					}
					if strings.HasPrefix(got.GoError, "panic:") {
						t.Fatalf("generated call panicked instead of returning an error: %s", got.GoError)
					}
				})
			}
		})
	}
}
