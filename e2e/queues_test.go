//go:build e2e

package e2e

import (
	"encoding/base64"
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

// queueMetaResponse mirrors testdata/workers/kitchensink/queue.go.
type queueMetaResponse struct {
	Messages []queueReceipt `json:"messages"`
}

// queueReceipt mirrors the consumer's observation of a delivered message.
type queueReceipt struct {
	ID        string `json:"id"`
	BodyKind  string `json:"bodyKind"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp"`
}

// queueBatchDelay is long enough that a message delivered immediately is
// distinguishable from one held by delaySeconds, and short enough that
// the poll below still finishes inside queuePollTimeout.
const queueBatchDelay = 2

// testQueueSendBatchContentTypes sends text, json, and bytes in one
// SendBatch and checks the consumer saw those JS body kinds. bytes is the
// check that contentType was actually applied: the default is json, and a
// Uint8Array sent as json does not come back as raw bytes.
func testQueueSendBatchContentTypes(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		marker := fmt.Sprintf("e2e-batch-ct-%d", time.Now().UnixNano())
		rawBytes := []byte{0x01, 0x02, 0xff}
		payload := map[string]any{
			"messages": []map[string]any{
				{"contentType": "text", "text": marker},
				{"contentType": "json", "json": map[string]any{"k": marker, "n": 1}},
				{"contentType": "bytes", "bytesBase64": base64.StdEncoding.EncodeToString(rawBytes)},
			},
		}
		sendQueueBatch(t, w, payload)

		deadline := time.Now().Add(queuePollTimeout)
		var lastBody string
		for time.Now().Before(deadline) {
			resp, body := w.Get(t, "/queue/meta")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /queue/meta status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
			}
			lastBody = body
			var got queueMetaResponse
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("failed to unmarshal queue/meta response %q: %v", body, err)
			}
			var sawText, sawJSON, sawBytes bool
			for _, msg := range got.Messages {
				switch msg.BodyKind {
				case "string":
					if msg.Body == marker {
						sawText = true
					}
				case "object":
					var obj map[string]any
					if err := json.Unmarshal([]byte(msg.Body), &obj); err != nil {
						continue
					}
					if obj["k"] == marker && obj["n"] == float64(1) {
						sawJSON = true
					}
				case "bytes":
					decoded, err := base64.StdEncoding.DecodeString(msg.Body)
					if err != nil {
						t.Fatalf("bytes receipt is not base64: %q", msg.Body)
					}
					if string(decoded) == string(rawBytes) {
						sawBytes = true
					}
				}
			}
			if sawText && sawJSON && sawBytes {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out after %s waiting for send-batch content types; last /queue/meta body = %q", queuePollTimeout, lastBody)
	}
}

// testQueueSendBatchDelay checks per-message delaySeconds and the
// batch-level delaySeconds option. Both messages must stay undelivered
// through the early window, then show up before queuePollTimeout.
func testQueueSendBatchDelay(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		now := time.Now().UnixNano()
		immediate := fmt.Sprintf("e2e-batch-now-%d", now)
		perMessage := fmt.Sprintf("e2e-batch-msg-delay-%d", now)
		batchDelayed := fmt.Sprintf("e2e-batch-opt-delay-%d", now)

		sendQueueBatch(t, w, map[string]any{
			"messages": []map[string]any{
				{"contentType": "text", "text": immediate},
				{"contentType": "text", "text": perMessage, "delaySeconds": queueBatchDelay},
			},
		})
		sendQueueBatch(t, w, map[string]any{
			"delaySeconds": queueBatchDelay,
			"messages": []map[string]any{
				{"contentType": "text", "text": batchDelayed},
			},
		})

		start := time.Now()
		early := start.Add(900 * time.Millisecond)
		for time.Now().Before(early) {
			msgs := queueReceivedMessages(t, w)
			if containsString(msgs, perMessage) || containsString(msgs, batchDelayed) {
				t.Fatalf("delayed message delivered after %s; delaySeconds=%d was not applied (messages = %v)", time.Since(start), queueBatchDelay, msgs)
			}
			time.Sleep(100 * time.Millisecond)
		}

		deadline := start.Add(queuePollTimeout)
		var sawImmediate, sawPer, sawBatch bool
		var perAt, batchAt time.Duration
		var last []string
		for time.Now().Before(deadline) {
			last = queueReceivedMessages(t, w)
			if containsString(last, immediate) {
				sawImmediate = true
			}
			if containsString(last, perMessage) && !sawPer {
				sawPer = true
				perAt = time.Since(start)
			}
			if containsString(last, batchDelayed) && !sawBatch {
				sawBatch = true
				batchAt = time.Since(start)
			}
			if sawImmediate && sawPer && sawBatch {
				const minDelay = 1500 * time.Millisecond
				if perAt < minDelay {
					t.Errorf("per-message delay delivered in %s, want >= %s", perAt, minDelay)
				}
				if batchAt < minDelay {
					t.Errorf("batch delaySeconds delivered in %s, want >= %s", batchAt, minDelay)
				}
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out after %s waiting for delayed batch messages (immediate=%v perMessage=%v batch=%v); last = %v", queuePollTimeout, sawImmediate, sawPer, sawBatch, last)
	}
}

func sendQueueBatch(t *testing.T, w *worker, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal send-batch body: %v", err)
	}
	resp, body := w.Do(t, http.MethodPost, "/queue/send-batch", nil, strings.NewReader(string(raw)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /queue/send-batch status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNoContent, body)
	}
}

func queueReceivedMessages(t *testing.T, w *worker) []string {
	t.Helper()
	resp, body := w.Get(t, "/queue/received")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /queue/received status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
	}
	var got queueReceivedResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to unmarshal queue/received response %q: %v", body, err)
	}
	return got.Messages
}
