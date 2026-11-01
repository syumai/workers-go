//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// workflowPollTimeout bounds how long a subtest waits for an instance to
// leave "queued" or reach a terminal status. The first run pays for wasm
// startup inside workerd's workflow isolate.
const workflowPollTimeout = 45 * time.Second

// workflowStatus mirrors testdata/workers/workflow/main.go's statusResponse.
type workflowStatus struct {
	Status string          `json:"status"`
	Error  *workflowError  `json:"error"`
	Output json.RawMessage `json:"output"`
}

type workflowError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func TestWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWrangler(t, "workflow")

	t.Run("create_batch", testWorkflowCreateBatch(w))
	t.Run("send_event", testWorkflowSendEvent(w))
	t.Run("wait_for_event_timeout", testWorkflowWaitForEventTimeout(w))
	t.Run("pause_resume", testWorkflowPauseResume(w))
	t.Run("terminate", testWorkflowTerminate(w))
}

func testWorkflowCreateBatch(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		suffix := time.Now().UnixNano()
		wantIDs := []string{
			fmt.Sprintf("batch-a-%d", suffix),
			fmt.Sprintf("batch-b-%d", suffix),
			fmt.Sprintf("batch-c-%d", suffix),
		}
		body := map[string]any{
			"instances": []map[string]string{
				{"id": wantIDs[0], "mode": "complete"},
				{"id": wantIDs[1], "mode": "complete"},
				{"id": wantIDs[2], "mode": "complete"},
			},
		}
		resp, raw := postJSON(t, w, "/batch", body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /batch status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, raw)
		}
		var created struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal([]byte(raw), &created); err != nil {
			t.Fatalf("failed to unmarshal /batch response %q: %v", raw, err)
		}
		if len(created.IDs) != len(wantIDs) {
			t.Fatalf("CreateBatch returned %d ids, want %d (%q)", len(created.IDs), len(wantIDs), raw)
		}
		for i, id := range wantIDs {
			if created.IDs[i] != id {
				t.Errorf("ids[%d] = %q, want %q", i, created.IDs[i], id)
			}
		}

		for _, id := range wantIDs {
			st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
				return st.Status == "complete" || st.Status == "errored" || st.Status == "terminated"
			})
			if st.Status != "complete" {
				t.Fatalf("instance %s status = %q, want %q (body = %+v)", id, st.Status, "complete", st)
			}
			var output map[string]bool
			if err := json.Unmarshal(st.Output, &output); err != nil {
				t.Fatalf("instance %s output %s: %v", id, st.Output, err)
			}
			if !output["ok"] {
				t.Errorf("instance %s output = %s, want {\"ok\":true}", id, st.Output)
			}
		}
	}
}

func testWorkflowSendEvent(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		id := fmt.Sprintf("wait-%d", time.Now().UnixNano())
		createWorkflow(t, w, id, "wait")
		// The event is only delivered once the step is inside WaitForEvent.
		// "waiting" is that state under workerd; "running" covers the window
		// before the engine publishes the wait.
		st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
			switch st.Status {
			case "waiting", "running", "paused", "complete", "errored", "terminated":
				return true
			default:
				return false
			}
		})
		if st.Status == "complete" || st.Status == "errored" || st.Status == "terminated" {
			t.Fatalf("instance %s reached %q before SendEvent (output = %s, error = %+v)", id, st.Status, st.Output, st.Error)
		}

		payload := map[string]string{"message": "approved-by-e2e"}
		resp, raw := postJSON(t, w, "/event?id="+id, map[string]any{
			"type":    "approved",
			"payload": payload,
		})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /event status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNoContent, raw)
		}

		st = pollWorkflow(t, w, id, func(st workflowStatus) bool {
			return st.Status == "complete" || st.Status == "errored" || st.Status == "terminated"
		})
		if st.Status != "complete" {
			t.Fatalf("instance %s status = %q, want %q (output = %s, error = %+v)", id, st.Status, "complete", st.Output, st.Error)
		}
		var output struct {
			Type    string `json:"type"`
			Payload struct {
				Message string `json:"message"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(st.Output, &output); err != nil {
			t.Fatalf("instance %s output %s: %v", id, st.Output, err)
		}
		if output.Type != "approved" {
			t.Errorf("output.type = %q, want %q", output.Type, "approved")
		}
		if output.Payload.Message != payload["message"] {
			t.Errorf("output.payload.message = %q, want %q", output.Payload.Message, payload["message"])
		}
	}
}

func testWorkflowWaitForEventTimeout(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		id := fmt.Sprintf("timeout-%d", time.Now().UnixNano())
		createWorkflow(t, w, id, "timeout")
		st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
			return st.Status == "complete" || st.Status == "errored" || st.Status == "terminated"
		})
		if st.Status != "complete" {
			t.Fatalf("instance %s status = %q, want %q (output = %s, error = %+v)", id, st.Status, "complete", st.Output, st.Error)
		}
		var output struct {
			TimedOut bool   `json:"timedOut"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(st.Output, &output); err != nil {
			t.Fatalf("instance %s output %s: %v", id, st.Output, err)
		}
		if !output.TimedOut {
			t.Fatalf("WaitForEvent timeout output = %+v, want timedOut true", output)
		}
		if output.Error == "" {
			t.Error("WaitForEvent timeout error is empty")
		}
	}
}

