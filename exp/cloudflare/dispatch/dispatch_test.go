//go:build js && wasm

package dispatch

import (
	"syscall/js"
	"testing"
)

// TestDispatchNamespace_Get verifies the generated Get sends name, args (as
// a plain JS object built from the map[string]any override), and options
// through to the underlying JS get(), and returns its (synchronous, not a
// Promise) result untouched, per DispatchNamespace.get's real signature.
func TestDispatchNamespace_Get(t *testing.T) {
	var gotName string
	var gotArgs js.Value
	var gotLimitsCPU float64
	fetcher := js.ValueOf(map[string]any{"fake": "fetcher"})
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotName = args[0].String()
		gotArgs = args[1]
		gotLimitsCPU = args[2].Get("limits").Get("cpuMs").Float()
		return fetcher
	}))

	ns := DispatchNamespaceFromJS(fake)
	got, err := ns.Get("customer-worker", map[string]any{"tenant": "acme"}, DynamicDispatchOptions{
		Limits: &DynamicDispatchLimits{CpuMs: 50},
	})
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if !got.Equal(fetcher) {
		t.Errorf("Get() = %v, want the fake fetcher value", got)
	}
	if gotName != "customer-worker" {
		t.Errorf("get() got name = %q, want %q", gotName, "customer-worker")
	}
	if tenant := gotArgs.Get("tenant").String(); tenant != "acme" {
		t.Errorf("get() got args.tenant = %q, want %q", tenant, "acme")
	}
	if gotLimitsCPU != 50 {
		t.Errorf("get() got options.limits.cpuMs = %v, want 50", gotLimitsCPU)
	}
}

// TestDispatchNamespace_Get_Errors verifies a JS exception thrown by get()
// (e.g. the script doesn't exist in this dispatch namespace, per its doc
// comment) surfaces as a Go error rather than panicking.
func TestDispatchNamespace_Get_Errors(t *testing.T) {
	fake := js.ValueOf(map[string]any{}) // no "get" method at all
	ns := DispatchNamespaceFromJS(fake)
	if _, err := ns.Get("missing-worker", nil, DynamicDispatchOptions{}); err == nil {
		t.Fatal("Get() succeeded, want an error")
	}
}

// TestDispatchNamespace_GetClient verifies GetClient wraps Get's raw
// js.Value Fetcher result as a *fetch.Client bound to that value (rather
// than, say, a fresh client with no binding).
func TestDispatchNamespace_GetClient(t *testing.T) {
	fetcher := js.ValueOf(map[string]any{"fake": "fetcher"})
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		return fetcher
	}))

	ns := DispatchNamespaceFromJS(fake)
	client, err := ns.GetClient("customer-worker", nil)
	if err != nil {
		t.Fatalf("GetClient() failed: %v", err)
	}
	if client == nil {
		t.Fatal("GetClient() returned a nil *fetch.Client")
	}
}

// TestDispatchNamespace_Get_UnconvertibleArgs checks that a value with no JS
// representation in args or the outbound options is returned as an error
// instead of panicking in the generated toJS, which runs before jsrt.Call.
func TestDispatchNamespace_Get_UnconvertibleArgs(t *testing.T) {
	called := false
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		called = true
		return js.Null()
	}))
	ns := DispatchNamespaceFromJS(fake)
	bad := map[string]any{"bad": make(chan int)}

	for name, call := range map[string]func() error{
		"args": func() error {
			_, err := ns.Get("w", bad, DynamicDispatchOptions{})
			return err
		},
		"outbound": func() error {
			_, err := ns.Get("w", nil, DynamicDispatchOptions{Outbound: bad})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Get panicked: %v", r)
				}
			}()
			if err := call(); err == nil {
				t.Fatal("Get succeeded, want an error")
			}
		})
	}
	if called {
		t.Error("the JS get was called despite the conversion error")
	}
}
