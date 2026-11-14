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
	"reflect"
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

// AnyToJS converts an arbitrary Go value, as accepted by a generated
// "any"/"unknown"-typed field of a structured (RPC / structured-clone) value
// such as Vectorize metadata, into a JS value. It extends js.ValueOf (which
// supports only nil, bool, numbers, strings, []any, map[string]any and
// js.Value, and panics on anything else) with the conversions workers-go
// needs for Cloudflare APIs:
//
//   - nil becomes null
//   - []byte becomes a new Uint8Array (a nil slice becomes an empty one)
//   - time.Time becomes a Date
//   - js.Value, and types with a JSValue() js.Value method (generated handle
//     types), pass through
//   - slices/arrays and string-keyed maps become Arrays/plain objects, each
//     element converted recursively so that e.g. a []byte nested in a
//     map[string]any is converted too
//
// A value with no JS representation (a chan, func, struct, a map with
// non-string keys, ...) is reported as an error. AnyToJS never panics, so it
// is safe to call outside jsrt.Call's recover.
func AnyToJS(v any) (js.Value, error) {
	switch x := v.(type) {
	case nil:
		return js.Null(), nil
	case js.Value:
		return x, nil
	case interface{ JSValue() js.Value }:
		return x.JSValue(), nil
	case []byte:
		return BytesToJS(x), nil
	case time.Time:
		return TimeToDate(x), nil
	case bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64:
		return valueOf(v)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		arr := js.Global().Get("Array").New(rv.Len())
		for i := 0; i < rv.Len(); i++ {
			e, err := AnyToJS(rv.Index(i).Interface())
			if err != nil {
				return js.Value{}, fmt.Errorf("index %d: %w", i, err)
			}
			arr.SetIndex(i, e)
		}
		return arr, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			break
		}
		o := NewObject()
		iter := rv.MapRange()
		for iter.Next() {
			k := iter.Key().String()
			e, err := AnyToJS(iter.Value().Interface())
			if err != nil {
				return js.Value{}, fmt.Errorf("key %q: %w", k, err)
			}
			o.Set(k, e)
		}
		return o, nil
	case reflect.Pointer:
		if rv.IsNil() {
			return js.Null(), nil
		}
	}
	return js.Value{}, fmt.Errorf("jsrt: cannot convert %T to a JS value", v)
}

// valueOf is js.ValueOf with a panic turned into an error.
func valueOf(v any) (res js.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = js.Value{}
			err = fmt.Errorf("jsrt: cannot convert %T to a JS value: %v", v, r)
		}
	}()
	return js.ValueOf(v), nil
}

// AnyArgs applies AnyToJS to each element of args, returning a slice suitable
// for spreading into Call. Generated bindings use it for rest parameters
// typed any/unknown. It returns an error, naming the offending element, for a
// value with no JS representation. It never panics.
func AnyArgs(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := AnyToJS(a)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
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