func testWorkflowPauseResume(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		id := fmt.Sprintf("pause-%d", time.Now().UnixNano())
		createWorkflow(t, w, id, "sleep")
		waitUntilHolding(t, w, id)

		resp, raw := postJSON(t, w, "/pause?id="+id, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /pause status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNoContent, raw)
		}
		st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
			return st.Status == "paused" || st.Status == "complete" || st.Status == "errored" || st.Status == "terminated"
		})
		if st.Status != "paused" {
			t.Fatalf("instance %s status = %q, want %q", id, st.Status, "paused")
		}

		resp, raw = postJSON(t, w, "/resume?id="+id, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /resume status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNoContent, raw)
		}
		st = pollWorkflow(t, w, id, func(st workflowStatus) bool {
			switch st.Status {
			case "paused", "waitingForPause":
				return false
			default:
				return st.Status != ""
			}
		})
		switch st.Status {
		case "running", "waiting", "queued":
		default:
			t.Fatalf("instance %s status after resume = %q, want running, waiting, or queued", id, st.Status)
		}
	}
}

func testWorkflowTerminate(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		id := fmt.Sprintf("term-%d", time.Now().UnixNano())
		createWorkflow(t, w, id, "sleep")
		waitUntilHolding(t, w, id)

		resp, raw := postJSON(t, w, "/terminate?id="+id, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("POST /terminate status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNoContent, raw)
		}
		st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
			return st.Status == "terminated" || st.Status == "complete" || st.Status == "errored"
		})
		if st.Status != "terminated" {
			t.Fatalf("instance %s status = %q, want %q (output = %s, error = %+v)", id, st.Status, "terminated", st.Output, st.Error)
		}
	}
}

func createWorkflow(t *testing.T, w *worker, id, mode string) {
	t.Helper()
	resp, raw := postJSON(t, w, "/create", map[string]string{"id": id, "mode": mode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /create status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("failed to unmarshal /create response %q: %v", raw, err)
	}
	if created.ID != id {
		t.Fatalf("created id = %q, want %q", created.ID, id)
	}
}

// waitUntilHolding polls until the instance is in a non-terminal state that
// pause and terminate can act on. A 60s Sleep spends that time "waiting".
func waitUntilHolding(t *testing.T, w *worker, id string) {
	t.Helper()
	st := pollWorkflow(t, w, id, func(st workflowStatus) bool {
		switch st.Status {
		case "waiting", "running", "paused", "complete", "errored", "terminated":
			return true
		default:
			return false
		}
	})
	switch st.Status {
	case "waiting", "running":
	default:
		t.Fatalf("instance %s status = %q, want waiting or running before pause/terminate", id, st.Status)
	}
}

func pollWorkflow(t *testing.T, w *worker, id string, done func(workflowStatus) bool) workflowStatus {
	t.Helper()
	deadline := time.Now().Add(workflowPollTimeout)
	var last string
	for {
		resp, body := w.Get(t, "/status?id="+id)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /status?id=%s status = %d, want %d (body = %q)", id, resp.StatusCode, http.StatusOK, body)
		}
		last = body
		var st workflowStatus
		if err := json.Unmarshal([]byte(body), &st); err != nil {
			t.Fatalf("failed to unmarshal status %q: %v", body, err)
		}
		if done(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for instance %s; last status = %s", workflowPollTimeout, id, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func postJSON(t *testing.T, w *worker, path string, v any) (*http.Response, string) {
	t.Helper()
	var body *bytes.Reader
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	return w.Do(t, http.MethodPost, path, headers, body)
}
