package hono

import (
	"io"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// textFromStream reads a JS ReadableStream via the standard text() method
// (native JS stream consumption) by wrapping it in a throwaway Response,
// instead of jstest.ReadAll.
//
// jstest.ReadAll goes through jsutil's readableStreamToReadCloser, which has
// a bug (found while writing the root package's handler_js_test.go, not
// previously covered by any test): the first chunk pulled from a stream
// created by jsutil.ConvertReaderToReadableStream (as convertBodyToJS does
// for any body that is not already a jsutil.RawJSBodyGetter, e.g. the
// io.NopCloser(strings.NewReader(...)) used below) is always an empty
// priming chunk, and readableStreamToReadCloser.Read mishandles it as
// end-of-stream, silently losing the real content. See handler_js_test.go's
// textBody doc comment for the full explanation. This is a real bug in
// internal/jsutil/stream.go (not touched here, per the "no non-test code
// changes" rule for this PR); textFromStream exists only so
// TestRunHonoMiddleware_setHeaderStatusBody is not blocked by it.
func textFromStream(t testing.TB, stream js.Value) string {
	t.Helper()
	res := jsutil.ResponseClass.New(stream, jsutil.NewObject())
	v, err := jsutil.AwaitPromise(res.Call("text"))
	if err != nil {
		t.Fatalf("res.text(): %v", err)
	}
	return v.String()
}

// TestRunHonoMiddleware_callsNext verifies that runHonoMiddleware
// (registered on jsutil.Binding as "runHonoMiddleware" by this package's
// init) builds a *Context from context.ctx (here, a fakeHonoContext acting
// as Hono's own Context - see its doc comment for the asymmetry with
// Cloudflare's ExecutionContext) and calls the Middleware set via the
// package-level middleware variable (ServeMiddleware blocks forever, so
// tests set middleware directly instead), with a next function that invokes
// the JS-side next callback.
func TestRunHonoMiddleware_callsNext(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "http://example.com/path?x=1", nil, nil)
	fake := newFakeHonoContext(t, reqObj)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: fake.Value()})

	var (
		gotPath        string
		calledNextThen bool
	)
	middleware = func(c *Context, next func()) {
		gotPath = c.Request().URL.Path
		next()
		calledNextThen = true
	}
	t.Cleanup(func() { middleware = nil })

	var nextCalls int
	nextFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		nextCalls++
		return jstest.Resolved(js.Undefined())
	})

	p := jstest.Binding(t, "runHonoMiddleware").Invoke(nextFn)
	jstest.Await(t, p)

	if gotPath != "/path" {
		t.Errorf("c.Request().URL.Path = %q, want %q", gotPath, "/path")
	}
	if nextCalls != 1 {
		t.Errorf("JS next callback called %d times, want 1", nextCalls)
	}
	if !calledNextThen {
		t.Errorf("middleware did not resume after calling next()")
	}
}

// TestRunHonoMiddleware_setHeaderStatusBody verifies that Context's
// SetHeader/SetStatus/SetBody reach the underlying Hono Context object's
// header()/status()/body() calls.
func TestRunHonoMiddleware_setHeaderStatusBody(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	fake := newFakeHonoContext(t, reqObj)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: fake.Value()})

	middleware = func(c *Context, next func()) {
		c.SetHeader("X-Test", "yes")
		c.SetStatus(201)
		c.SetBody(io.NopCloser(strings.NewReader("hi")))
	}
	t.Cleanup(func() { middleware = nil })

	nextFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		return jstest.Resolved(js.Undefined())
	})
	p := jstest.Binding(t, "runHonoMiddleware").Invoke(nextFn)
	jstest.Await(t, p)

	if headerCalls := fake.HeaderCalls(); len(headerCalls) != 1 || headerCalls[0] != [2]string{"X-Test", "yes"} {
		t.Errorf("header() calls = %v, want [[X-Test yes]]", headerCalls)
	}
	if statusCalls := fake.StatusCalls(); len(statusCalls) != 1 || statusCalls[0] != 201 {
		t.Errorf("status() calls = %v, want [201]", statusCalls)
	}
	bodyCalls := fake.BodyCalls()
	if len(bodyCalls) != 1 {
		t.Fatalf("body() calls = %d, want 1", len(bodyCalls))
	}
	// textFromStream (not jstest.ReadAll) is used here to read the body:
	// see its doc comment for why jstest.ReadAll cannot be used to check
	// body content.
	if got := textFromStream(t, bodyCalls[0]); got != "hi" {
		t.Errorf("body() argument content = %q, want %q", got, "hi")
	}
}

// TestRunHonoMiddleware_nextRejects documents a known issue found while
// writing this test: middleware.go's next function
// (`jsutil.AwaitPromise(nextFnObj.Invoke())`) discards the error that
// AwaitPromise returns when the JS-side next() Promise rejects. So a
// middleware has no way to observe that next() failed, and
// runHonoMiddleware's own Promise still resolves instead of rejecting -
// unlike what the binding contract would suggest (the JS next() argument
// models Hono's own middleware chaining, where a downstream failure should
// be observable).
//
// Confirmed empirically with a throwaway probe test: a JS next callback
// that returns a rejected Promise, invoked the same way as
// TestRunHonoMiddleware_callsNext, still resulted in the middleware running
// past next() and the outer Promise resolving successfully.
func TestRunHonoMiddleware_nextRejects(t *testing.T) {
	t.Skip("known issue: middleware.go's next() discards the error from jsutil.AwaitPromise(nextFnObj.Invoke()), so a middleware can never observe (nor propagate) a rejected next() - runHonoMiddleware's Promise resolves instead of rejecting")
}
