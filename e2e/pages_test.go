//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestPages checks that the Go SDK's Pages Functions entry point
// (workers.Serve's onRequest export) works under a real `wrangler pages
// dev`, including resolving the app.wasm import from a Pages Function.
func TestPages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWranglerPages(t, "pages")

	t.Run("onRequest", func(t *testing.T) {
		resp, body := w.Get(t, "/api/hello")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		const want = "hello from pages"
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})
}
