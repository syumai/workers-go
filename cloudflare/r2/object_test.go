//go:build js && wasm

package r2

import (
	"io"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

func TestToObject(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		uploaded := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		cacheExpiry := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
		v := js.ValueOf(map[string]any{
			"key":      "path/to/object",
			"version":  "v1",
			"size":     12,
			"etag":     "abc123",
			"httpEtag": `"abc123"`,
			"uploaded": jsutil.TimeToDate(uploaded),
			"httpMetadata": map[string]any{
				"contentType": "text/plain",
				"cacheExpiry": jsutil.TimeToDate(cacheExpiry),
			},
			"customMetadata": map[string]any{"foo": "bar"},
			"body":           jstest.ReadableStream([]byte("hello")),
		})

		obj, err := toObject(v)
		if err != nil {
			t.Fatalf("toObject: %v", err)
		}
		if obj.Key != "path/to/object" {
			t.Errorf("Key = %q, want %q", obj.Key, "path/to/object")
		}
		if obj.Version != "v1" {
			t.Errorf("Version = %q, want %q", obj.Version, "v1")
		}
		if obj.Size != 12 {
			t.Errorf("Size = %d, want 12", obj.Size)
		}
		if obj.ETag != "abc123" {
			t.Errorf("ETag = %q, want %q", obj.ETag, "abc123")
		}
		if obj.HTTPETag != `"abc123"` {
			t.Errorf("HTTPETag = %q, want %q", obj.HTTPETag, `"abc123"`)
		}
		if !obj.Uploaded.Equal(uploaded) {
			t.Errorf("Uploaded = %v, want %v", obj.Uploaded, uploaded)
		}
		if obj.HTTPMetadata.ContentType != "text/plain" {
			t.Errorf("HTTPMetadata.ContentType = %q, want %q", obj.HTTPMetadata.ContentType, "text/plain")
		}
		if !obj.HTTPMetadata.CacheExpiry.Equal(cacheExpiry) {
			t.Errorf("HTTPMetadata.CacheExpiry = %v, want %v", obj.HTTPMetadata.CacheExpiry, cacheExpiry)
		}
		if obj.CustomMetadata["foo"] != "bar" {
			t.Errorf("CustomMetadata[foo] = %q, want %q", obj.CustomMetadata["foo"], "bar")
		}
		if obj.Body == nil {
			t.Fatalf("Body is nil, want a reader")
		}
		got, err := io.ReadAll(obj.Body)
		if err != nil {
			t.Fatalf("io.ReadAll(Body): %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("Body content = %q, want %q", got, "hello")
		}
	})

	t.Run("metadata_undefined", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"key":      "k",
			"version":  "v1",
			"size":     0,
			"etag":     "e",
			"httpEtag": "e",
			"uploaded": jsutil.TimeToDate(time.Unix(0, 0)),
		})

		obj, err := toObject(v)
		if err != nil {
			t.Fatalf("toObject: %v", err)
		}
		if obj.HTTPMetadata != (HTTPMetadata{}) {
			t.Errorf("HTTPMetadata = %+v, want zero value", obj.HTTPMetadata)
		}
		if len(obj.CustomMetadata) != 0 {
			t.Errorf("CustomMetadata = %+v, want empty", obj.CustomMetadata)
		}
	})

	t.Run("body_absent_head_like", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"key":      "k",
			"version":  "v1",
			"size":     0,
			"etag":     "e",
			"httpEtag": "e",
			"uploaded": jsutil.TimeToDate(time.Unix(0, 0)),
		})

		obj, err := toObject(v)
		if err != nil {
			t.Fatalf("toObject: %v", err)
		}
		if obj.Body != nil {
			t.Errorf("Body = %v, want nil for a head-like object", obj.Body)
		}
	})
}

