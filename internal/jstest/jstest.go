//go:build js && wasm

// Package jstest provides small, cross-cutting helpers for testing
// workers-go packages under GOOS=js GOARCH=wasm (the Node.js test runner in
// testdata/wasm/). It is for tests only: never import it from non-test code.
//
// Several helpers replace package-level state in internal/jsutil
// (jsutil.RuntimeContext, jsutil.Binding) for the duration of a test and
// restore it via t.Cleanup. Because that state is shared for the whole test
// binary, do not combine SetRuntimeContext / SetEnv with t.Parallel().
//
// js.FuncOf callbacks registered from this package (and from tests using it)
// must never call t.Fatalf (or anything that calls runtime.Goexit, such as
// t.FailNow). js.FuncOf callbacks run on the goroutine that is servicing the
// JavaScript event loop; if that goroutine exits via runtime.Goexit before
// returning a value to JavaScript, the pending Promise chain that triggered
// the callback is abandoned and never settles, which deadlocks the test.
// Failures detected inside a callback must be reported with t.Errorf instead,
// and Await (see below) must only be called outside of a callback.
package jstest

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

var arrayBufferClass = js.Global().Get("ArrayBuffer")

// RuntimeContext is the Go-side representation of the fake globalThis.context
// object that SetRuntimeContext installs.
type RuntimeContext struct {
	// Env becomes context.env. Values that are already js.Value are used
	// as-is; any other value is converted with js.ValueOf. A nil Env
	// leaves context.env undefined.
	Env map[string]any
	// Ctx becomes context.ctx (the ExecutionContext). If it is the zero
	// js.Value (undefined), NewExecutionContext(t).Value() is used.
	Ctx js.Value
	// Connect becomes context.connect. If it is the zero js.Value
	// (undefined), context.connect is left undefined.
	Connect js.Value
}

// SetRuntimeContext replaces jsutil.RuntimeContext (globalThis.context) and
// jsutil.Binding with a fake built from rc, and restores both via
// t.Cleanup. It must not be combined with t.Parallel().
func SetRuntimeContext(t testing.TB, rc RuntimeContext) {
	t.Helper()

	prevRuntimeContext := jsutil.RuntimeContext
	prevBinding := jsutil.Binding

	ctx := rc.Ctx
	if ctx.IsUndefined() {
		ctx = NewExecutionContext(t).Value()
	}

	// The new context.binding reuses the existing jsutil.Binding value
	// (falling back to a fresh object if it was undefined) so that
	// jstest.Binding(t, name) keeps reading from the same object that is
	// exposed as context.binding.
	binding := prevBinding
	if binding.IsUndefined() {
		binding = jsutil.NewObject()
	}

	newCtx := jsutil.NewObject()
	if rc.Env != nil {
		newCtx.Set("env", envObject(rc.Env))
	}
	newCtx.Set("ctx", ctx)
	if !rc.Connect.IsUndefined() {
		newCtx.Set("connect", rc.Connect)
	}
	newCtx.Set("binding", binding)

	jsutil.RuntimeContext = newCtx
	jsutil.Binding = binding

	t.Cleanup(func() {
		jsutil.RuntimeContext = prevRuntimeContext
		jsutil.Binding = prevBinding
	})
}

// SetEnv is a shortcut for SetRuntimeContext(t, RuntimeContext{Env: env}).
func SetEnv(t testing.TB, env map[string]any) {
	t.Helper()
	SetRuntimeContext(t, RuntimeContext{Env: env})
}

func envObject(env map[string]any) js.Value {
	obj := jsutil.NewObject()
	for k, v := range env {
		if jv, ok := v.(js.Value); ok {
			obj.Set(k, jv)
		} else {
			obj.Set(k, js.ValueOf(v))
		}
	}
	return obj
}

// ExecutionContext is a fake ExecutionContext (the Cloudflare Workers
// ctx.waitUntil / ctx.passThroughOnException object) that records calls made
// to it so tests can assert on them.
type ExecutionContext struct {
	value js.Value

	waitUntilFn   js.Func
	passThroughFn js.Func

	mu               sync.Mutex
	promises         []js.Value
	passThroughCalls int
}

// NewExecutionContext creates an ExecutionContext. Its underlying js.Funcs
// are released via t.Cleanup.
func NewExecutionContext(t testing.TB) *ExecutionContext {
	t.Helper()

	ec := &ExecutionContext{}
	ec.waitUntilFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		var p js.Value
		if len(args) > 0 {
			p = args[0]
		}
		ec.mu.Lock()
		ec.promises = append(ec.promises, p)
		ec.mu.Unlock()
		return js.Undefined()
	})
	ec.passThroughFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		ec.mu.Lock()
		ec.passThroughCalls++
		ec.mu.Unlock()
		return js.Undefined()
	})
	t.Cleanup(func() {
		ec.waitUntilFn.Release()
		ec.passThroughFn.Release()
	})

	v := jsutil.NewObject()
	v.Set("waitUntil", ec.waitUntilFn)
	v.Set("passThroughOnException", ec.passThroughFn)
	ec.value = v

	return ec
}

