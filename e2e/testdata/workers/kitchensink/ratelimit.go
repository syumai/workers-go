//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go/exp/cloudflare/ratelimit"
)

// rateLimitBinding is the ratelimits binding name declared in
// wrangler.jsonc. The configured simple limit is intentionally tiny so the
// e2e test can observe both an allowed call and a rejected one.
const rateLimitBinding = "RATE_LIMITER"

// rateLimitResponse is the JSON shape of GET /ratelimit: the Success field
// RateLimit.Limit returns. Assertions live in the e2e test.
type rateLimitResponse struct {
	Success bool `json:"success"`
}

// handleRateLimit increments the rate limit counter for the query key and
// returns whether the call was under the limit. A missing key is 400; a
// binding or Limit error is 500 with the error text.
func handleRateLimit(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	rl, err := ratelimit.NewRateLimit(rateLimitBinding)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	outcome, err := rl.Limit(ratelimit.RateLimitOptions{Key: key})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rateLimitResponse{Success: outcome.Success}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
