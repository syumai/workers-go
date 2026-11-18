//go:build js && wasm

package jshttp

import (
	"net/http"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// panicHandler is an http.Handler that always panics before writing
// anything, used to verify ServeRequest's panic recovery.
type panicHandler struct{}

func (panicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	panic("boom")
}

func fakeGetRequest(t *testing.T, url string) js.Value {
	t.Helper()
	return jsutil.RequestClass.New(url, js.ValueOf(map[string]any{
		"method": "GET",
	}))
}

// awaitPromiseWithTimeout wraps jsutil.AwaitPromise with a bounded wait so a
// regression that makes the underlying stream hang fails the test instead of
// hanging the test binary forever.
func awaitPromiseWithTimeout(t *testing.T, promiseVal js.Value, timeout time.Duration) (js.Value, error) {
	t.Helper()
	type result struct {
		val js.Value
		err error
	}
	done := make(chan result, 1)
	go func() {
		val, err := jsutil.AwaitPromise(promiseVal)
		done <- result{val, err}
	}()
	select {
	case r := <-done:
		return r.val, r.err
	case <-time.After(timeout):
		t.Fatal("timed out waiting for promise to settle")
		return js.Value{}, nil
	}
}

// TestServeRequest_HandlerPanicBeforeWrite verifies that a panic raised by
// the http.Handler (before it writes anything) is recovered into a 500
// Response instead of crashing the wasm program and leaving ServeRequest's
// caller (and the JS side's Promise) hanging forever, and that the
// resulting Response's body can still be read to completion without
// hanging.
func TestServeRequest_HandlerPanicBeforeWrite(t *testing.T) {
	respObj, err := ServeRequest(panicHandler{}, fakeGetRequest(t, "http://test.invalid/"), nil)
	if err != nil {
		t.Fatalf("ServeRequest returned an error: %v", err)
	}
	if got, want := respObj.Get("status").Int(), http.StatusInternalServerError; got != want {
		t.Errorf("status = %d, want %d", got, want)
	}

	// Reading the body must terminate (it's fine for it to come back empty
	// or with an error) rather than hang forever.
	if _, err := awaitPromiseWithTimeout(t, respObj.Call("text"), 5*time.Second); err != nil {
		t.Logf("resp.text() rejected (acceptable, stream was aborted): %v", err)
	}
}

// TestServeRequest_HandlerPanicAfterWrite verifies that a panic raised
// *after* the handler already wrote a response is still recovered (no
// crash), but — since the status/headers were already committed to the JS
// side by the time it panics — the status is left as whatever the handler
// set before writing, not overridden to 500. It also verifies that
// consuming the (now-aborted) response body terminates instead of hanging.
func TestServeRequest_HandlerPanicAfterWrite(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		panic("boom after write")
	})

	respObj, err := ServeRequest(handler, fakeGetRequest(t, "http://test.invalid/"), nil)
	if err != nil {
		t.Fatalf("ServeRequest returned an error: %v", err)
	}
	if got, want := respObj.Get("status").Int(), http.StatusOK; got != want {
		t.Errorf("status = %d, want %d", got, want)
	}

	// The stream was aborted mid-read (writer.CloseWithError), so
	// resp.text() is expected to reject; the important thing is that it
	// settles at all within the timeout instead of hanging forever.
	if _, err := awaitPromiseWithTimeout(t, respObj.Call("text"), 5*time.Second); err == nil {
		t.Log("resp.text() resolved instead of rejecting; acceptable as long as it didn't hang")
	} else {
		t.Logf("resp.text() rejected as expected for an aborted stream: %v", err)
	}
}

// TestServeRequest_MalformedURL verifies that a request whose URL Go's
// net/url cannot parse (workerd passes request.url through without
// validating percent-escapes) is answered with 400 Bad Request, as Go's
// net/http server does for the same request-target, instead of returning
// an error that rejects the handler's Promise and surfaces as a 500. The
// http.Handler must not run for such a request.
func TestServeRequest_MalformedURL(t *testing.T) {
	for _, url := range []string{
		"http://test.invalid/healthz%zz",
		"http://test.invalid/%",
	} {
		t.Run(url, func(t *testing.T) {
			called := false
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			})
			respObj, err := ServeRequest(h, fakeGetRequest(t, url), nil)
			if err != nil {
				t.Fatalf("ServeRequest returned an error: %v", err)
			}
			if got, want := respObj.Get("status").Int(), http.StatusBadRequest; got != want {
				t.Errorf("status = %d, want %d", got, want)
			}
			if called {
				t.Error("handler was called for a request with an unparseable URL")
			}
		})
	}
}