// Value returns the underlying JS object to plug into RuntimeContext.Ctx.
func (c *ExecutionContext) Value() js.Value {
	return c.value
}

// Wait awaits every Promise passed to waitUntil so far. It must be called
// from outside of a js.FuncOf callback (see the package doc comment).
func (c *ExecutionContext) Wait(t testing.TB) {
	t.Helper()

	c.mu.Lock()
	promises := append([]js.Value(nil), c.promises...)
	c.mu.Unlock()

	for _, p := range promises {
		if _, err := jsutil.AwaitPromise(p); err != nil {
			t.Errorf("jstest: waitUntil promise rejected: %v", err)
		}
	}
}

// PassThroughCalls returns how many times passThroughOnException was called.
func (c *ExecutionContext) PassThroughCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.passThroughCalls
}

// Func wraps js.FuncOf and releases it via t.Cleanup.
func Func(t testing.TB, fn func(this js.Value, args []js.Value) any) js.Func {
	t.Helper()
	f := js.FuncOf(fn)
	t.Cleanup(f.Release)
	return f
}

func valueOf(v any) js.Value {
	if jv, ok := v.(js.Value); ok {
		return jv
	}
	return js.ValueOf(v)
}

// Resolved returns a Promise that resolves with v. v is converted with
// js.ValueOf unless it is already a js.Value.
func Resolved(v any) js.Value {
	jv := valueOf(v)
	var fn js.Func
	fn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer fn.Release()
		resolve := args[0]
		resolve.Invoke(jv)
		return js.Undefined()
	})
	return jsutil.NewPromise(fn)
}

// Rejected returns a Promise that rejects with an Error whose message is msg.
func Rejected(msg string) js.Value {
	var fn js.Func
	fn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer fn.Release()
		reject := args[1]
		reject.Invoke(jsutil.Error(msg))
		return js.Undefined()
	})
	return jsutil.NewPromise(fn)
}

// await is the non-fatal core of Await, split out so tests can inspect the
// error a rejected Promise produces without being able to observe a
// t.Fatalf call.
func await(p js.Value) (js.Value, error) {
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := jsutil.AwaitPromise(p)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(5 * time.Second):
		return js.Value{}, fmt.Errorf("jstest: timed out after 5s waiting for promise")
	}
}

// Await waits for p to settle, calling t.Fatalf if it rejects or does not
// settle within 5 seconds. Await must only be called from outside of a
// js.FuncOf callback: it blocks the calling goroutine and, on failure, calls
// t.Fatalf, which would deadlock the event loop if invoked from a callback
// (see the package doc comment).
func Await(t testing.TB, p js.Value) js.Value {
	t.Helper()
	v, err := await(p)
	if err != nil {
		t.Fatalf("jstest.Await: %v", err)
	}
	return v
}

// Uint8Array converts b to a JavaScript Uint8Array.
func Uint8Array(b []byte) js.Value {
	ua := jsutil.NewUint8Array(len(b))
	if len(b) > 0 {
		js.CopyBytesToJS(ua, b)
	}
	return ua
}

// Bytes reads a Uint8Array or ArrayBuffer into a Go []byte.
func Bytes(t testing.TB, v js.Value) []byte {
	t.Helper()
	if v.InstanceOf(arrayBufferClass) {
		v = jsutil.Uint8ArrayClass.New(v)
	}
	b := make([]byte, v.Get("byteLength").Int())
	if len(b) > 0 {
		js.CopyBytesToGo(b, v)
	}
	return b
}

// ReadableStream returns a ReadableStream that enqueues chunks in order and
// then closes.
func ReadableStream(chunks ...[]byte) js.Value {
	init := jsutil.NewObject()
	var start js.Func
	start = js.FuncOf(func(_ js.Value, args []js.Value) any {
		controller := args[0]
		for _, c := range chunks {
			controller.Call("enqueue", Uint8Array(c))
		}
		controller.Call("close")
		return js.Undefined()
	})
	init.Set("start", start)
	// ReadableStream's constructor calls start() synchronously, so it is
	// safe to release right after construction.
	rs := jsutil.ReadableStreamClass.New(init)
	start.Release()
	return rs
}

