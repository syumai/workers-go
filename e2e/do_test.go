//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestDurableObject checks that the Go SDK's Durable Object stub
// (cloudflare.NewDurableObjectNamespace/IdFromName/Get/Fetch) can drive a
// real, JS-defined Durable Object class under workerd: see
// testdata/workers/durableobject/do.mjs's Counter.
func TestDurableObject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWrangler(t, "durableobject")

	t.Run("counter", func(t *testing.T) {
		// Use a name unique to this test run so repeated `go test`
		// invocations against the same --persist-to don't see stale
		// counts (each run gets its own t.TempDir(), but this also
		// guards against sharing within this subtest's own retries).
		const name = "e2e-counter"

		resp1, body1 := w.Get(t, "/counter?name="+name)
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("first request status = %d, want %d (body = %q)", resp1.StatusCode, http.StatusOK, body1)
		}
		if body1 != "1" {
			t.Errorf("first request body = %q, want %q", body1, "1")
		}

		resp2, body2 := w.Get(t, "/counter?name="+name)
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("second request status = %d, want %d (body = %q)", resp2.StatusCode, http.StatusOK, body2)
		}
		if body2 != "2" {
			t.Errorf("second request body = %q, want %q", body2, "2")
		}

		// A different name must not share state with "e2e-counter".
		resp3, body3 := w.Get(t, "/counter?name=e2e-counter-other")
		if resp3.StatusCode != http.StatusOK {
			t.Fatalf("other-name request status = %d, want %d (body = %q)", resp3.StatusCode, http.StatusOK, body3)
		}
		if body3 != "1" {
			t.Errorf("other-name request body = %q, want %q", body3, "1")
		}
	})
}
