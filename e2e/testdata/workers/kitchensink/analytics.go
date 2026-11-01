//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go/exp/cloudflare/analytics"
)

// analyticsBinding is the analytics_engine_datasets binding name declared
// in wrangler.jsonc.
const analyticsBinding = "ANALYTICS"

// analyticsResponse is the JSON shape of GET /analytics. ok is true only
// after WriteDataPoint returns. Local wrangler dev accepts the call and
// drops the point, so this handler cannot return the stored row.
type analyticsResponse struct {
	OK bool `json:"ok"`
}

// handleAnalytics writes one data point with an index, two blobs, and two
// doubles. A resolve or write error is returned as 500 with the error text
// so the e2e test can see what wrangler dev produced.
func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	dataset, err := analytics.NewAnalyticsEngineDataset(analyticsBinding)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = dataset.WriteDataPoint(analytics.AnalyticsEngineDataPoint{
		Indexes: []string{"e2e"},
		Blobs:   []string{"kitchensink", r.URL.Path},
		Doubles: []float64{1, 2.5},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(analyticsResponse{OK: true}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
