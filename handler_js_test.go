//go:build js && wasm

package workers

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

// awaitRejected waits for p to settle and fails the test if it does not
// settle within 5 seconds or if it resolves instead of rejecting. It plays
// the same role as jstest.Await, but for the rejection path, which jstest
// does not expose (jstest.Await always calls t.Fatalf on rejection).
// It must only be called from outside of a js.FuncOf callback, for the same
// reasons documented on jstest.Await.
func awaitRejected(t testing.TB, p js.Value) error {
	t.Helper()
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
		if r.err == nil {
			t.Fatalf("promise resolved with %v, want it to reject", r.v)
		}
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out after 5s waiting for promise to reject")
		return nil
	}
}

// TestReady_callsImport verifies that Ready() reaches the
// //go:wasmimport workers ready import: the Node test runner
// (testdata/wasm/wasm_exec_node.js) increments context.readyCount each time
// that import is called. Other tests in this file also call Ready()
// (directly, or indirectly via Serve), so this compares the count before and
// after the call under test instead of asserting an absolute value.
func TestReady_callsImport(t *testing.T) {
	before := jstest.ReadyCount(t)
	Ready()
	got := jstest.ReadyCount(t) - before
	if got != 1 {
		t.Errorf("ReadyCount delta = %d, want 1", got)
	}
}

// TestHandleRequest_beforeServe verifies that calling handleRequest before
// ServeNonBlock/Serve has set httpHandler rejects with an error that points
// callers at Serve.
func TestHandleRequest_beforeServe(t *testing.T) {
	httpHandler = nil

	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	err := awaitRejected(t, p)
	if !strings.Contains(err.Error(), "Serve") {
		t.Errorf("error = %q, want it to mention Serve", err)
	}
}

// TestServe_blocksUntilDone verifies that Serve blocks until the response
// body of a handled request has been fully read, then returns. doneCh is
// closed at most once for the lifetime of the test binary (see doneOnce in
// handler_js.go), so this must be the only test in the package that
// observes Done()/Serve's return - and it must run before any other test
// drains a response body to completion, or doneCh will already be closed by
// the time this test's Serve() call reaches <-Done().
func TestServe_blocksUntilDone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// A background task that outlives the response (this is what
		// cloudflare.WaitUntil registers via jsutil.TrackBackgroundTask):
		// Done must not close - and Serve must not return - while it is
		// still running, or resuming the parked goroutine would hit
		// "Go program has already exited".
		jsutil.TrackBackgroundTask(func() {
			time.Sleep(300 * time.Millisecond)
		})
		w.Write([]byte("ok"))
	})

	serveReturned := make(chan struct{})
	go func() {
		Serve(mux)
		close(serveReturned)
	}()

	select {
	case <-serveReturned:
		t.Fatalf("Serve returned before the request was handled")
	case <-time.After(50 * time.Millisecond):
	}

	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	res := jstest.Await(t, p)
	if body := string(jstest.ReadAll(t, res.Get("body"))); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}

	// The response body is fully read, but the tracked background task is
	// still sleeping: Serve must keep blocking until it finishes.
	select {
	case <-serveReturned:
		t.Fatalf("Serve returned while a background task was still running")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case <-serveReturned:
	case <-time.After(5 * time.Second):
		t.Fatalf("Serve did not return after the background task finished")
	}
}

