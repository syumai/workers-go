// Command analytics writes one Analytics Engine data point per request
// (exp/cloudflare/analytics).
//
// GET / calls WriteDataPoint with a fixed index, the request method and
// path as blobs, and the double 1. Local wrangler dev accepts the call and
// discards the point, so the JSON body is the point that was submitted,
// not a row read back from the dataset.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/analytics"
)

const bindingName = "ANALYTICS"

type response struct {
	OK      bool      `json:"ok"`
	Indexes []string  `json:"indexes"`
	Blobs   []string  `json:"blobs"`
	Doubles []float64 `json:"doubles"`
}

func main() {
	workers.Serve(http.HandlerFunc(handle))
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	dataset, err := analytics.NewAnalyticsEngineDataset(bindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	point := analytics.AnalyticsEngineDataPoint{
		Indexes: []string{"example"},
		Blobs:   []string{r.Method, r.URL.Path},
		Doubles: []float64{1},
	}
	if err := dataset.WriteDataPoint(point); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response{
		OK:      true,
		Indexes: point.Indexes,
		Blobs:   point.Blobs,
		Doubles: point.Doubles,
	}); err != nil {
		log.Println(err)
	}
}
