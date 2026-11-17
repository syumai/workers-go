//go:build js && wasm

package jsrt

import (
	"strings"
	"syscall/js"
	"testing"
	"time"
)

// TestCallRawBytesPanics documents the failure that AnyToJS exists to avoid:
// js.ValueOf cannot represent a []byte, so passing one straight through Call
// (what generated "...any" bindings used to do) fails.
func TestCallRawBytesPanics(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("bind", js.FuncOf(func(this js.Value, args []js.Value) any { return nil }))
	if _, err := Call(fake, "bind", []byte{1, 2, 3}); err == nil {
		t.Fatal("Call with a raw []byte succeeded, want a ValueOf error")
	}
}

func TestAnyToJS(t *testing.T) {
	u8 := js.Global().Get("Uint8Array")
	date := js.Global().Get("Date")
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name  string
		in    any
		check func(t *testing.T, v js.Value)
	}{
		{"nil", nil, func(t *testing.T, v js.Value) {
			if !v.IsNull() {
				t.Errorf("got %v, want null", v)
			}
		}},
		{"string", "s", func(t *testing.T, v js.Value) {
			if v.String() != "s" {
				t.Errorf("got %v", v)
			}
		}},
		{"int64", int64(7), func(t *testing.T, v js.Value) {
			if v.Int() != 7 {
				t.Errorf("got %v", v)
			}
		}},
		{"float64", 1.5, func(t *testing.T, v js.Value) {
			if v.Float() != 1.5 {
				t.Errorf("got %v", v)
			}
		}},
		{"bool", true, func(t *testing.T, v js.Value) {
			if !v.Bool() {
				t.Errorf("got %v", v)
			}
		}},
		{"bytes", []byte{9, 8, 7}, func(t *testing.T, v js.Value) {
			if !v.InstanceOf(u8) || v.Length() != 3 || v.Index(0).Int() != 9 || v.Index(2).Int() != 7 {
				t.Errorf("got %v, want Uint8Array [9 8 7]", v)
			}
		}},
		{"empty bytes", []byte{}, func(t *testing.T, v js.Value) {
			if !v.InstanceOf(u8) || v.Length() != 0 {
				t.Errorf("got %v, want empty Uint8Array", v)
			}
		}},
		{"nil bytes", []byte(nil), func(t *testing.T, v js.Value) {
			if !v.InstanceOf(u8) || v.Length() != 0 {
				t.Errorf("got %v, want empty Uint8Array", v)
			}
		}},
		{"time", ts, func(t *testing.T, v js.Value) {
			if !v.InstanceOf(date) || int64(v.Call("getTime").Float()) != ts.UnixMilli() {
				t.Errorf("got %v, want Date(%v)", v, ts)
			}
		}},
		{"js.Value passthrough", js.ValueOf("x"), func(t *testing.T, v js.Value) {
			if v.String() != "x" {
				t.Errorf("got %v", v)
			}
		}},
		{"nested", map[string]any{"b": []byte{1}, "l": []any{[]byte{2}, "z"}}, func(t *testing.T, v js.Value) {
			if !v.Get("b").InstanceOf(u8) || v.Get("b").Index(0).Int() != 1 {
				t.Errorf("b = %v", v.Get("b"))
			}
			l := v.Get("l")
			if !l.Index(0).InstanceOf(u8) || l.Index(1).String() != "z" {
				t.Errorf("l = %v", l)
			}
		}},
		{"typed slice", []string{"a", "b"}, func(t *testing.T, v js.Value) {
			if v.Length() != 2 || v.Index(1).String() != "b" {
				t.Errorf("got %v", v)
			}
		}},
		{"map[string]string", map[string]string{"k": "v"}, func(t *testing.T, v js.Value) {
			if v.Get("k").String() != "v" {
				t.Errorf("got %v", v)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := AnyToJS(tt.in)
			if err != nil {
				t.Fatalf("AnyToJS(%T) failed: %v", tt.in, err)
			}
			tt.check(t, v)
		})
	}
}

// TestAnyToJSRejects checks that values with no JS representation come back
// as errors, at any nesting depth, rather than panicking in js.ValueOf.
func TestAnyToJSRejects(t *testing.T) {
	type s struct{ A int }
	for name, in := range map[string]any{
		"chan":                           make(chan int),
		"func":                           func() {},
		"struct":                         s{A: 1},
		"int-keyed map":                  map[int]string{1: "a"},
		"nested in map":                  map[string]any{"ok": 1, "bad": make(chan int)},
		"nested in slice":                []any{"ok", func() {}},
		"nested in []byte-ish map slice": []map[string]any{{"bad": s{}}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("AnyToJS panicked: %v", r)
				}
			}()
			if _, err := AnyToJS(in); err == nil {
				t.Fatalf("AnyToJS(%T) succeeded, want an error", in)
			}
		})
	}
}

func TestAnyArgs(t *testing.T) {
	u8 := js.Global().Get("Uint8Array")
	got, err := AnyArgs([]any{"q", []byte{1}, int64(2), nil})
	if err != nil {
		t.Fatalf("AnyArgs failed: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	for i, g := range got {
		if _, ok := g.(js.Value); !ok {
			t.Errorf("got[%d] = %T, want js.Value", i, g)
		}
	}
	if b := got[1].(js.Value); !b.InstanceOf(u8) || b.Index(0).Int() != 1 {
		t.Errorf("[]byte argument = %v, want Uint8Array [1]", b)
	}
	if out, err := AnyArgs(nil); err != nil || len(out) != 0 {
		t.Errorf("AnyArgs(nil) = %v, %v; want empty, nil", out, err)
	}
}

// TestAnyArgsRejects checks that an element with no JS representation is
// returned as an error naming its position, never a panic.
func TestAnyArgsRejects(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AnyArgs panicked: %v", r)
		}
	}()
	_, err := AnyArgs([]any{"ok", make(chan int)})
	if err == nil {
		t.Fatal("AnyArgs accepted a chan, want an error")
	}
	if !strings.Contains(err.Error(), "argument 1") {
		t.Errorf("error %q does not name the offending argument", err)
	}
}
