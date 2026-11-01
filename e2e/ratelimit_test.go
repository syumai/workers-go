//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// rateLimitSimpleLimit is the simple.limit configured on the kitchensink
// RATE_LIMITER binding. The first this-many calls for a fresh key must
// succeed; the next call must be rejected. Local miniflare enforces this
// exactly for sequential calls inside the period.
const rateLimitSimpleLimit = 2

// rateLimitResponse mirrors testdata/workers/kitchensink/ratelimit.go.
type rateLimitResponse struct {
	Success bool `json:"success"`
}

func testRateLimit(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		// A fresh key per run so a previous kitchensink process (or a
		// persisted counter under --persist-to) cannot exhaust the limit
		// before this subtest starts. TempDir persistence is already
		// per-test, but the key stays unique anyway.
		key := "e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		path := "/ratelimit?key=" + url.QueryEscape(key)

		for i := 1; i <= rateLimitSimpleLimit; i++ {
			got := getRateLimit(t, w, path)
			if !got.Success {
				t.Fatalf("call %d/%d success = false, want true", i, rateLimitSimpleLimit)
			}
		}

		got := getRateLimit(t, w, path)
		if got.Success {
			t.Fatalf("call %d success = true, want false once the limit of %d is exceeded", rateLimitSimpleLimit+1, rateLimitSimpleLimit)
		}
	}
}

func getRateLimit(t *testing.T, w *worker, path string) rateLimitResponse {
	t.Helper()
	resp, body := w.Get(t, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
	}
	var got rateLimitResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to unmarshal /ratelimit response %q: %v", body, err)
	}
	return got
}
