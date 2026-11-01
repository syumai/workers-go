//go:build js && wasm

// Package jsrt provides the minimal helper vocabulary used by generated
// Cloudflare bindings under exp/cloudflare/.... It delegates to
// internal/jsutil, internal/jshttp, and cloudflare/internal/cfruntimecontext
// rather than reimplementing their logic, so that cfgen's templates depend
// on a small, stable surface instead of the full internal API.
//
// exp/internal/jsrt intentionally does not import anything under exp/ so
// that a future split of exp/cloudflare into its own module does not run
// into the internal/ package boundary (see tmp/05-design-options.md
// section 4, "案 A' の具体的な置き方").
package jsrt

import (
	"fmt"
	"io"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Await awaits a JS Promise, returning its resolved value or an error
// derived from the rejection reason.
func Await(p js.Value) (js.Value, error) {
	return jsutil.AwaitPromise(p)
}

// Call invokes v.method(args...), recovering any JS exception raised by the
// call (a Go panic, per syscall/js) into an error instead of crashing the
// program.
func Call(v js.Value, method string, args ...any) (res js.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = js.Value{}
			err = fmt.Errorf("%s: %v", method, r)
		}
	}()
	res = v.Call(method, args...)
	return res, nil
}

// Binding resolves bindingName from the runtime's env object. It returns an
// error if the binding is undefined (i.e. not configured for the Worker).
//
// This reads jsutil.RuntimeContext directly (mirroring
// cloudflare/internal/cfruntimecontext.MustGetRuntimeContextEnv) rather than
// importing that package, since exp/internal/jsrt sits outside the
// cloudflare/ tree and Go's internal-package visibility rule would forbid
// the import.
func Binding(bindingName string) (js.Value, error) {
	env := jsutil.RuntimeContext.Get("env")
	if env.IsUndefined() {
		return js.Value{}, fmt.Errorf("jsrt: runtime context has no env")
	}
	v := env.Get(bindingName)
	if v.IsUndefined() {
		return js.Value{}, fmt.Errorf("jsrt: binding %q is not configured", bindingName)
	}
	return v, nil
}

// NewObject creates a new, empty JS object.
func NewObject() js.Value {
	return jsutil.NewObject()
}

// AsyncFunc builds a JS-callable js.Func that returns a Promise when
// invoked: handler runs in a new goroutine, and a non-nil error rejects the
// Promise while a nil error resolves it with the returned js.Value. The
// caller must Release the returned js.Func once it's no longer needed (e.g.
// once the JS call it was passed to has settled).
//
// Generated bindings use this for a method whose only parameter is a
// callback of TypeScript shape "(a: A) => Promise<U>" / "() => Promise<U>"
// (tmp/06-codegen-spec.md 5.1 item 5), such as DurableObjectStorage.
// Transaction and DurableObjectState.BlockConcurrencyWhile.
func AsyncFunc(handler func(args []js.Value) (js.Value, error)) js.Func {
	return jsutil.AsyncFunc(handler)
}

// RuntimeContextValue reads key directly off the runtime context object
// passed in from the JS side (env/ctx/binding, plus whatever a runtime
// shim under cmd/workers-assets-gen/assets/runtimes/*/runtime.mjs adds to
// createRuntimeContext's return value, e.g. "connect" or "EmailMessage").
// It returns an error if the value is undefined — either because the
// current runtime shim (e.g. browser.mjs) doesn't provide it, or because
// nothing has set up a runtime context at all (as in a plain `go test`
// process).
//
// This mirrors cloudflare/internal/cfruntimecontext.GetRuntimeContextValue,
// which exp/internal/jsrt cannot import directly: Go's internal/
// visibility rule only lets code under
// github.com/syumai/workers-go/cloudflare/... import
// github.com/syumai/workers-go/cloudflare/internal/..., and
// exp/internal/jsrt sits outside that tree (see the package doc comment).
func RuntimeContextValue(key string) (js.Value, error) {
	v := jsutil.RuntimeContext.Get(key)
	if v.IsUndefined() {
		return js.Value{}, fmt.Errorf("jsrt: runtime context value %q is not available", key)
	}
	return v, nil
}

