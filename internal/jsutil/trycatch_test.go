//go:build js && wasm

package jsutil

import (
	"syscall/js"
	"testing"
)

func TestTryCatch(t *testing.T) {
	t.Run("returns_value", func(t *testing.T) {
		fn := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			return js.ValueOf("ok")
		})
		defer fn.Release()

		got, err := TryCatch(fn)
		if err != nil {
			t.Fatalf("TryCatch() error = %v, want nil", err)
		}
		if got.String() != "ok" {
			t.Errorf("TryCatch() = %v, want %q", got, "ok")
		}
	})

	t.Run("returns_undefined", func(t *testing.T) {
		fn := js.FuncOf(func(_ js.Value, _ []js.Value) any {
			return js.Undefined()
		})
		defer fn.Release()

		got, err := TryCatch(fn)
		if err != nil {
			t.Fatalf("TryCatch() error = %v, want nil", err)
		}
		if !got.IsUndefined() {
			t.Errorf("TryCatch() = %v, want undefined", got)
		}
	})

	t.Run("throws", func(t *testing.T) {
		// TryCatch's contract (see cloudflare/sockets.Connect) is to run fn
		// via globalThis.tryCatch's try/catch, converting a JS-side throw
		// into a Go error. In production, fn's body typically triggers that
		// throw by calling a JS function that fails (e.g. Value.Invoke on a
		// connect() call the runtime rejects), which makes Go's
		// syscall/js panic (Value.Invoke/Call panics on failure) from
		// *inside* the js.FuncOf callback that globalThis.tryCatch's try
		// block is running.
		//
		// That panic does not behave like a normal, isolated Go panic here:
		// this callback is invoked reentrantly (Go test -> tryCatch (JS) ->
		// fn (Go) ), and panicking in that nested position was observed to
		// hang the whole Node process indefinitely instead of being
		// surfaced to the outer try/catch or crashing cleanly, which would
		// hang `make test` itself. So this subtest cannot safely exercise
		// that path.
		t.Skip("known issue: a panic raised from inside the fn passed to TryCatch hangs the process instead of being caught, so this path can't be safely tested here")
	})
}