// ReadAll reads a ReadableStream to completion and returns its bytes.
func ReadAll(t testing.TB, stream js.Value) []byte {
	t.Helper()
	rc := jsutil.ConvertReadableStreamToReadCloser(stream)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("jstest.ReadAll: %v", err)
	}
	return b
}

// Request builds a JavaScript standard Request. If body is nil, the request
// has no body.
func Request(t testing.TB, method, url string, body []byte, header http.Header) js.Value {
	t.Helper()
	init := jsutil.NewObject()
	init.Set("method", method)
	init.Set("headers", jshttp.ToJSHeader(header))
	if body != nil {
		init.Set("body", Uint8Array(body))
	}
	return jsutil.RequestClass.New(url, init)
}

// Response builds a JavaScript standard Response. If body is nil, the
// response has no body.
func Response(t testing.TB, status int, body []byte, header http.Header) js.Value {
	t.Helper()
	init := jsutil.NewObject()
	init.Set("status", status)
	init.Set("headers", jshttp.ToJSHeader(header))
	bodyArg := jsutil.Null
	if body != nil {
		bodyArg = Uint8Array(body)
	}
	return jsutil.ResponseClass.New(bodyArg, init)
}

// Binding returns jsutil.Binding.Get(name), failing the test if it is
// undefined.
func Binding(t testing.TB, name string) js.Value {
	t.Helper()
	v := jsutil.Binding.Get(name)
	if v.IsUndefined() {
		t.Fatalf("jstest.Binding: binding %q not found", name)
	}
	return v
}

// ReadyCount returns how many times the //go:wasmimport workers ready import
// has been called, as recorded by the Node test runner
// (testdata/wasm/wasm_exec_node.js) on context.readyCount. It returns 0 if
// that property is undefined.
func ReadyCount(t testing.TB) int {
	t.Helper()
	v := js.Global().Get("context").Get("readyCount")
	if v.IsUndefined() {
		return 0
	}
	return v.Int()
}

// AssertObjectEqual asserts that got is a JS object whose own enumerable
// keys and values (compared one level deep) match want. Every difference is
// reported with t.Errorf.
func AssertObjectEqual(t testing.TB, got js.Value, want map[string]any) {
	t.Helper()
	for _, diff := range objectDiff(got, want) {
		t.Errorf("%s", diff)
	}
}

// objectDiff computes the differences between got and want, split out from
// AssertObjectEqual so it can be unit tested directly (a testing.T's
// failures can't otherwise be observed from within a test).
func objectDiff(got js.Value, want map[string]any) []string {
	if got.IsUndefined() || got.IsNull() {
		return []string{fmt.Sprintf("got is %v, want an object with keys %v", got, sortedKeys(want))}
	}

	gotKeys := map[string]bool{}
	keysArr := jsutil.ObjectClass.Call("keys", got)
	for i := 0; i < keysArr.Length(); i++ {
		gotKeys[keysArr.Index(i).String()] = true
	}

	var diffs []string
	for _, k := range sortedKeys(want) {
		if !gotKeys[k] {
			diffs = append(diffs, fmt.Sprintf("missing key %q", k))
			continue
		}
		if d := valueDiff(k, got.Get(k), want[k]); d != "" {
			diffs = append(diffs, d)
		}
	}
	wantKeys := map[string]bool{}
	for k := range want {
		wantKeys[k] = true
	}
	for k := range gotKeys {
		if !wantKeys[k] {
			diffs = append(diffs, fmt.Sprintf("unexpected key %q", k))
		}
	}

	sort.Strings(diffs)
	return diffs
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func valueDiff(key string, gv js.Value, wv any) string {
	switch w := wv.(type) {
	case nil:
		if !gv.IsNull() && !gv.IsUndefined() {
			return fmt.Sprintf("key %q: got %v, want null", key, gv)
		}
	case string:
		if gv.Type() != js.TypeString || gv.String() != w {
			return fmt.Sprintf("key %q: got %v, want %q", key, gv, w)
		}
	case bool:
		if gv.Type() != js.TypeBoolean || gv.Bool() != w {
			return fmt.Sprintf("key %q: got %v, want %v", key, gv, w)
		}
	case int:
		if gv.Type() != js.TypeNumber || gv.Int() != w {
			return fmt.Sprintf("key %q: got %v, want %d", key, gv, w)
		}
	case float64:
		if gv.Type() != js.TypeNumber || gv.Float() != w {
			return fmt.Sprintf("key %q: got %v, want %v", key, gv, w)
		}
	case js.Value:
		if !gv.Equal(w) {
			return fmt.Sprintf("key %q: got %v, want %v", key, gv, w)
		}
	default:
		return fmt.Sprintf("key %q: unsupported want type %T", key, wv)
	}
	return ""
}
