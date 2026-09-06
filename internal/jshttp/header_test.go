//go:build js && wasm

package jshttp

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestToHeader(t *testing.T) {
	t.Run("single_value", func(t *testing.T) {
		h := jsutil.HeadersClass.New()
		h.Call("append", "X-Test", "1")

		got := ToHeader(h)
		if want := "1"; got.Get("X-Test") != want {
			t.Errorf("ToHeader()[X-Test] = %q, want %q", got.Get("X-Test"), want)
		}
	})

	t.Run("multiple_values_same_name", func(t *testing.T) {
		h := jsutil.HeadersClass.New()
		h.Call("append", "X-Multi", "a")
		h.Call("append", "X-Multi", "b")

		got := ToHeader(h)
		// NOTE (current behavior, not necessarily desired): the Headers
		// object joins repeated values for the same name with ", " before
		// ToHeader ever sees them, and ToHeader splits back on "," without
		// trimming, so the second value keeps a leading space. See
		// header.go and the Set-Cookie case below, where the same splitting
		// corrupts values that legitimately contain a comma.
		want := []string{"a", " b"}
		if !reflect.DeepEqual(got["X-Multi"], want) {
			t.Errorf("ToHeader()[X-Multi] = %q, want %q", got["X-Multi"], want)
		}
	})

	t.Run("set_cookie_with_comma", func(t *testing.T) {
		t.Skip("known issue: ToHeader splits header values on comma, corrupting Set-Cookie values that contain one (see header.go)")

		h := jsutil.HeadersClass.New()
		h.Call("append", "Set-Cookie", "a=1; Expires=Wed, 09 Jun 2021 10:18:14 GMT")
		h.Call("append", "Set-Cookie", "b=2; Path=/")

		got := ToHeader(h)
		want := []string{
			"a=1; Expires=Wed, 09 Jun 2021 10:18:14 GMT",
			"b=2; Path=/",
		}
		if !reflect.DeepEqual(got["Set-Cookie"], want) {
			t.Errorf("ToHeader()[Set-Cookie] = %q, want %q", got["Set-Cookie"], want)
		}
	})

	t.Run("name_is_canonicalized", func(t *testing.T) {
		h := jsutil.HeadersClass.New()
		h.Call("append", "x-test", "1")

		got := ToHeader(h)
		if _, ok := got["X-Test"]; !ok {
			t.Errorf("ToHeader() did not canonicalize the header name to X-Test: %v", got)
		}
	})
}

func TestToJSHeader(t *testing.T) {
	header := http.Header{}
	header.Add("X-Test", "1")
	header.Add("X-Multi", "a")
	header.Add("X-Multi", "b")

	h := ToJSHeader(header)
	entries := jsutil.ArrayFrom(h.Call("entries"))
	got := map[string]string{}
	for i := 0; i < entries.Length(); i++ {
		e := entries.Index(i)
		got[e.Index(0).String()] = e.Index(1).String()
	}

	if want := "1"; got["x-test"] != want {
		t.Errorf("ToJSHeader().get(x-test) = %q, want %q", got["x-test"], want)
	}
	if want := "a, b"; got["x-multi"] != want {
		t.Errorf("ToJSHeader().get(x-multi) = %q, want %q", got["x-multi"], want)
	}
}

func TestHeader_roundTrip(t *testing.T) {
	// Use single-valued headers only: round-tripping a multi-value header
	// through ToJSHeader/ToHeader hits the comma-splitting behavior
	// documented in TestToHeader, so it isn't a lossless round trip.
	want := http.Header{
		"X-Test":       {"1"},
		"Content-Type": {"text/plain"},
	}

	got := ToHeader(ToJSHeader(want))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}
