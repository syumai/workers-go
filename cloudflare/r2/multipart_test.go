//go:build js && wasm

package r2

import (
	"errors"
	"io"
	"strings"
	"syscall/js"
	"testing"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"
)

func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestMultipartUpload_UploadPart verifies UploadPart passes the requested
// partNumber through and sends the io.Reader's bytes as an ArrayBuffer.
// The generated binding reads the reader to completion because workerd
// rejects a ReadableStream without a known length. The decoded
// *UploadedPart carries back the fake's partNumber/etag.
func TestMultipartUpload_UploadPart(t *testing.T) {
	var gotPartNumber int
	var gotValue js.Value

	fake := js.ValueOf(map[string]any{
		"key":      "my-key",
		"uploadId": "upload-1",
	})
	fake.Set("uploadPart", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotPartNumber = args[0].Int()
		gotValue = args[1]
		return resolve(map[string]any{
			"partNumber": args[0],
			"etag":       "etag-part-1",
		})
	}))

	upload := toMultipartUpload(r2js.R2MultipartUploadFromJS(fake))
	if upload.Key != "my-key" || upload.UploadID != "upload-1" {
		t.Fatalf("toMultipartUpload() = %+v, want Key=my-key UploadID=upload-1", upload)
	}

	part, err := upload.UploadPart(3, strings.NewReader("part body"))
	if err != nil {
		t.Fatalf("UploadPart() failed: %v", err)
	}
	if gotPartNumber != 3 {
		t.Errorf("uploadPart received partNumber = %d, want 3", gotPartNumber)
	}
	if !gotValue.InstanceOf(js.Global().Get("ArrayBuffer")) {
		t.Fatalf("UploadPart() passed %v, want an ArrayBuffer", gotValue)
	}
	u8 := js.Global().Get("Uint8Array").New(gotValue)
	gotBytes := make([]byte, u8.Get("byteLength").Int())
	js.CopyBytesToGo(gotBytes, u8)
	if string(gotBytes) != "part body" {
		t.Errorf("uploadPart body = %q, want %q", gotBytes, "part body")
	}
	if part.PartNumber != 3 {
		t.Errorf("part.PartNumber = %d, want 3", part.PartNumber)
	}
	if part.ETag != "etag-part-1" {
		t.Errorf("part.ETag = %q, want %q", part.ETag, "etag-part-1")
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// TestMultipartUpload_UploadPart_readError verifies a reader failure is
// returned and does not call uploadPart.
func TestMultipartUpload_UploadPart_readError(t *testing.T) {
	called := false
	fake := js.ValueOf(map[string]any{
		"key":      "my-key",
		"uploadId": "upload-1",
	})
	fake.Set("uploadPart", js.FuncOf(func(this js.Value, args []js.Value) any {
		called = true
		return resolve(map[string]any{"partNumber": 1, "etag": "unused"})
	}))

	upload := toMultipartUpload(r2js.R2MultipartUploadFromJS(fake))
	want := errors.New("read failed")
	_, err := upload.UploadPart(1, errReader{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("UploadPart() error = %v, want %v", err, want)
	}
	if called {
		t.Fatal("uploadPart was called after the reader failed")
	}
}

// closerReader records whether Close ran, so the generated binding's
// ArrayBuffer conversion is checked for closing an io.Closer.
type closerReader struct {
	io.Reader
	closed bool
}

func (c *closerReader) Close() error {
	c.closed = true
	return nil
}

func TestMultipartUpload_UploadPart_closesReader(t *testing.T) {
	fake := js.ValueOf(map[string]any{
		"key":      "my-key",
		"uploadId": "upload-1",
	})
	fake.Set("uploadPart", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(map[string]any{"partNumber": 1, "etag": "etag"})
	}))
	body := &closerReader{Reader: strings.NewReader("x")}
	upload := toMultipartUpload(r2js.R2MultipartUploadFromJS(fake))
	if _, err := upload.UploadPart(1, body); err != nil {
		t.Fatalf("UploadPart() failed: %v", err)
	}
	if !body.closed {
		t.Fatal("UploadPart() did not close the reader")
	}
}
