//go:build e2e

package e2e

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// fetchCancelResult mirrors testdata/workers/kitchensink/fetchcancel.go.
type fetchCancelResult struct {
	Stage            string `json:"stage"`
	DeadlineExceeded bool   `json:"deadlineExceeded"`
	Err              string `json:"err"`
	ElapsedMs        int64  `json:"elapsedMs"`
}

// stallServer starts a local origin whose handlers never finish: /headers
// sends nothing, /body sends headers and one chunk and then blocks. It
// records whether the client dropped the connection, which only happens if
// the Worker's fetch was really aborted (not just abandoned on the Go
// side).
func stallServer(t *testing.T) (base string, dropped <-chan string) {
	t.Helper()
	drop := make(chan string, 4)
	release := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
		}
		select {
		case <-r.Context().Done():
			drop <- r.URL.Path
		case <-release:
		}
	}))
	// Listen on all loopback interfaces: workerd reaches 127.0.0.1.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.Listener = l
	srv.Start()
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv.URL, drop
}

func testFetchCancel(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		base, dropped := stallServer(t)

		for _, stage := range []string{"headers", "body"} {
			t.Run(stage, func(t *testing.T) {
				q := url.Values{}
				q.Set("url", base+"/"+stage)
				q.Set("timeout", "500ms")
				start := time.Now()
				resp, body := w.Get(t, "/fetch/cancel?"+q.Encode())
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
				}
				// The origin never finishes, so a prompt response proves
				// the context ended the fetch.
				if elapsed := time.Since(start); elapsed > 10*time.Second {
					t.Errorf("request took %v; fetch was not interrupted", elapsed)
				}
				var got fetchCancelResult
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("unmarshal %q: %v", body, err)
				}
				if got.Stage != stage {
					t.Errorf("stage = %q, want %q (err = %q)", got.Stage, stage, got.Err)
				}
				if !got.DeadlineExceeded {
					t.Errorf("deadlineExceeded = false, err = %q", got.Err)
				}

				// The abort must reach the origin: workerd closing the
				// connection is what frees the remote resources.
				select {
				case path := <-dropped:
					if path != "/"+stage {
						t.Errorf("dropped path = %q, want %q", path, "/"+stage)
					}
				case <-time.After(10 * time.Second):
					t.Error("origin never saw the connection close; fetch was not aborted")
				}
			})
		}
	}
}
