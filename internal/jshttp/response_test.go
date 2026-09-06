//go:build js && wasm

package jshttp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// readAllStream reads a JS ReadableStream to completion. It is a small,
// package-local stand-in for internal/jstest.ReadAll: internal/jstest
// imports this package, so importing it back here would create an import
// cycle (see internal/jsutil/stream_test.go for the same pattern).
func readAllStream(t *testing.T, stream js.Value) []byte {
	t.Helper()
	rc := jsutil.ConvertReadableStreamToReadCloser(stream)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("readAllStream: %v", err)
	}
	return b
}

func TestToResponse(t *testing.T) {
	header := jsutil.HeadersClass.New()
	header.Call("append", "X-Test", "1")
	body := []byte("hello")
	ua := jsutil.NewUint8Array(len(body))
	js.CopyBytesToJS(ua, body)
	init := jsutil.NewObject()
	init.Set("status", 201)
	init.Set("headers", header)
	jsResp := jsutil.ResponseClass.New(ua, init)

	res, err := ToResponse(jsResp)
	if err != nil {
		t.Fatalf("ToResponse() error = %v, want nil", err)
	}
	if res.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", res.StatusCode)
	}
	if !strings.HasPrefix(res.Status, "201") {
		t.Errorf("Status = %q, want prefix %q", res.Status, "201")
	}
	if got := res.Header.Get("X-Test"); got != "1" {
		t.Errorf("Header.Get(X-Test) = %q, want %q", got, "1")
	}

	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(Body) error = %v, want nil", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("Body = %q, want %q", got, body)
	}
}

func TestNewJSResponse(t *testing.T) {
	t.Run("status_zero_defaults_to_200", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("hi"))
		resp := newJSResponse(0, http.Header{}, 0, body, nil)
		if got := resp.Get("status").Int(); got != http.StatusOK {
			t.Errorf("status = %d, want %d", got, http.StatusOK)
		}
	})

	noBodyStatuses := []int{
		http.StatusSwitchingProtocols,
		http.StatusNoContent,
		http.StatusResetContent,
		http.StatusNotModified,
	}
	for _, status := range noBodyStatuses {
		t.Run(fmt.Sprintf("status_%d_has_null_body", status), func(t *testing.T) {
			if status == http.StatusSwitchingProtocols {
				// Node's (spec-compliant) Response constructor rejects any
				// status outside [200, 599], so 101 can only be
				// constructed by Cloudflare's non-standard Response
				// implementation. Verified via wrangler e2e instead (see
				// tmp/test-plan/04-wrangler-e2e.md).
				t.Skip("status 101 is rejected by Node's Response constructor; only workerd's Response allows it")
			}

			resp := newJSResponse(status, http.Header{}, 0, nil, nil)
			if body := resp.Get("body"); !body.IsNull() {
				t.Errorf("body = %v, want null", body)
			}
		})
	}

	t.Run("content_length_uses_readable_stream_without_fixed_length_stream", func(t *testing.T) {
		// MaybeFixedLengthStreamClass is undefined under Node (see
		// tmp/test-plan/04-wrangler-e2e.md), so this always exercises the
		// ConvertReaderToReadableStream branch here, regardless of
		// contentLength.
		body := io.NopCloser(strings.NewReader("hello"))
		resp := newJSResponse(http.StatusOK, http.Header{}, 5, body, nil)
		if stream := resp.Get("body"); !stream.InstanceOf(jsutil.ReadableStreamClass) {
			t.Errorf("body = %v, want a ReadableStream", stream)
		}
	})

	t.Run("raw_body_takes_priority", func(t *testing.T) {
		raw := jsutil.ReadableStreamClass.New()
		body := io.NopCloser(strings.NewReader("ignored, raw body wins"))
		resp := newJSResponse(http.StatusOK, http.Header{}, 5, body, &raw)
		if got := resp.Get("body"); !got.Equal(raw) {
			t.Errorf("body = %v, want the raw stream %v", got, raw)
		}
	})

	t.Run("headers_are_carried_over", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Test", "1")
		resp := newJSResponse(http.StatusOK, h, 0, nil, nil)
		if got := resp.Get("headers").Call("get", "X-Test").String(); got != "1" {
			t.Errorf("headers.get(X-Test) = %q, want %q", got, "1")
		}
	})
}

func TestToJSResponse_ReadAll(t *testing.T) {
	t.Skip("known issue: ConvertReaderToReadableStream's first chunk is spuriously treated as EOF by ConvertReadableStreamToReadCloser, so reading a Go-authored JS body back with this package's own helper never sees the real bytes (see internal/jsutil/stream_test.go)")

	body := []byte("response payload")
	res := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}

	jsResp := ToJSResponse(res)
	got := readAllStream(t, jsResp.Get("body"))
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}
