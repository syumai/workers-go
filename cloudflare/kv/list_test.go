//go:build js && wasm

package kv

import (
	"syscall/js"
	"testing"
)

func TestToListKey(t *testing.T) {
	t.Run("with_expiration", func(t *testing.T) {
		v := js.ValueOf(map[string]any{"name": "foo", "expiration": 123})
		got, err := toListKey(v)
		if err != nil {
			t.Fatalf("toListKey: %v", err)
		}
		if got.Name != "foo" {
			t.Errorf("Name = %q, want %q", got.Name, "foo")
		}
		if got.Expiration != 123 {
			t.Errorf("Expiration = %d, want %d", got.Expiration, 123)
		}
	})

	t.Run("without_expiration", func(t *testing.T) {
		v := js.ValueOf(map[string]any{"name": "foo"})
		got, err := toListKey(v)
		if err != nil {
			t.Fatalf("toListKey: %v", err)
		}
		if got.Name != "foo" {
			t.Errorf("Name = %q, want %q", got.Name, "foo")
		}
		if got.Expiration != 0 {
			t.Errorf("Expiration = %d, want 0", got.Expiration)
		}
	})

	t.Run("name_missing", func(t *testing.T) {
		// toListKey does not validate that "name" is present: with no
		// "name" key, v.Get("name") is undefined and js.Value.String()
		// returns the placeholder "<undefined>", not an error. This test
		// fixes that current behavior. A stricter implementation could
		// return an error here instead; if it ever does, update this test.
		v := js.ValueOf(map[string]any{})
		got, err := toListKey(v)
		if err != nil {
			t.Fatalf("toListKey: unexpected error: %v", err)
		}
		if got.Name != "<undefined>" {
			t.Errorf("Name = %q, want %q", got.Name, "<undefined>")
		}
	})
}

func TestToListResult(t *testing.T) {
	t.Run("keys_and_cursor", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"keys": []any{
				map[string]any{"name": "a", "expiration": 10},
				map[string]any{"name": "b"},
			},
			"list_complete": false,
			"cursor":        "next",
		})
		got, err := toListResult(v)
		if err != nil {
			t.Fatalf("toListResult: %v", err)
		}
		if len(got.Keys) != 2 {
			t.Fatalf("len(Keys) = %d, want 2", len(got.Keys))
		}
		if got.Keys[0].Name != "a" || got.Keys[0].Expiration != 10 {
			t.Errorf("Keys[0] = %+v, want {Name: a, Expiration: 10}", got.Keys[0])
		}
		if got.Keys[1].Name != "b" || got.Keys[1].Expiration != 0 {
			t.Errorf("Keys[1] = %+v, want {Name: b, Expiration: 0}", got.Keys[1])
		}
		if got.ListComplete {
			t.Errorf("ListComplete = true, want false")
		}
		if got.Cursor != "next" {
			t.Errorf("Cursor = %q, want %q", got.Cursor, "next")
		}
	})

	t.Run("cursor_missing", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"keys":          []any{},
			"list_complete": true,
		})
		got, err := toListResult(v)
		if err != nil {
			t.Fatalf("toListResult: %v", err)
		}
		if len(got.Keys) != 0 {
			t.Fatalf("len(Keys) = %d, want 0", len(got.Keys))
		}
		if !got.ListComplete {
			t.Errorf("ListComplete = false, want true")
		}
		if got.Cursor != "" {
			t.Errorf("Cursor = %q, want empty", got.Cursor)
		}
	})
}
