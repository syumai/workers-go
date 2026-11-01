//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// imagesResponse mirrors testdata/workers/kitchensink/images.go.
type imagesResponse struct {
	Format        string  `json:"format"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	FileSize      float64 `json:"fileSize"`
	ContentType   string  `json:"contentType"`
	ResizedBytes  int     `json:"resizedBytes"`
	ResizedWidth  float64 `json:"resizedWidth"`
	ResizedHeight float64 `json:"resizedHeight"`
}

func testImagesInfoResize(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		restore := extendClientTimeout(w, 45*time.Second)
		defer restore()

		resp, body := w.Get(t, "/images")
		skipIfRemoteBindingUnavailable(t, "Images", resp.StatusCode, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", resp.StatusCode, http.StatusOK, body)
		}
		var got imagesResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal Images response %q: %v", body, err)
		}
		if !strings.Contains(strings.ToLower(got.Format), "png") {
			t.Errorf("format = %q, want a png format", got.Format)
		}
		if got.Width != 1 || got.Height != 1 {
			t.Errorf("info size = %v×%v, want 1×1", got.Width, got.Height)
		}
		if got.FileSize <= 0 {
			t.Errorf("fileSize = %v, want > 0", got.FileSize)
		}
		if !strings.Contains(strings.ToLower(got.ContentType), "png") {
			t.Errorf("content type = %q, want a png type", got.ContentType)
		}
		if got.ResizedBytes <= 0 {
			t.Errorf("resizedBytes = %d, want > 0", got.ResizedBytes)
		}
		if got.ResizedWidth != 8 {
			t.Errorf("resized width = %v, want 8", got.ResizedWidth)
		}
		if got.ResizedHeight <= 0 {
			t.Errorf("resized height = %v, want > 0", got.ResizedHeight)
		}
	}
}
