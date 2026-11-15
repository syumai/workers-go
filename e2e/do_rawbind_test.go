//go:build e2e

package e2e

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// rawSQLResult mirrors testdata/workers/durableobject's rawSQLResult.
type rawSQLResult struct {
	Error  string `json:"error"`
	TypeOf string `json:"typeof"`
	Hex    string `json:"hex"`
}

// rawBindExpectation is what one Go value bound through the *generated*
// SQLStorage.Exec (no jsSQLBindings shim) must store in real workerd.
type rawBindExpectation struct {
	// wantTypeOf is SQLite's typeof() of what is stored ("text", "real",
	// "blob", "null"). wantBytes are the exact bytes SQLite stores: the
	// UTF-8 of the text for "text"/"real" (a REAL is compared via its text
	// form, e.g. "42.0"), the raw contents for "blob". The worker reports
	// them as hex(); the test decodes that so cases read as real values.
	wantTypeOf string
	wantBytes  []byte
	// mustNotFail, when set, asserts only that Exec succeeds. Used where
	// the stored form is a design choice (time.Time).
	mustNotFail bool
	// wantRejected, when set, asserts that Exec returns a deliberate error
	// for a value that has no faithful SQL representation, and stores
	// nothing. The error must not be the js.ValueOf panic ("ValueOf:
	// invalid value"): that is the bug, not a rejection. Used for slices
	// and maps, which workerd would otherwise silently stringify.
	wantRejected bool
	// orNull additionally accepts a stored NULL (a nil []byte may
	// reasonably map to either the empty BLOB or NULL).
	orNull bool
}

// rawBindCases are what each Go value must store when bound through the
// generated SQLStorage.Exec.
var rawBindCases = map[string]rawBindExpectation{
	"nil":        {wantTypeOf: "null"},
	"string":     {wantTypeOf: "text", wantBytes: []byte("text")},
	"float64":    {wantTypeOf: "real", wantBytes: []byte("1.5")},
	"bool_true":  {wantTypeOf: "text", wantBytes: []byte("true")},
	"bool_false": {wantTypeOf: "text", wantBytes: []byte("false")},
	// A js.Value passes through untouched, which is why handleSQL's shim
	// works. workerd stores both a Uint8Array and an ArrayBuffer as a BLOB.
	"js_uint8array":  {wantTypeOf: "blob", wantBytes: []byte{1, 2, 3}},
	"js_arraybuffer": {wantTypeOf: "blob", wantBytes: []byte{1, 2, 3}},

	// CHARACTERIZATION: these pass today but are not claims of correctness.
	// JS has one number type, so Go integers cross as doubles and SQLite
	// stores 42 as REAL "42.0". Confirmed identical in plain JS; this is
	// workerd, not workers-go.
	"int64":       {wantTypeOf: "real", wantBytes: []byte("42.0")},
	"int":         {wantTypeOf: "real", wantBytes: []byte("42.0")},
	"bytes":       {wantTypeOf: "blob", wantBytes: []byte{0x00, 0x01, 0xff, 'h', 'i'}},
	"bytes_empty": {wantTypeOf: "blob", wantBytes: []byte{}},
	// A nil []byte may reasonably be the empty BLOB or NULL.
	"bytes_nil": {wantTypeOf: "blob", wantBytes: []byte{}, orNull: true},

	// Slices and maps have no faithful SQL representation. Left alone they
	// reach workerd as a JS Array / plain object, which SqlStorage silently
	// stringifies ("1,x", "[object Object]"). Auto-encoding them (e.g. as
	// JSON) is wrong for generated code, since it cannot know the column is
	// meant to hold JSON; the caller must decide. So the correct behavior is
	// to return an error and store nothing.
	"any_slice": {wantRejected: true},
	"map":       {wantRejected: true},
	// How a time is stored is a design decision; it just must not crash.
	"time": {mustNotFail: true},
	// A []string is not an SQL value either, so it must be rejected with a
	// deliberate error rather than the js.ValueOf panic.
	"string_slice": {wantRejected: true},
}

// runRawBind binds each Go value in cases through the generated
// SQLStorage.Exec exactly as Go holds it and compares what workerd stored. It
// is the e2e ground truth for what the generated "...any" rest parameter does
// with each Go type.
func runRawBind(t *testing.T, w *worker, cases map[string]rawBindExpectation) {
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			q := url.Values{}
			q.Set("case", name)
			q.Set("name", "e2e-rawbind-"+name)
			resp, body := w.Get(t, "/notebook/rawsql?"+q.Encode())
			if resp.StatusCode != 200 {
				t.Fatalf("status = %d (body = %q)", resp.StatusCode, body)
			}
			var got rawSQLResult
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", body, err)
			}
			t.Logf("result: %+v", got)

			if want.wantRejected {
				if got.Error == "" {
					t.Fatalf("Exec accepted a value with no faithful SQL form and stored typeof=%s hex=%s; it must return an error", got.TypeOf, got.Hex)
				}
				if strings.Contains(got.Error, "ValueOf: invalid value") {
					t.Fatalf("Exec panicked in js.ValueOf instead of rejecting the value deliberately: %s", got.Error)
				}
				return
			}
			if got.Error != "" {
				t.Fatalf("Exec failed: %s", got.Error)
			}
			if want.mustNotFail {
				return
			}
			if want.orNull && got.TypeOf == "null" {
				return
			}
			gotBytes, err := hex.DecodeString(got.Hex)
			if err != nil {
				t.Fatalf("decode hex %q: %v", got.Hex, err)
			}
			if got.TypeOf != want.wantTypeOf || !bytes.Equal(gotBytes, want.wantBytes) {
				t.Errorf("stored typeof=%s bytes=%q, want typeof=%s bytes=%q",
					got.TypeOf, gotBytes, want.wantTypeOf, want.wantBytes)
			}
		})
	}
}

func testDurableObjectRawBind(w *worker) func(*testing.T) {
	return func(t *testing.T) { runRawBind(t, w, rawBindCases) }
}
