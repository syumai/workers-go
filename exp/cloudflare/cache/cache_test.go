//go:build js && wasm

package cache

import (
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestCache_Match_UndefinedIsNil verifies a cache miss (Cache.match resolves
// to undefined per the spec, since Match's return type is left as a raw
// js.Value) can be detected via jsrt.IsNil rather than crashing or being
// mistaken for a real response.
func TestCache_Match_UndefinedIsNil(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("match", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(js.Undefined())
	}))

	c := CacheFromJS(fake)
	got, err := c.Match(js.ValueOf("request"), CacheQueryOptions{})
	if err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if !jsrt.IsNil(got) {
		t.Errorf("Match() = %v, want a nil-ish (undefined) js.Value on a cache miss", got)
	}
}

// TestCache_Match_HitIsNotNil verifies a cache hit's response js.Value is
// not mistaken for a miss.
func TestCache_Match_HitIsNotNil(t *testing.T) {
	fakeResponse := js.ValueOf(map[string]any{"status": 200})
	fake := js.ValueOf(map[string]any{})
	fake.Set("match", js.FuncOf(func(this js.Value, args []js.Value) any {
		return resolve(fakeResponse)
	}))

	c := CacheFromJS(fake)
	got, err := c.Match(js.ValueOf("request"), CacheQueryOptions{})
	if err != nil {
		t.Fatalf("Match() failed: %v", err)
	}
	if jsrt.IsNil(got) {
		t.Fatal("Match() reported nil for a cache hit")
	}
	if got.Get("status").Int() != 200 {
		t.Errorf("Match().Get(\"status\") = %v, want 200", got.Get("status").Int())
	}
}

// TestCaches verifies Caches() wraps the global caches object.
func TestCaches(t *testing.T) {
	original := js.Global().Get("caches")
	defer js.Global().Set("caches", original)

	fakeCaches := js.ValueOf(map[string]any{})
	js.Global().Set("caches", fakeCaches)

	got := Caches()
	if !got.JSValue().Equal(fakeCaches) {
		t.Errorf("Caches().JSValue() did not return the global caches object")
	}
}
