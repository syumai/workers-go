//go:build js && wasm

package vectorize

import (
	"syscall/js"
	"testing"
)

// TestVectorize_Query exercises an array-typed method parameter mapped via
// a types: override ([]float32 for a vector param that's a union in the
// .d.ts), a data-type parameter (VectorizeQueryOptions, including its
// map[string]any-overridden filter field), and decoding the JS-array
// result field (VectorizeMatches.Matches, a VectorizeMatch struct per 5.1
// item 1's Pick<Partial<VectorizeVector>,"values"> &
// Omit<VectorizeVector,"values"> & {score} intersection support). Every
// VectorizeMatch field is required (score isn't optional in the .d.ts, and
// the others come from Omit<VectorizeVector,"values">'s own required
// fields), so the fake match object below sets them all.
func TestVectorize_Query(t *testing.T) {
	var gotVectorLen int
	var gotTopK float64
	fake := js.ValueOf(map[string]any{})
	fake.Set("query", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotVectorLen = args[0].Length()
		gotTopK = args[1].Get("topK").Float()
		result := js.ValueOf(map[string]any{
			"matches": []any{js.ValueOf(map[string]any{
				"id":        "v1",
				"namespace": "ns1",
				"score":     0.9,
			})},
			"count": 1,
		})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	v := VectorizeFromJS(fake)
	matches, err := v.Query([]float32{0.1, 0.2, 0.3}, VectorizeQueryOptions{TopK: 5})
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}
	if gotVectorLen != 3 {
		t.Errorf("vector sent to JS had length %d, want 3", gotVectorLen)
	}
	if gotTopK != 5 {
		t.Errorf("options.topK sent to JS = %v, want 5", gotTopK)
	}
	if matches.Count != 1 || len(matches.Matches) != 1 {
		t.Errorf("matches = %+v, want Count=1 and one Matches entry", matches)
	}
	if got := matches.Matches[0]; got.ID != "v1" || got.Namespace != "ns1" || got.Score != 0.9 {
		t.Errorf("matches.Matches[0] = %+v, want ID=v1 Namespace=ns1 Score=0.9", got)
	}
}

// TestVectorize_Insert exercises an array-of-data-type method parameter
// (vectors []VectorizeVector), including the []float32-overridden Values
// field and the map[string]any-overridden Metadata field.
func TestVectorize_Insert(t *testing.T) {
	var gotValuesLen int
	var gotMetadataFoo string
	fake := js.ValueOf(map[string]any{})
	fake.Set("insert", js.FuncOf(func(this js.Value, args []js.Value) any {
		vec := args[0].Index(0)
		gotValuesLen = vec.Get("values").Length()
		gotMetadataFoo = vec.Get("metadata").Get("foo").String()
		result := js.ValueOf(map[string]any{"mutationId": "m1"})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	v := VectorizeFromJS(fake)
	mutation, err := v.Insert([]VectorizeVector{
		{ID: "v1", Values: []float32{1, 2, 3, 4}, Metadata: map[string]any{"foo": "bar"}},
	})
	if err != nil {
		t.Fatalf("Insert() failed: %v", err)
	}
	if gotValuesLen != 4 {
		t.Errorf("vectors[0].values sent to JS had length %d, want 4", gotValuesLen)
	}
	if gotMetadataFoo != "bar" {
		t.Errorf("vectors[0].metadata.foo sent to JS = %q, want %q", gotMetadataFoo, "bar")
	}
	if mutation.MutationID != "m1" {
		t.Errorf("mutation.MutationID = %q, want %q", mutation.MutationID, "m1")
	}
}

// TestVectorize_UnconvertibleMetadata is a regression test for the generated
// struct toJS methods, which run before jsrt.Call and so are not covered by
// its recover: a metadata value with no JS representation must come back as
// an error from Insert and Query, never panic and never reach JS.
func TestVectorize_UnconvertibleMetadata(t *testing.T) {
	called := false
	fake := js.ValueOf(map[string]any{})
	for _, m := range []string{"insert", "query"} {
		fake.Set(m, js.FuncOf(func(this js.Value, args []js.Value) any {
			called = true
			return js.Null()
		}))
	}
	idx := VectorizeFromJS(fake)
	bad := map[string]any{"ok": "x", "bad": make(chan int)}

	t.Run("insert", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Insert panicked: %v", r)
			}
		}()
		if _, err := idx.Insert([]VectorizeVector{{ID: "a", Metadata: bad}}); err == nil {
			t.Fatal("Insert succeeded, want an error")
		}
	})
	t.Run("query", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Query panicked: %v", r)
			}
		}()
		if _, err := idx.Query([]float32{1}, VectorizeQueryOptions{Filter: bad}); err == nil {
			t.Fatal("Query succeeded, want an error")
		}
	})
	if called {
		t.Error("the JS method was called despite the conversion error")
	}
}
