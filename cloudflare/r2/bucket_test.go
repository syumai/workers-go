//go:build js && wasm

package r2

import (
	"bytes"
	"io"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestNewBucket_undefined(t *testing.T) {
	jstest.SetEnv(t, map[string]any{})

	b, err := NewBucket("BUCKET")
	if err == nil {
		t.Fatalf("NewBucket() = %v, want error", b)
	}
}

func TestBucket_Put_Get_roundTrip(t *testing.T) {
	fb := newFakeBucket(t)
	jstest.SetEnv(t, map[string]any{"BUCKET": fb.value})

	b, err := NewBucket("BUCKET")
	if err != nil {
		t.Fatalf("NewBucket: %v", err)
	}

	const content = "hello r2"
	putOpts := &PutOptions{
		HTTPMetadata:   HTTPMetadata{ContentType: "text/plain"},
		CustomMetadata: map[string]string{"foo": "bar"},
	}
	putObj, err := b.Put("key1", io.NopCloser(bytes.NewReader([]byte(content))), putOpts)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if putObj.Body != nil {
		t.Errorf("Put() result Body = %v, want nil", putObj.Body)
	}

	got, err := b.Get("key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatalf("Get() = nil, want an object")
	}
	if got.Body == nil {
		t.Fatalf("Get() result Body is nil, want a reader")
	}
	body, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(Body): %v", err)
	}
	if string(body) != content {
		t.Errorf("Body content = %q, want %q", body, content)
	}
	if got.HTTPMetadata.ContentType != "text/plain" {
		t.Errorf("HTTPMetadata.ContentType = %q, want %q", got.HTTPMetadata.ContentType, "text/plain")
	}
	if got.CustomMetadata["foo"] != "bar" {
		t.Errorf("CustomMetadata[foo] = %q, want %q", got.CustomMetadata["foo"], "bar")
	}
}

func TestBucket_Head(t *testing.T) {
	fb := newFakeBucket(t)
	jstest.SetEnv(t, map[string]any{"BUCKET": fb.value})

	b, err := NewBucket("BUCKET")
	if err != nil {
		t.Fatalf("NewBucket: %v", err)
	}

	if _, err := b.Put("key1", io.NopCloser(bytes.NewReader([]byte("hello"))), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}

	obj, err := b.Head("key1")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if obj == nil {
		t.Fatalf("Head() = nil, want an object")
	}
	if obj.Key != "key1" {
		t.Errorf("Key = %q, want %q", obj.Key, "key1")
	}
	if obj.Size != len("hello") {
		t.Errorf("Size = %d, want %d", obj.Size, len("hello"))
	}
	if obj.Body != nil {
		t.Errorf("Head() result Body = %v, want nil", obj.Body)
	}
}

// TestBucket_Get_missing fixes the current behavior of Get on a miss: the
// real R2 get() resolves with null, and Bucket.Get checks v.IsNull() before
// converting, returning (nil, nil). Unlike kv.Namespace.GetString (see its
// TestNamespace_GetString_missing), this does not panic or produce a
// placeholder value.
func TestBucket_Get_missing(t *testing.T) {
	fb := newFakeBucket(t)
	jstest.SetEnv(t, map[string]any{"BUCKET": fb.value})

	b, err := NewBucket("BUCKET")
	if err != nil {
		t.Fatalf("NewBucket: %v", err)
	}

	obj, err := b.Get("missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if obj != nil {
		t.Errorf("Get(missing) = %v, want nil", obj)
	}
}

func TestBucket_Delete(t *testing.T) {
	fb := newFakeBucket(t)
	jstest.SetEnv(t, map[string]any{"BUCKET": fb.value})

	b, err := NewBucket("BUCKET")
	if err != nil {
		t.Fatalf("NewBucket: %v", err)
	}

	if _, err := b.Put("key1", io.NopCloser(bytes.NewReader([]byte("hello"))), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !fb.has("key1") {
		t.Fatalf("fakeBucket: key1 not stored after Put")
	}
	if err := b.Delete("key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fb.has("key1") {
		t.Errorf("fakeBucket: key1 still stored after Delete")
	}
}

func TestBucket_List(t *testing.T) {
	fb := newFakeBucket(t)
	jstest.SetEnv(t, map[string]any{"BUCKET": fb.value})

	b, err := NewBucket("BUCKET")
	if err != nil {
		t.Fatalf("NewBucket: %v", err)
	}

	for _, k := range []string{"a", "b"} {
		if _, err := b.Put(k, io.NopCloser(bytes.NewReader([]byte(k))), nil); err != nil {
			t.Fatalf("Put(%q): %v", k, err)
		}
	}

	objs, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs.Objects) != 2 {
		t.Fatalf("len(Objects) = %d, want 2", len(objs.Objects))
	}
	if objs.Objects[0].Key != "a" || objs.Objects[1].Key != "b" {
		t.Errorf("Objects keys = [%q, %q], want [a, b]", objs.Objects[0].Key, objs.Objects[1].Key)
	}
	if objs.Truncated {
		t.Errorf("Truncated = true, want false")
	}
}

func TestObject_BodyUsed(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		v := js.ValueOf(map[string]any{"bodyUsed": true})
		obj := &Object{instance: v}
		got, err := obj.BodyUsed()
		if err != nil {
			t.Fatalf("BodyUsed: %v", err)
		}
		if !got {
			t.Errorf("BodyUsed() = false, want true")
		}
	})

	t.Run("missing", func(t *testing.T) {
		v := js.ValueOf(map[string]any{})
		obj := &Object{instance: v}
		_, err := obj.BodyUsed()
		if err == nil {
			t.Fatalf("BodyUsed() error = nil, want an error")
		}
	})
}
