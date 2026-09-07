//go:build js && wasm

package kv

import (
	"syscall/js"
	"testing"
)

// resolve wraps v in an already-resolved JS Promise.
func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestKVNamespace_GetText_PassesLiteralType verifies that the overload
// split for KVNamespace.get sends the correct literal "type" argument to
// JS for each Go method (GetText -> "text"), and that a resolved string
// decodes as a non-nil *string.
func TestKVNamespace_GetText_PassesLiteralType(t *testing.T) {
	var gotKey, gotType string
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].String()
		gotType = args[1].String()
		return resolve("hello")
	}))

	ns := KVNamespaceFromJS(fake)
	got, err := ns.GetText("some-key")
	if err != nil {
		t.Fatalf("GetText() failed: %v", err)
	}
	if gotKey != "some-key" {
		t.Errorf("key sent to JS = %q, want %q", gotKey, "some-key")
	}
	if gotType != "text" {
		t.Errorf("type literal sent to JS = %q, want %q", gotType, "text")
	}
	if got == nil || *got != "hello" {
		t.Errorf("GetText() = %v, want *string(\"hello\")", got)
	}
}

// TestKVNamespace_GetJSON_PassesLiteralType verifies the "json" overload
// sends "json" as the type literal and returns the raw js.Value (since the
// decoded shape is caller-defined).
func TestKVNamespace_GetJSON_PassesLiteralType(t *testing.T) {
	var gotType string
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotType = args[1].String()
		return resolve(map[string]any{"n": 1})
	}))

	ns := KVNamespaceFromJS(fake)
	got, err := ns.GetJSON("k")
	if err != nil {
		t.Fatalf("GetJSON() failed: %v", err)
	}
	if gotType != "json" {
		t.Errorf("type literal sent to JS = %q, want %q", gotType, "json")
	}
	if got.Get("n").Int() != 1 {
		t.Errorf("GetJSON().Get(\"n\") = %v, want 1", got.Get("n").Int())
	}
}

// TestKVNamespace_GetText_NilOnNull verifies a resolved JS null decodes to
// a nil *string rather than a pointer to "" (spec item 3: Promise<T|null>
// with T a prim becomes (*T, error), nil on null).
func TestKVNamespace_GetText_NilOnNull(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(nil)
	}))

	ns := KVNamespaceFromJS(fake)
	got, err := ns.GetText("missing")
	if err != nil {
		t.Fatalf("GetText() failed: %v", err)
	}
	if got != nil {
		t.Errorf("GetText() = %v, want nil", *got)
	}
}

// TestKVNamespace_GetWithMetadata_CacheStatusNull verifies
// KVNamespaceGetWithMetadataResult.CacheStatus (a required-but-nullable
// `string | null` field) decodes a JS null without panicking (it would
// panic calling js.Value.String() on a null value without the
// isNullableType guard).
func TestKVNamespace_GetWithMetadata_CacheStatusNull(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("getWithMetadata", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(map[string]any{
			"value":       "v",
			"metadata":    nil,
			"cacheStatus": nil,
		})
	}))

	ns := KVNamespaceFromJS(fake)
	got, err := ns.GetTextWithMetadata("k")
	if err != nil {
		t.Fatalf("GetTextWithMetadata() failed: %v", err)
	}
	if got.CacheStatus != "" {
		t.Errorf("CacheStatus = %q, want \"\"", got.CacheStatus)
	}
	if got.Value.String() != "v" {
		t.Errorf("Value = %v, want \"v\"", got.Value)
	}
}

// TestKVNamespace_Put_PassesRawValue verifies Put forwards its value
// argument (a js.Value, per the KVNamespace.put.params.value override) and
// the encoded options struct untouched.
func TestKVNamespace_Put_PassesRawValue(t *testing.T) {
	var gotValue js.Value
	var gotExpirationTTL int
	fake := js.ValueOf(map[string]any{})
	fake.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotValue = args[1]
		gotExpirationTTL = args[2].Get("expirationTtl").Int()
		return resolve(nil)
	}))

	ns := KVNamespaceFromJS(fake)
	if err := ns.Put("k", js.ValueOf("payload"), KVNamespacePutOptions{ExpirationTTL: 60}); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if gotValue.String() != "payload" {
		t.Errorf("value sent to JS = %v, want \"payload\"", gotValue)
	}
	if gotExpirationTTL != 60 {
		t.Errorf("expirationTtl sent to JS = %v, want 60", gotExpirationTTL)
	}
}
