//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// aiResponse mirrors testdata/workers/kitchensink/ai.go.
type aiResponse struct {
	Model  string    `json:"model"`
	Length int       `json:"length"`
	Shape  []float64 `json:"shape"`
}

func testAIEmbeddings(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		// The first remote Workers AI call can sit behind a cold proxy.
		restore := extendClientTimeout(w, 45*time.Second)
		defer restore()

		resp, body := w.Get(t, "/ai")
		skipIfRemoteBindingUnavailable(t, "AI", resp.StatusCode, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", resp.StatusCode, http.StatusOK, body)
		}
		var got aiResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal AI response %q: %v", body, err)
		}
		if got.Model != "@cf/baai/bge-base-en-v1.5" {
			t.Errorf("model = %q, want @cf/baai/bge-base-en-v1.5", got.Model)
		}
		// The vector values change with the model; only the dimension is stable.
		if got.Length <= 0 {
			t.Fatalf("embedding length = %d, want > 0; body = %s", got.Length, body)
		}
		if len(got.Shape) >= 2 && int(got.Shape[len(got.Shape)-1]) != got.Length {
			t.Errorf("shape %v last dim = %v, want length %d", got.Shape, got.Shape[len(got.Shape)-1], got.Length)
		}
	}
}

// extendClientTimeout raises the shared worker client's timeout for one
// slow remote binding call and returns a function that puts it back.
func extendClientTimeout(w *worker, d time.Duration) func() {
	prev := w.Client.Timeout
	w.Client.Timeout = d
	return func() { w.Client.Timeout = prev }
}

// skipIfRemoteBindingUnavailable skips when a local wrangler binding
// proxied the call to Cloudflare and this environment has no account.
// Other failures stay failures: a decoder panic or a deprecated model
// must not be reported as a skip.
func skipIfRemoteBindingUnavailable(t *testing.T, binding string, status int, body string) {
	t.Helper()
	if !remoteBindingUnavailable(status, body) {
		return
	}
	msg := body
	if len(msg) > 500 {
		msg = msg[:500]
	}
	msg = strings.TrimSpace(msg)
	t.Skipf("skipping: %s binding is unavailable without a Cloudflare account (local wrangler dev proxies it remotely): %s", binding, msg)
}

func remoteBindingUnavailable(status int, body string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if status >= 200 && status < 300 {
		return false
	}
	s := strings.ToLower(body)
	for _, needle := range []string{
		"authentication",
		"unauthorized",
		"not logged in",
		"must be logged in",
		"cloudflare_api_token",
		"api token",
		"credentials",
		"auth token",
		"not authenticated",
		"code: 10000",
		"accounts/undefined",
		"no account",
		"remote-only",
		"remote binding",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