// BytesFromJS copies the contents of a JS typed array (e.g. Uint8Array) or
// ArrayBuffer into a new Go byte slice.
func BytesFromJS(v js.Value) []byte {
	if IsNil(v) {
		return nil
	}
	return jsutil.BytesFromJS(v)
}

// BytesToJS copies a Go byte slice into a new JS Uint8Array.
func BytesToJS(b []byte) js.Value {
	return jsutil.BytesToJS(b)
}

var (
	float32ArrayClass = js.Global().Get("Float32Array")
	float64ArrayClass = js.Global().Get("Float64Array")
)

// Float32ArrayFromJS reads a JS Float32Array (or any indexable, .length'd
// array-like of numbers) into a new []float32.
func Float32ArrayFromJS(v js.Value) []float32 {
	if IsNil(v) {
		return nil
	}
	n := v.Get("length").Int()
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(v.Index(i).Float())
	}
	return out
}

// Float32ArrayToJS creates a new JS Float32Array from f.
func Float32ArrayToJS(f []float32) js.Value {
	arr := float32ArrayClass.New(len(f))
	for i, x := range f {
		arr.SetIndex(i, x)
	}
	return arr
}

// Float64ArrayFromJS reads a JS Float64Array (or any indexable, .length'd
// array-like of numbers) into a new []float64.
func Float64ArrayFromJS(v js.Value) []float64 {
	if IsNil(v) {
		return nil
	}
	n := v.Get("length").Int()
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = v.Index(i).Float()
	}
	return out
}

// Float64ArrayToJS creates a new JS Float64Array from f.
func Float64ArrayToJS(f []float64) js.Value {
	arr := float64ArrayClass.New(len(f))
	for i, x := range f {
		arr.SetIndex(i, x)
	}
	return arr
}

// DateToTime converts a JS Date value into a time.Time.
func DateToTime(v js.Value) time.Time {
	if IsNil(v) {
		return time.Time{}
	}
	t, _ := jsutil.DateToTime(v)
	return t
}

// TimeToDate converts a time.Time into a JS Date value.
func TimeToDate(t time.Time) js.Value {
	return jsutil.TimeToDate(t)
}

// ReadCloser converts a JS ReadableStream into an io.ReadCloser.
func ReadCloser(stream js.Value) io.ReadCloser {
	return jsutil.ConvertReadableStreamToReadCloser(stream)
}

// WriteCloser converts a JS WritableStream into an io.WriteCloser.
func WriteCloser(stream js.Value) io.WriteCloser {
	return jsutil.ConvertWritableStreamToWriteCloser(stream)
}

// ReadableStreamFromReader converts an io.Reader into a JS ReadableStream. If
// r also implements io.Closer, it is closed when the stream is closed or
// canceled.
func ReadableStreamFromReader(r io.Reader) js.Value {
	return jsutil.ConvertReaderToReadableStream(r)
}

// ArrayBufferFromReader reads r to completion and returns a JS ArrayBuffer
// holding those bytes. A nil reader yields an empty ArrayBuffer. If r
// implements io.Closer, it is closed before returning.
//
// R2's uploadPart accepts a ReadableStream only when its length is already
// known. ReadableStreamFromReader produces a stream without a length, which
// workerd rejects ("must have a known length"). An ArrayBuffer always has
// one, so generated bindings that must accept an io.Reader use this instead.
func ArrayBufferFromReader(r io.Reader) (js.Value, error) {
	if r == nil {
		return js.Global().Get("ArrayBuffer").New(0), nil
	}
	if c, ok := r.(io.Closer); ok {
		defer c.Close()
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return js.Value{}, err
	}
	return BytesToJS(b).Get("buffer"), nil
}

// HeadersFromJS converts a JS Headers value into an http.Header.
func HeadersFromJS(v js.Value) http.Header {
	return jshttp.ToHeader(v)
}

// HeadersToJS converts an http.Header into a JS Headers value.
func HeadersToJS(h http.Header) js.Value {
	return jshttp.ToJSHeader(h)
}

// IsNil reports whether v is JS undefined or null.
func IsNil(v js.Value) bool {
	return v.IsUndefined() || v.IsNull()
}
