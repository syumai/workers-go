//go:build e2e

package e2e

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestKitchenSink subtests below reproduce the reports tracked by #223:
//
//   - #101: a HandlerFunc that writes no response body. The original
//     report was a handler that does nothing at all. The same
//     "reading 'exports'" TypeError still happened for null-body statuses
//     (204/205/304), which also write no body.
//   - #163: syscall/js.valueNew ("reading 'exports'") after the handler
//     has returned. The callback constructs a JavaScript object once the
//     response is out and records that in KV.

func testIssue101Empty(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		resp, body := w.Get(t, "/issue101/empty")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "" {
			t.Errorf("body = %q, want empty", body)
		}
	}
}

func testIssue101Nobody(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("options", func(t *testing.T) {
			resp, body := w.Do(t, http.MethodOptions, "/issue101/nobody", nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
			}
			if body != "" {
				t.Errorf("body = %q, want empty", body)
			}
			if got := resp.Header.Get("X-Empty"); got != "1" {
				t.Errorf("X-Empty = %q, want %q", got, "1")
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
			}
		})

		// 200 writes an empty stream. 204/205/304 are null-body statuses:
		// before the Done() signal was deferred to a later turn, building
		// that Response exited the instance inside valueNew.
		for _, status := range []int{
			http.StatusOK,
			http.StatusNoContent,
			http.StatusResetContent,
			http.StatusNotModified,
		} {
			t.Run(strconv.Itoa(status), func(t *testing.T) {
				resp, body := w.Get(t, "/issue101/nobody?status="+strconv.Itoa(status))
				if resp.StatusCode != status {
					t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, status, body)
				}
				if body != "" {
					t.Errorf("body = %q, want empty", body)
				}
				if got := resp.Header.Get("X-Empty"); got != "1" {
					t.Errorf("X-Empty = %q, want %q", got, "1")
				}
			})
		}
	}
}

func testIssue163CallbackAfterResponse(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		resp, body := w.Get(t, "/issue163")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /issue163 status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "scheduled" {
			t.Errorf("body = %q, want %q", body, "scheduled")
		}
		if got := resp.Header.Get("X-Issue"); got != "163" {
			t.Errorf("X-Issue = %q, want %q", got, "163")
		}

		deadline := time.Now().Add(waitUntilPollTimeout)
		var lastResp *http.Response
		var lastBody string
		for time.Now().Before(deadline) {
			lastResp, lastBody = w.Get(t, "/issue163/result")
			if lastResp.StatusCode == http.StatusOK && lastBody == "object" {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("post-response valueNew callback did not write %q to KV within %v; last GET /issue163/result status = %d, body = %q", "object", waitUntilPollTimeout, lastResp.StatusCode, lastBody)
	}
}
