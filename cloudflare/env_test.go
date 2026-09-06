//go:build js && wasm

package cloudflare

import (
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestGetenv(t *testing.T) {
	jstest.SetEnv(t, map[string]any{"FOO": "bar"})

	if got, want := Getenv("FOO"), "bar"; got != want {
		t.Errorf("Getenv(%q) = %q, want %q", "FOO", got, want)
	}
	if got, want := Getenv("MISSING"), ""; got != want {
		t.Errorf("Getenv(%q) = %q, want %q", "MISSING", got, want)
	}
}

func TestGetBinding(t *testing.T) {
	binding := js.ValueOf(map[string]any{"k": "v"})
	jstest.SetEnv(t, map[string]any{"BIND": binding})

	got := GetBinding("BIND")
	if got.Get("k").String() != "v" {
		t.Errorf("GetBinding(%q) = %v, want object with k=%q", "BIND", got, "v")
	}
}

// TestGetenv_withoutContext fixes the current behavior: Getenv panics when
// there is no `env` in the runtime context, rather than returning "".
func TestGetenv_withoutContext(t *testing.T) {
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Getenv() did not panic, want panic (no env in context)")
		}
	}()
	Getenv("FOO")
	t.Fatal("unreachable: Getenv() should have panicked")
}
