//go:build js && wasm

package r2

import (
	"syscall/js"
	"testing"
)

func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// fakeR2Object builds a JS object shaped enough like R2Object to exercise
// the fields R2ObjectBody inherits via handle-extends flattening (item 4),
// including the int-overridden size (applied to both R2Object.size and
// R2ObjectBody.size, since a types: override doesn't itself follow
// inheritance).
func fakeR2Object(key string, size int) map[string]any {
	return map[string]any{
		"key":     key,
		"version": "v1",
		"size":    size,
		"etag":    "etag-1",
	}
}

// TestR2ObjectBody_InheritsR2Object verifies R2ObjectBody's getters include
// ones it only has via extending R2Object (Key, Size), alongside its own
// (Text).
func TestR2ObjectBody_InheritsR2Object(t *testing.T) {
	fake := js.ValueOf(fakeR2Object("my-key", 42))
	fake.Set("text", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve("body text")
	}))

	body := R2ObjectBodyFromJS(fake)
	if got := body.Key(); got != "my-key" {
		t.Errorf("Key() = %q, want %q (inherited from R2Object)", got, "my-key")
	}
	if got := body.Size(); got != 42 {
		t.Errorf("Size() = %d, want 42 (inherited from R2Object, int-overridden)", got)
	}
	text, err := body.Text()
	if err != nil {
		t.Fatalf("Text() failed: %v", err)
	}
	if text != "body text" {
		t.Errorf("Text() = %q, want %q", text, "body text")
	}
}

// TestR2Bucket_Head_NilOnNull verifies R2Bucket.Head's Promise<R2Object |
// null> return decodes a JS null to a nil *R2Object (item 3/4: a nullable
// handle return needs a guard, not just NameFromJS(null)).
func TestR2Bucket_Head_NilOnNull(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("head", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(nil)
	}))

	bucket := R2BucketFromJS(fake)
	obj, err := bucket.Head("missing")
	if err != nil {
		t.Fatalf("Head() failed: %v", err)
	}
	if obj != nil {
		t.Errorf("Head() = %+v, want nil", obj)
	}
}

// TestR2Bucket_Head_NonNil verifies a resolved object decodes to a non-nil
// *R2Object with fields intact.
func TestR2Bucket_Head_NonNil(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("head", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(fakeR2Object("found", 7))
	}))

	bucket := R2BucketFromJS(fake)
	obj, err := bucket.Head("found")
	if err != nil {
		t.Fatalf("Head() failed: %v", err)
	}
	if obj == nil {
		t.Fatal("Head() = nil, want a non-nil *R2Object")
	}
	if obj.Key() != "found" {
		t.Errorf("Key() = %q, want %q", obj.Key(), "found")
	}
}

// TestR2ObjectsFromJS verifies R2Objects' intersection + union merge
// (item 5: the object-literal operand's fields alongside the
// truncated:true|false discriminant union's fields) decodes into one flat
// struct, for both branches of the union.
func TestR2ObjectsFromJS(t *testing.T) {
	truncated := js.ValueOf(map[string]any{
		"objects":           []any{fakeR2Object("a", 1)},
		"delimitedPrefixes": []any{"p/"},
		"truncated":         true,
		"cursor":            "next-page",
	})
	got, err := r2ObjectsFromJS(truncated)
	if err != nil {
		t.Fatalf("r2ObjectsFromJS() failed: %v", err)
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if got.Cursor != "next-page" {
		t.Errorf("Cursor = %q, want %q", got.Cursor, "next-page")
	}
	if len(got.Objects) != 1 || got.Objects[0].Key() != "a" {
		t.Errorf("Objects = %+v, want one object keyed \"a\"", got.Objects)
	}
	if len(got.DelimitedPrefixes) != 1 || got.DelimitedPrefixes[0] != "p/" {
		t.Errorf("DelimitedPrefixes = %v, want [\"p/\"]", got.DelimitedPrefixes)
	}

	notTruncated := js.ValueOf(map[string]any{
		"objects":           []any{},
		"delimitedPrefixes": []any{},
		"truncated":         false,
	})
	got2, err := r2ObjectsFromJS(notTruncated)
	if err != nil {
		t.Fatalf("r2ObjectsFromJS() failed: %v", err)
	}
	if got2.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if got2.Cursor != "" {
		t.Errorf("Cursor = %q, want \"\" (absent from the truncated:false branch)", got2.Cursor)
	}
}
