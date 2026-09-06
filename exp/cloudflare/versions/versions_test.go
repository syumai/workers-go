//go:build js && wasm

package versions

import (
	"syscall/js"
	"testing"
)

// TestNewWorkerVersionMetadata exercises the data-type binding path: New*
// resolves the binding via jsrt.Binding and decodes it directly (there is
// no underlying JS handle object to wrap, since WorkerVersionMetadata is a
// plain object-literal alias in the .d.ts).
func TestWorkerVersionMetadataFromJS_ToJS_RoundTrip(t *testing.T) {
	want := js.ValueOf(map[string]any{
		"id":        "v1",
		"tag":       "some-tag",
		"timestamp": "2026-09-07T00:00:00Z",
	})

	meta, err := workerVersionMetadataFromJS(want)
	if err != nil {
		t.Fatalf("workerVersionMetadataFromJS failed: %v", err)
	}
	if meta.ID != "v1" || meta.Tag != "some-tag" || meta.Timestamp != "2026-09-07T00:00:00Z" {
		t.Fatalf("decoded metadata = %+v, want {ID:v1 Tag:some-tag Timestamp:2026-09-07T00:00:00Z}", meta)
	}

	back := meta.toJS()
	if got := back.Get("id").String(); got != "v1" {
		t.Errorf("toJS().id = %q, want %q", got, "v1")
	}
	if got := back.Get("tag").String(); got != "some-tag" {
		t.Errorf("toJS().tag = %q, want %q", got, "some-tag")
	}
	if got := back.Get("timestamp").String(); got != "2026-09-07T00:00:00Z" {
		t.Errorf("toJS().timestamp = %q, want %q", got, "2026-09-07T00:00:00Z")
	}
}
