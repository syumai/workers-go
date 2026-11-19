//go:build js && wasm

package fetch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// TestClient_Do_invalidURL relies on Node's global fetch() rejecting for a
// scheme it doesn't support (only http/https are handled), so this never
// actually reaches the network.
func TestClient_Do_invalidURL(t *testing.T) {
	c := NewClient()
	req, err := NewRequest(context.Background(), http.MethodGet, "foo://bar", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := c.Do(req, nil); err == nil {
		t.Fatal("Do() error = nil, want error")
	}
}

func TestClient_Do_withBindingFake(t *testing.T) {
	wantBody := []byte("hello from fake")
	resVal := jstest.Response(t, http.StatusCreated, wantBody, http.Header{"X-From-Fake": {"1"}})
	fetcher := newFakeFetcher(t, resVal)

	c := NewClient(WithBinding(fetcher.val))
	wantReqBody := []byte("request payload")
	req, err := NewRequest(context.Background(), http.MethodPost, "https://example.com/path", bytes.NewReader(wantReqBody))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("X-Test", "1")

	res, err := c.Do(req, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer res.Body.Close()

	gotReq := fetcher.lastRequest()
	if got, want := gotReq.Get("url").String(), "https://example.com/path"; got != want {
		t.Errorf("request url = %q, want %q", got, want)
	}
	if got, want := gotReq.Get("method").String(), http.MethodPost; got != want {
		t.Errorf("request method = %q, want %q", got, want)
	}
	if got, want := gotReq.Get("headers").Call("get", "X-Test").String(), "1"; got != want {
		t.Errorf("request header X-Test = %q, want %q", got, want)
	}
	v, err := jsutil.AwaitPromise(gotReq.Call("text"))
	if err != nil {
		t.Fatalf("request.text() error = %v, want nil", err)
	}
	if got := v.String(); got != string(wantReqBody) {
		t.Errorf("request body = %q, want %q", got, wantReqBody)
	}

	if res.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func TestClient_HTTPClient_redirectMode(t *testing.T) {
	resVal := jstest.Response(t, http.StatusOK, []byte{}, nil)
	fetcher := newFakeFetcher(t, resVal)

	c := NewClient(WithBinding(fetcher.val))
	httpClient := c.HTTPClient(RedirectModeManual)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("httpClient.Do() error = %v", err)
	}
	res.Body.Close()

	gotInit := fetcher.lastInit()
	if got, want := gotInit.Get("redirect").String(), string(RedirectModeManual); got != want {
		t.Errorf("init.redirect = %q, want %q", got, want)
	}
}

// hangingFetcher is a fake binding whose fetch() never settles on its own: it
// only rejects with an AbortError when the signal in init is aborted. This is
// what a malicious server that never answers looks like to the runtime.
func newHangingFetcher(t testing.TB, signals chan<- js.Value) js.Value {
	t.Helper()
	fetchFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		var executor js.Func
		executor = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer executor.Release()
			reject := pArgs[1]
			if len(args) > 1 && args[1].Type() == js.TypeObject {
				signal := args[1].Get("signal")
				if signal.Type() == js.TypeObject {
					signals <- signal
					var onAbort js.Func
					onAbort = js.FuncOf(func(js.Value, []js.Value) any {
						onAbort.Release()
						reject.Invoke(jsutil.Error("AbortError"))
						return js.Undefined()
					})
					signal.Call("addEventListener", "abort", onAbort)
				}
			}
			return js.Undefined()
		})
		return jsutil.NewPromise(executor)
	})
	obj := jsutil.NewObject()
	obj.Set("fetch", fetchFn)
	return obj
}

func TestClient_Do_contextCancelAbortsPendingFetch(t *testing.T) {
	signals := make(chan js.Value, 1)
	c := NewClient(WithBinding(newHangingFetcher(t, signals)))

	ctx, cancel := context.WithCancel(context.Background())
	req, err := NewRequest(ctx, http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Do(req, nil)
		errCh <- err
	}()

	signal := <-signals
	if signal.Get("aborted").Bool() {
		t.Fatal("signal aborted before cancel")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Do() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Do() still blocked 5s after context cancel")
	}
	// The abort is delivered by a separate goroutine; give it a moment.
	deadline := time.Now().Add(5 * time.Second)
	for !signal.Get("aborted").Bool() {
		if time.Now().After(deadline) {
			t.Fatal("AbortSignal was never aborted")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestClient_HTTPClient_timeoutAbortsPendingFetch(t *testing.T) {
	signals := make(chan js.Value, 1)
	c := NewClient(WithBinding(newHangingFetcher(t, signals)))
	httpClient := c.HTTPClient(RedirectModeFollow)
	httpClient.Timeout = 50 * time.Millisecond

	req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := httpClient.Do(req)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Do() error = nil, want timeout")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Do() still blocked long after Client.Timeout")
	}
}

func TestClient_Do_alreadyCanceledContext(t *testing.T) {
	signals := make(chan js.Value, 1)
	c := NewClient(WithBinding(newHangingFetcher(t, signals)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := NewRequest(ctx, http.MethodGet, "https://example.com/", nil)
	if _, err := c.Do(req, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Do() error = %v, want context.Canceled", err)
	}
}

func TestClient_Do_noSignalForBackgroundContext(t *testing.T) {
	resVal := jstest.Response(t, http.StatusOK, []byte("ok"), nil)
	fetcher := newFakeFetcher(t, resVal)
	c := NewClient(WithBinding(fetcher.val))
	req, _ := NewRequest(context.Background(), http.MethodGet, "https://example.com/", nil)
	res, err := c.Do(req, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	res.Body.Close()
	if init := fetcher.lastInit(); init.Type() == js.TypeObject && init.Get("signal").Type() == js.TypeObject {
		t.Error("init.signal set for a context that can never be canceled")
	}
}

func TestClient_Do_contextCancelInterruptsBodyRead(t *testing.T) {
	// A response whose body delivers one chunk and then stalls forever.
	bodyInit := jsutil.NewObject()
	bodyInit.Set("start", jstest.Func(t, func(_ js.Value, args []js.Value) any {
		args[0].Call("enqueue", jstest.Uint8Array([]byte("partial")))
		return js.Undefined()
	}))
	stalled := jsutil.ReadableStreamClass.New(bodyInit)
	resInit := jsutil.NewObject()
	resInit.Set("status", 200)
	resVal := jsutil.ResponseClass.New(stalled, resInit)
	fetcher := newFakeFetcher(t, resVal)

	c := NewClient(WithBinding(fetcher.val))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := NewRequest(ctx, http.MethodGet, "https://example.com/", nil)
	res, err := c.Do(req, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer res.Body.Close()

	buf := make([]byte, 64)
	n, err := res.Body.Read(buf)
	if err != nil || string(buf[:n]) != "partial" {
		t.Fatalf("first Read() = %q, %v; want \"partial\", nil", buf[:n], err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := res.Body.Read(buf)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Read() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Body.Read still blocked 5s after context cancel")
	}
}
