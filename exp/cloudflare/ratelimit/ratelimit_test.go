//go:build js && wasm

package ratelimit

import (
	"syscall/js"
	"testing"
)

// TestRateLimit_Limit exercises the full generated round trip for a
// Promise-returning method on a handle type: a fake JS RateLimit object
// whose limit() returns a resolved Promise, wrapped with RateLimitFromJS,
// called through Limit, and decoded back into a RateLimitOutcome.
func TestRateLimit_Limit(t *testing.T) {
	var gotKey string
	fake := js.ValueOf(map[string]any{})
	fake.Set("limit", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].Get("key").String()
		result := js.ValueOf(map[string]any{"success": true})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	rl := RateLimitFromJS(fake)
	outcome, err := rl.Limit(RateLimitOptions{Key: "some-key"})
	if err != nil {
		t.Fatalf("Limit() failed: %v", err)
	}
	if !outcome.Success {
		t.Fatalf("outcome.Success = false, want true")
	}
	if gotKey != "some-key" {
		t.Fatalf("options.key sent to JS = %q, want %q", gotKey, "some-key")
	}
}

// TestRateLimit_Limit_Rejects verifies a rejected Promise surfaces as a Go
// error rather than panicking.
func TestRateLimit_Limit_Rejects(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("limit", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("boom"))
	}))

	rl := RateLimitFromJS(fake)
	if _, err := rl.Limit(RateLimitOptions{Key: "k"}); err == nil {
		t.Fatalf("Limit() succeeded, want an error")
	}
}
