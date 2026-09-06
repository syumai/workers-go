//go:build js && wasm

package fetch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
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
	// No request body: jshttp.ToJSRequest streams a non-nil body as a
	// ReadableStream, and Node's Request/fetch reject a streamed body
	// without an explicit `duplex: "half"` option (which ToJSRequest does
	// not set). That is a separate, pre-existing gap; this test sticks to
	// a bodyless request so it exercises method/header/URL forwarding.
	req, err := NewRequest(context.Background(), http.MethodPost, "https://example.com/path", nil)
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
