//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// cronEventResponse mirrors testdata/workers/kitchensink/cron.go's
// cronEventResponse, stored as the KV value at "cron:last".
type cronEventResponse struct {
	Cron          string `json:"cron"`
	ScheduledTime string `json:"scheduledTime"`
}

// cronPollTimeout bounds how long testCronFires waits, after triggering
// wrangler's local cron simulation, for the scheduled task to have written
// its result to KV.
const cronPollTimeout = 5 * time.Second

func testCronFires(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const wantCron = "* * * * *" // must match wrangler.jsonc's triggers.crons entry

		schedResp, schedBody := w.Scheduled(t, wantCron)
		if schedResp.StatusCode != http.StatusOK {
			t.Fatalf("GET /__scheduled status = %d, want %d (body = %q)", schedResp.StatusCode, http.StatusOK, schedBody)
		}

		deadline := time.Now().Add(cronPollTimeout)
		var lastResp *http.Response
		var lastBody string
		for time.Now().Before(deadline) {
			lastResp, lastBody = w.Get(t, "/kv/cron:last")
			if lastResp.StatusCode == http.StatusOK {
				var got cronEventResponse
				if err := json.Unmarshal([]byte(lastBody), &got); err != nil {
					t.Fatalf("failed to unmarshal cron:last KV value %q: %v", lastBody, err)
				}
				if got.Cron != wantCron {
					t.Fatalf("cron = %q, want %q", got.Cron, wantCron)
				}
				if got.ScheduledTime == "" {
					t.Fatalf("scheduledTime is empty, want a non-empty RFC3339 timestamp")
				}
				if _, err := time.Parse(time.RFC3339, got.ScheduledTime); err != nil {
					t.Fatalf("scheduledTime = %q is not RFC3339: %v", got.ScheduledTime, err)
				}
				return // success
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out after %s waiting for cron:last KV value; last GET /kv/cron:last status = %d, body = %q", cronPollTimeout, lastResp.StatusCode, lastBody)
	}
}
