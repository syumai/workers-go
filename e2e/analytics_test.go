//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
)

// analyticsResponse mirrors testdata/workers/kitchensink/analytics.go.
// Local Analytics Engine accepts writeDataPoint and discards the point.
// There is no binding method or local SQL API that reads it back, so the
// test only checks that the write call succeeded.
type analyticsResponse struct {
	OK bool `json:"ok"`
}

func testAnalyticsWrite(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		resp, body := w.Get(t, "/analytics")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		var got analyticsResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal /analytics response %q: %v", body, err)
		}
		if !got.OK {
			t.Fatalf("ok = false, want true (body = %q)", body)
		}
	}
}
