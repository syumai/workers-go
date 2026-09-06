//go:build js && wasm

package kv

import (
	"io"
	"strings"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestNewNamespace_undefined(t *testing.T) {
	jstest.SetEnv(t, map[string]any{})

	ns, err := NewNamespace("KV")
	if err == nil {
		t.Fatalf("NewNamespace() = %v, want error", ns)
	}
}

func TestNamespace_PutString_GetString(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	if err := ns.PutString("key1", "value1", nil); err != nil {
		t.Fatalf("PutString: %v", err)
	}
	got, err := ns.GetString("key1", nil)
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if got != "value1" {
		t.Errorf("GetString() = %q, want %q", got, "value1")
	}
}

func TestNamespace_PutReader_GetReader(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	const want = "hello world"
	if err := ns.PutReader("key1", strings.NewReader(want), nil); err != nil {
		t.Fatalf("PutReader: %v", err)
	}

	r, err := ns.GetReader("key1", nil)
	if err != nil {
		t.Fatalf("GetReader: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	if string(got) != want {
		t.Errorf("GetReader() content = %q, want %q", got, want)
	}
}

func TestNamespace_List_prefixLimitCursor(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	for _, k := range []string{"a/1", "a/2", "a/3", "b/1"} {
		if err := ns.PutString(k, k, nil); err != nil {
			t.Fatalf("PutString(%q): %v", k, err)
		}
	}

	res, err := ns.List(&ListOptions{Prefix: "a/", Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Keys) != 2 {
		t.Fatalf("len(Keys) = %d, want 2", len(res.Keys))
	}
	if res.Keys[0].Name != "a/1" || res.Keys[1].Name != "a/2" {
		t.Errorf("Keys = [%q, %q], want [a/1, a/2]", res.Keys[0].Name, res.Keys[1].Name)
	}
	if res.ListComplete {
		t.Errorf("ListComplete = true, want false (more results remain)")
	}
	if res.Cursor == "" {
		t.Fatalf("Cursor is empty, want a cursor for the next page")
	}

	res2, err := ns.List(&ListOptions{Prefix: "a/", Limit: 2, Cursor: res.Cursor})
	if err != nil {
		t.Fatalf("List (page 2): %v", err)
	}
	if len(res2.Keys) != 1 || res2.Keys[0].Name != "a/3" {
		t.Fatalf("page 2 Keys = %+v, want [a/3]", res2.Keys)
	}
	if !res2.ListComplete {
		t.Errorf("ListComplete = false, want true (no more results)")
	}
}

func TestNamespace_Delete(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	if err := ns.PutString("key1", "value1", nil); err != nil {
		t.Fatalf("PutString: %v", err)
	}
	if !fk.has("key1") {
		t.Fatalf("fakeKV: key1 not stored after PutString")
	}
	if err := ns.Delete("key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fk.has("key1") {
		t.Errorf("fakeKV: key1 still stored after Delete")
	}
}

// TestNamespace_GetString_missing fixes the current, arguably surprising,
// behavior of GetString on a miss: the real KV get() resolves with null,
// and GetString does not special-case that (unlike r2.Bucket.Get, which
// checks v.IsNull() and returns (nil, nil)). js.Value.String() on a null
// value does not panic; per its doc comment, it returns the placeholder
// "<null>". So GetString currently returns ("<null>", nil), not ("", nil)
// or an error, on a miss.
func TestNamespace_GetString_missing(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	got, err := ns.GetString("missing", nil)
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if got != "<null>" {
		t.Errorf("GetString(missing) = %q, want %q", got, "<null>")
	}
}

func TestNamespace_optionsReachJS(t *testing.T) {
	fk := newFakeKV(t)
	jstest.SetEnv(t, map[string]any{"KV": fk.value})

	ns, err := NewNamespace("KV")
	if err != nil {
		t.Fatalf("NewNamespace: %v", err)
	}

	if err := ns.PutString("key1", "value1", &PutOptions{ExpirationTTL: 60}); err != nil {
		t.Fatalf("PutString: %v", err)
	}

	calls := fk.callsSnapshot()
	var last *fakeKVCall
	for i := range calls {
		if calls[i].Method == "put" {
			last = &calls[i]
		}
	}
	if last == nil {
		t.Fatalf("fakeKV: no put call recorded")
	}
	jstest.AssertObjectEqual(t, last.Opts, map[string]any{"expirationTtl": 60})
}
