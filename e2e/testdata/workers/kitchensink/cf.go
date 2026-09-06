//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

// handleCF reports the incoming request's Cloudflare-specific properties
// (request.cf). Under `wrangler dev` without --remote, request.cf may or
// may not be populated depending on the wrangler version; this handler
// surfaces whatever fetch.NewIncomingProperties actually returns (200 with
// the properties, or 500 with its error) so the e2e test can pin down the
// real local-dev behavior instead of assuming it.
func handleCF(w http.ResponseWriter, r *http.Request) {
	props, err := fetch.NewIncomingProperties(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(props); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