func TestToHTTPMetadata(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		expiry := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
		v := js.ValueOf(map[string]any{
			"contentType":        "text/plain",
			"contentLanguage":    "en",
			"contentDisposition": "inline",
			"contentEncoding":    "gzip",
			"cacheControl":       "max-age=100",
			"cacheExpiry":        jsutil.TimeToDate(expiry),
		})
		md, err := toHTTPMetadata(v)
		if err != nil {
			t.Fatalf("toHTTPMetadata: %v", err)
		}
		want := HTTPMetadata{
			ContentType:        "text/plain",
			ContentLanguage:    "en",
			ContentDisposition: "inline",
			ContentEncoding:    "gzip",
			CacheControl:       "max-age=100",
		}
		if md.ContentType != want.ContentType ||
			md.ContentLanguage != want.ContentLanguage ||
			md.ContentDisposition != want.ContentDisposition ||
			md.ContentEncoding != want.ContentEncoding ||
			md.CacheControl != want.CacheControl {
			t.Errorf("toHTTPMetadata() = %+v, want string fields %+v", md, want)
		}
		if !md.CacheExpiry.Equal(expiry) {
			t.Errorf("CacheExpiry = %v, want %v", md.CacheExpiry, expiry)
		}
	})

	t.Run("undefined", func(t *testing.T) {
		md, err := toHTTPMetadata(js.Undefined())
		if err != nil {
			t.Fatalf("toHTTPMetadata: %v", err)
		}
		if md != (HTTPMetadata{}) {
			t.Errorf("toHTTPMetadata(undefined) = %+v, want zero value", md)
		}
	})

	t.Run("null", func(t *testing.T) {
		md, err := toHTTPMetadata(jsutil.Null)
		if err != nil {
			t.Fatalf("toHTTPMetadata: %v", err)
		}
		if md != (HTTPMetadata{}) {
			t.Errorf("toHTTPMetadata(null) = %+v, want zero value", md)
		}
	})
}

// TestHTTPMetadata_toJS checks HTTPMetadata.toJS() by round-tripping the
// result back through toHTTPMetadata, and checks that a zero CacheExpiry is
// omitted from the JS object rather than encoded as some placeholder date.
func TestHTTPMetadata_toJS(t *testing.T) {
	tests := map[string]struct {
		md HTTPMetadata
	}{
		"zero": {md: HTTPMetadata{}},
		"strings_only": {md: HTTPMetadata{
			ContentType:        "text/plain",
			ContentLanguage:    "en",
			ContentDisposition: "inline",
			ContentEncoding:    "gzip",
			CacheControl:       "max-age=100",
		}},
		"with_cache_expiry": {md: HTTPMetadata{
			CacheControl: "max-age=100",
			CacheExpiry:  time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC),
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.md.toJS()

			if tt.md.CacheExpiry.IsZero() {
				if v := got.Get("cacheExpiry"); !v.IsUndefined() {
					t.Errorf("cacheExpiry = %v, want omitted for a zero CacheExpiry", v)
				}
			}

			roundTripped, err := toHTTPMetadata(got)
			if err != nil {
				t.Fatalf("toHTTPMetadata: %v", err)
			}
			if roundTripped.ContentType != tt.md.ContentType ||
				roundTripped.ContentLanguage != tt.md.ContentLanguage ||
				roundTripped.ContentDisposition != tt.md.ContentDisposition ||
				roundTripped.ContentEncoding != tt.md.ContentEncoding ||
				roundTripped.CacheControl != tt.md.CacheControl ||
				!roundTripped.CacheExpiry.Equal(tt.md.CacheExpiry) {
				t.Errorf("round trip = %+v, want %+v", roundTripped, tt.md)
			}
		})
	}
}

func TestPutOptions_toJS(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var opts *PutOptions
		got := opts.toJS()
		if !got.IsUndefined() {
			t.Fatalf("toJS() = %v, want undefined", got)
		}
	})

	t.Run("zero_value_omitted", func(t *testing.T) {
		got := (&PutOptions{}).toJS()
		jstest.AssertObjectEqual(t, got, map[string]any{})
	})

	t.Run("http_metadata", func(t *testing.T) {
		opts := &PutOptions{HTTPMetadata: HTTPMetadata{ContentType: "text/plain"}}
		got := opts.toJS()
		md := got.Get("httpMetadata")
		if md.IsUndefined() {
			t.Fatalf("httpMetadata is undefined, want an object")
		}
		if md.Get("contentType").String() != "text/plain" {
			t.Errorf("httpMetadata.contentType = %v, want %q", md.Get("contentType"), "text/plain")
		}
	})

	t.Run("custom_metadata", func(t *testing.T) {
		opts := &PutOptions{CustomMetadata: map[string]string{"foo": "bar", "baz": "qux"}}
		got := opts.toJS()
		cm := got.Get("customMetadata")
		if cm.IsUndefined() {
			t.Fatalf("customMetadata is undefined, want an object")
		}
		if cm.Get("foo").String() != "bar" || cm.Get("baz").String() != "qux" {
			t.Errorf("customMetadata = {foo: %v, baz: %v}, want {foo: bar, baz: qux}", cm.Get("foo"), cm.Get("baz"))
		}
	})

	t.Run("md5", func(t *testing.T) {
		opts := &PutOptions{MD5: "deadbeef"}
		got := opts.toJS()
		if got.Get("md5").String() != "deadbeef" {
			t.Errorf("md5 = %v, want %q", got.Get("md5"), "deadbeef")
		}
	})
}