// TestHandleRequest_roundTrip drives handleRequest the way worker.mjs does:
// build a JS Request, invoke the "handleRequest" binding with it, await the
// resulting Promise<Response>, and check that the Response reflects what the
// registered http.Handler did with the *http.Request it received.
func TestHandleRequest_roundTrip(t *testing.T) {
	t.Run("method_and_url", func(t *testing.T) {
		var (
			gotMethod   string
			gotPath     string
			gotRawQuery string
			gotTrigger  js.Value
		)
		ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotRawQuery = r.URL.RawQuery
			gotTrigger = runtimecontext.MustExtractTriggerObj(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		reqObj := jstest.Request(t, "POST", "http://example.com/echo?x=1", nil, nil)
		p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
		res := jstest.Await(t, p)
		jstest.ReadAll(t, res.Get("body"))

		if gotMethod != "POST" {
			t.Errorf("Method = %q, want %q", gotMethod, "POST")
		}
		if gotPath != "/echo" {
			t.Errorf("URL.Path = %q, want %q", gotPath, "/echo")
		}
		if gotRawQuery != "x=1" {
			t.Errorf("URL.RawQuery = %q, want %q", gotRawQuery, "x=1")
		}
		if !gotTrigger.Equal(reqObj) {
			t.Errorf("runtimecontext.MustExtractTriggerObj(r.Context()) is not the original Request object")
		}
	})

	t.Run("request_headers", func(t *testing.T) {
		var gotRemoteAddr, gotHost string
		ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotRemoteAddr = r.RemoteAddr
			gotHost = r.Host
			w.WriteHeader(http.StatusOK)
		}))

		header := http.Header{}
		header.Set("Cf-Connecting-Ip", "203.0.113.5")
		header.Set("Host", "example.org")
		reqObj := jstest.Request(t, "GET", "http://example.com/", nil, header)
		p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
		res := jstest.Await(t, p)
		jstest.ReadAll(t, res.Get("body"))

		if gotRemoteAddr != "203.0.113.5" {
			t.Errorf("RemoteAddr = %q, want %q (from Cf-Connecting-Ip)", gotRemoteAddr, "203.0.113.5")
		}
		if gotHost != "example.org" {
			t.Errorf("Host = %q, want %q", gotHost, "example.org")
		}
	})

	t.Run("request_body", func(t *testing.T) {
		t.Run("with_content_length", func(t *testing.T) {
			var (
				gotBody          []byte
				gotContentLength int64
			)
			ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotContentLength = r.ContentLength
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("io.ReadAll(r.Body): %v", err)
				}
				gotBody = b
				w.WriteHeader(http.StatusOK)
			}))

			header := http.Header{}
			header.Set("Content-Length", "3")
			reqObj := jstest.Request(t, "POST", "http://example.com/", []byte{1, 2, 3}, header)
			p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
			res := jstest.Await(t, p)
			jstest.ReadAll(t, res.Get("body"))

			if gotContentLength != 3 {
				t.Errorf("ContentLength = %d, want 3", gotContentLength)
			}
			if !bytes.Equal(gotBody, []byte{1, 2, 3}) {
				t.Errorf("body = %v, want %v", gotBody, []byte{1, 2, 3})
			}
		})

		t.Run("without_content_length", func(t *testing.T) {
			var gotContentLength int64
			ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotContentLength = r.ContentLength
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))

			reqObj := jstest.Request(t, "POST", "http://example.com/", []byte{1, 2, 3}, nil)
			p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
			res := jstest.Await(t, p)
			jstest.ReadAll(t, res.Get("body"))

			if gotContentLength != -1 {
				t.Errorf("ContentLength = %d, want -1 (unknown, no Content-Length header)", gotContentLength)
			}
		})
	})

	t.Run("response_status_204", func(t *testing.T) {
		ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

		reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
		p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
		res := jstest.Await(t, p)

		if got := res.Get("status").Int(); got != http.StatusNoContent {
			t.Errorf("status = %d, want %d", got, http.StatusNoContent)
		}
		if !res.Get("body").IsNull() {
			t.Errorf("body = %v, want null", res.Get("body"))
		}
	})

	t.Run("response_headers", func(t *testing.T) {
		ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Multi", "a")
			w.Header().Add("X-Multi", "b")
			w.WriteHeader(http.StatusOK)
		}))

		reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
		p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
		res := jstest.Await(t, p)
		jstest.ReadAll(t, res.Get("body"))

		if got := res.Get("headers").Call("get", "X-Multi").String(); got != "a, b" {
			t.Errorf("X-Multi header = %q, want %q", got, "a, b")
		}
	})
}

