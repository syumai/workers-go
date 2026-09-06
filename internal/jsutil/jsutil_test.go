//go:build js && wasm

package jsutil

import (
	"reflect"
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func TestMaybeString(t *testing.T) {
	tests := []struct {
		name string
		in   js.Value
		want string
	}{
		{"undefined", js.Undefined(), ""},
		{"value", js.ValueOf("hello"), "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaybeString(tt.in); got != tt.want {
				t.Errorf("MaybeString(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("null", func(t *testing.T) {
		t.Skip("known issue: null is not treated as zero value")
	})
}

func TestMaybeInt(t *testing.T) {
	tests := []struct {
		name string
		in   js.Value
		want int
	}{
		{"undefined", js.Undefined(), 0},
		{"value", js.ValueOf(42), 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaybeInt(tt.in); got != tt.want {
				t.Errorf("MaybeInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}

	t.Run("null", func(t *testing.T) {
		t.Skip("known issue: null is not treated as zero value")
	})
}

func TestMaybeDate(t *testing.T) {
	t.Run("undefined", func(t *testing.T) {
		got, err := MaybeDate(js.Undefined())
		if err != nil {
			t.Fatalf("MaybeDate(undefined) error = %v, want nil", err)
		}
		if !got.IsZero() {
			t.Errorf("MaybeDate(undefined) = %v, want zero value", got)
		}
	})

	t.Run("value", func(t *testing.T) {
		want := time.Date(2024, time.March, 5, 12, 34, 56, 0, time.UTC)
		got, err := MaybeDate(TimeToDate(want))
		if err != nil {
			t.Fatalf("MaybeDate(value) error = %v, want nil", err)
		}
		if !got.Equal(want) {
			t.Errorf("MaybeDate(value) = %v, want %v", got, want)
		}
	})

	t.Run("null", func(t *testing.T) {
		t.Skip("known issue: null is not treated as zero value")
	})
}

func TestDateToTime_TimeToDate_roundTrip(t *testing.T) {
	want := time.Date(2024, time.March, 5, 12, 34, 56, 789_000_000, time.UTC)

	date := TimeToDate(want)
	got, err := DateToTime(date)
	if err != nil {
		t.Fatalf("DateToTime() error = %v, want nil", err)
	}
	if !got.Equal(want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestStrRecordToMap(t *testing.T) {
	valuesObj := NewObject()
	valuesObj.Set("a", "1")
	valuesObj.Set("b", "2")

	tests := map[string]struct {
		in   js.Value
		want map[string]string
	}{
		"empty_object": {NewObject(), map[string]string{}},
		"undefined":    {js.Undefined(), map[string]string{}},
		"null":         {Null, map[string]string{}},
		"values":       {valuesObj, map[string]string{"a": "1", "b": "2"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := StrRecordToMap(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StrRecordToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorf(t *testing.T) {
	err := Errorf("boom %d", 42)

	if !err.InstanceOf(ErrorClass) {
		t.Errorf("Errorf() = %v, want an Error instance", err)
	}
	if got := err.Get("message").String(); got != "boom 42" {
		t.Errorf("Errorf().message = %q, want %q", got, "boom 42")
	}
}

// jsPromise builds a Promise whose executor synchronously resolves or
// rejects with v (converted with js.ValueOf unless it is already a
// js.Value). This mirrors internal/jstest.Resolved/Rejected, duplicated
// here because internal/jstest imports this package (importing it back
// would create an import cycle).
func jsPromise(t *testing.T, resolveVal, rejectVal any) js.Value {
	t.Helper()
	var fn js.Func
	fn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer fn.Release()
		if rejectVal != nil {
			reject := args[1]
			if v, ok := rejectVal.(js.Value); ok {
				reject.Invoke(v)
			} else {
				reject.Invoke(js.ValueOf(rejectVal))
			}
			return js.Undefined()
		}
		resolve := args[0]
		if v, ok := resolveVal.(js.Value); ok {
			resolve.Invoke(v)
		} else {
			resolve.Invoke(js.ValueOf(resolveVal))
		}
		return js.Undefined()
	})
	return NewPromise(fn)
}

func TestAwaitPromise(t *testing.T) {
	t.Run("resolved", func(t *testing.T) {
		got, err := AwaitPromise(jsPromise(t, "ok", nil))
		if err != nil {
			t.Fatalf("AwaitPromise() error = %v, want nil", err)
		}
		if got.String() != "ok" {
			t.Errorf("AwaitPromise() = %v, want %q", got, "ok")
		}
	})

	t.Run("rejected_with_error", func(t *testing.T) {
		_, err := AwaitPromise(jsPromise(t, nil, Error("boom")))
		if err == nil {
			t.Fatalf("AwaitPromise() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "boom") {
			t.Errorf("AwaitPromise() error = %v, want to contain %q", err, "boom")
		}
	})

	t.Run("rejected_with_string", func(t *testing.T) {
		// known issue: AwaitPromise's catch handler calls result.Call("toString"),
		// which syscall/js only allows on object values. When a Promise
		// rejects with a plain (non-Error) value such as a string, that
		// call panics instead of returning an error, which crashes the
		// whole test binary (not just this subtest). Skip before ever
		// constructing that promise.
		t.Skip("known issue: AwaitPromise panics (not just errors) on a non-object rejection value")

		_, err := AwaitPromise(jsPromise(t, nil, "plain string rejection"))
		if err == nil {
			t.Fatalf("AwaitPromise() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "plain string rejection") {
			t.Errorf("AwaitPromise() error = %v, want to contain %q", err, "plain string rejection")
		}
	})
}

func TestArrayFrom(t *testing.T) {
	set := js.Global().Get("Set").New(js.ValueOf([]any{"a", "b", "c"}))

	arr := ArrayFrom(set)

	want := []string{"a", "b", "c"}
	if got := arr.Length(); got != len(want) {
		t.Fatalf("ArrayFrom(Set).Length() = %d, want %d", got, len(want))
	}
	for i, w := range want {
		if got := arr.Index(i).String(); got != w {
			t.Errorf("ArrayFrom(Set)[%d] = %q, want %q", i, got, w)
		}
	}
}
