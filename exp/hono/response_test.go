package hono

import (
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeRawJSBody implements jsutil.RawJSBodyGetter so
// TestConvertBodyToJS_rawJSBodyGetter can verify convertBodyToJS's
// zero-copy path without needing a body that actually originated from a JS
// ReadableStream.
type fakeRawJSBody struct {
	io.ReadCloser
	raw js.Value
}

func (f *fakeRawJSBody) GetRawJSBody() js.Value {
	return f.raw
}

// TestConvertBodyToJS_rawJSBodyGetter verifies that convertBodyToJS returns
// a RawJSBodyGetter's underlying stream unchanged (the zero-copy path),
// instead of wrapping it in a new stream.
func TestConvertBodyToJS_rawJSBodyGetter(t *testing.T) {
	raw := jstest.ReadableStream([]byte("raw"))
	body := &fakeRawJSBody{ReadCloser: io.NopCloser(strings.NewReader("")), raw: raw}

	got := convertBodyToJS(body)
	if !got.Equal(raw) {
		t.Errorf("convertBodyToJS did not return the RawJSBodyGetter's stream unchanged")
	}
}

// TestConvertBodyToJS_reader verifies that convertBodyToJS wraps a plain
// io.ReadCloser (not a jsutil.RawJSBodyGetter) in a JS ReadableStream backed
// by that reader's content.
func TestConvertBodyToJS_reader(t *testing.T) {
	body := io.NopCloser(strings.NewReader("hello"))

	got := convertBodyToJS(body)
	if !got.InstanceOf(jsutil.ReadableStreamClass) {
		t.Errorf("convertBodyToJS(%T) = %v, want a ReadableStream", body, got)
	}
	// textFromStream (not jstest.ReadAll) is used here to read the body:
	// see its doc comment (middleware_js_test.go) for why jstest.ReadAll
	// cannot be used to check the content of a stream built by
	// jsutil.ConvertReaderToReadableStream.
	if s := textFromStream(t, got); s != "hello" {
		t.Errorf("stream content = %q, want %q", s, "hello")
	}
}

// TestNewJSResponse verifies NewJSResponse's defaults: a statusCode of 0
// leaves the Response at its own default status (200), and a nil
// http.Header still leaves the Response with a Headers object rather than
// undefined/null.
func TestNewJSResponse(t *testing.T) {
	body := io.NopCloser(strings.NewReader(""))

	res := NewJSResponse(body, 0, nil)

	if got := res.Get("status").Int(); got != http.StatusOK {
		t.Errorf("status = %d, want %d (default)", got, http.StatusOK)
	}
	if h := res.Get("headers"); h.IsUndefined() || h.IsNull() || !h.InstanceOf(jsutil.HeadersClass) {
		t.Errorf("headers = %v, want a Headers object", h)
	}
}