// TestHandleRequest_streamingResponse verifies that the Promise returned by
// handleRequest resolves as soon as the handler's first Write (which calls
// ResponseWriter.Ready) has run, without waiting for the handler goroutine
// to finish writing the rest of the body.
func TestHandleRequest_streamingResponse(t *testing.T) {
	proceed := make(chan struct{})
	ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("first-"))
		w.(http.Flusher).Flush()
		<-proceed
		w.Write([]byte("second"))
	}))

	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	// If handleRequest waited for the handler goroutine to finish instead of
	// only its first Write, this would deadlock (the handler is blocked on
	// proceed, which is only closed below) and jstest.Await would time out
	// and call t.Fatalf.
	res := jstest.Await(t, p)
	close(proceed)

	if body := string(jstest.ReadAll(t, res.Get("body"))); body != "first-second" {
		t.Errorf("body = %q, want %q", body, "first-second")
	}
}

// TestHandleRequest_rawJSBody verifies the zero-copy path: when a handler
// copies a request body that originated from a JS ReadableStream straight to
// the ResponseWriter (which implements jsutil.RawJSBodyWriter), io.Copy uses
// readableStreamToReadCloser's WriteTo to hand the original JS stream
// through untouched, and the resulting Response is built from that same
// stream instance.
func TestHandleRequest_rawJSBody(t *testing.T) {
	ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(w, r.Body); err != nil {
			t.Errorf("io.Copy: %v", err)
		}
	}))

	stream := jstest.ReadableStream([]byte("hello"))
	init := jsutil.NewObject()
	init.Set("method", "POST")
	init.Set("body", stream)
	// The Fetch spec requires "duplex" when a Request body is a
	// ReadableStream; jstest.Request does not need it because it only ever
	// builds buffer bodies.
	init.Set("duplex", "half")
	reqObj := jsutil.RequestClass.New("http://example.com/", init)

	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	res := jstest.Await(t, p)

	if gotBody := res.Get("body"); !gotBody.Equal(stream) {
		t.Errorf("response body is not the same ReadableStream instance that was passed in as the request body")
	}
}

// TestHandleRequest_tooManyArgs verifies that calling the "handleRequest"
// binding with more than one argument rejects instead of silently ignoring
// the extra argument (handler_js.go's handleRequestCallback).
func TestHandleRequest_tooManyArgs(t *testing.T) {
	ServeNonBlock(http.NewServeMux())

	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj, js.ValueOf("extra"))
	err := awaitRejected(t, p)
	if !strings.Contains(err.Error(), "too many args") {
		t.Errorf("error = %q, want it to mention too many args", err)
	}
}

// TestHandleRequest_panicInHandler verifies that a panic inside the
// registered http.Handler is recovered (see jshttp.ServeRequest's deferred
// recover in internal/jshttp/serve.go) and turned into a 500 response
// instead of crashing the whole wasm process, unlike the bare, unrecovered
// goroutine this used to run in. See tmp/test-plan/03-binding-contract-tests.md
// §4 for the original repro.
func TestHandleRequest_panicInHandler(t *testing.T) {
	ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	res := jstest.Await(t, p)

	if got := res.Get("status").Int(); got != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", got, http.StatusInternalServerError)
	}
}

// TestHandleRequest_malformedURL verifies that a request with a malformed
// percent-escape in its path (which workerd forwards verbatim, but Go's
// url.Parse rejects) resolves with 400 Bad Request, like Go's net/http
// server, rather than rejecting the handleRequest promise (a 500 from the
// runtime) before the registered handler gets a chance to run.
func TestHandleRequest_malformedURL(t *testing.T) {
	called := false
	ServeNonBlock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	reqObj := jstest.Request(t, "GET", "http://example.com/healthz%zz", nil, nil)
	p := jstest.Binding(t, "handleRequest").Invoke(reqObj)
	res := jstest.Await(t, p)

	if got := res.Get("status").Int(); got != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", got, http.StatusBadRequest)
	}
	if called {
		t.Error("handler was called for a request with an unparseable URL")
	}
}
