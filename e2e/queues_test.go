//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// queueReceivedResponse mirrors testdata/workers/kitchensink/queue.go's
// queueReceivedResponse.
type queueReceivedResponse struct {
	Messages []string `json:"messages"`
}

// queuePollTimeout bounds how long testQueueRoundtrip waits for the
// consumer to have written a sent message to KV.
const queuePollTimeout = 15 * time.Second

func testQueueRoundtrip(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		// A payload unique to this test run, so a previous run's leftover
		// KV state (this fixture's queue consumer never deletes what it
		// writes) can't produce a false positive.
		want := fmt.Sprintf("e2e-queue-message-%d", time.Now().UnixNano())

		sendResp, sendBody := w.Do(t, http.MethodPost, "/queue/send", nil, strings.NewReader(want))
		if sendResp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /queue/send status = %d, want %d (body = %q)", sendResp.StatusCode, http.StatusNoContent, sendBody)
		}

		deadline := time.Now().Add(queuePollTimeout)
		var lastBody string
		for time.Now().Before(deadline) {
			resp, body := w.Get(t, "/queue/received")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /queue/received status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
			}
			lastBody = body
			var got queueReceivedResponse
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("failed to unmarshal queue/received response %q: %v", body, err)
			}
			for _, msg := range got.Messages {
				if msg == want {
					return // success
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out after %s waiting for queue message %q to be received; last /queue/received body = %q", queuePollTimeout, want, lastBody)
	}
}
