//go:build e2e

package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// testMalformedURL sends request targets with invalid percent-escapes. Go's
// http.Client refuses to send them, so the request line is written to a raw
// TCP connection. workerd forwards the target verbatim in request.url, but
// Go's url.Parse rejects it; like net/http's own server, the worker must
// answer 400 Bad Request instead of failing the request with a 500.
func testMalformedURL(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		u, err := url.Parse(w.BaseURL)
		if err != nil {
			t.Fatalf("failed to parse base URL %q: %v", w.BaseURL, err)
		}
		for _, target := range []string{"/healthz%zz", "/%"} {
			t.Run(target, func(t *testing.T) {
				conn, err := net.DialTimeout("tcp", u.Host, requestTimeout)
				if err != nil {
					t.Fatalf("failed to dial %s: %v", u.Host, err)
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(requestTimeout))
				fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target, u.Host)
				resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil {
					t.Fatalf("failed to read response: %v", err)
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("GET %s status = %d, want %d (body = %q)", target, resp.StatusCode, http.StatusBadRequest, b)
				}
			})
		}
		// The worker must keep serving normal requests afterwards.
		if resp, body := w.Get(t, "/healthz"); resp.StatusCode != http.StatusOK {
			t.Errorf("GET /healthz after malformed requests: status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
	}
}
