//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// r2ListResponse mirrors testdata/workers/kitchensink/r2.go's
// r2ListResponse.
type r2ListResponse struct {
	Keys []string `json:"keys"`
}

func testR2PutGetRoundtrip(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-r2-put-get"
		const value = "r2 put/get roundtrip value"

		headers := http.Header{}
		headers.Set("X-Content-Type", "text/x-e2e-marker")
		headers.Set("X-Meta-Color", "teal")
		headers.Set("X-Meta-Origin", "e2e-test")
		putResp, _ := w.Do(t, http.MethodPut, "/r2/"+key, headers, strings.NewReader(value))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
		}

		getResp, body := w.Get(t, "/r2/"+key)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
		}
		if body != value {
			t.Errorf("GET body = %q, want %q", body, value)
		}
		if got := getResp.Header.Get("X-Content-Type"); got != "text/x-e2e-marker" {
			t.Errorf("GET X-Content-Type = %q, want %q", got, "text/x-e2e-marker")
		}
		if got := getResp.Header.Get("X-Meta-Color"); got != "teal" {
			t.Errorf("GET X-Meta-Color = %q, want %q", got, "teal")
		}
		if got := getResp.Header.Get("X-Meta-Origin"); got != "e2e-test" {
			t.Errorf("GET X-Meta-Origin = %q, want %q", got, "e2e-test")
		}
	}
}

func testR2Head(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-r2-head"
		const value = "r2 head value, longer than its metadata"

		headers := http.Header{}
		headers.Set("X-Content-Type", "text/x-e2e-head")
		putResp, _ := w.Do(t, http.MethodPut, "/r2/"+key, headers, strings.NewReader(value))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
		}

		headResp, body := w.Do(t, http.MethodHead, "/r2/"+key, nil, nil)
		if headResp.StatusCode != http.StatusOK {
			t.Fatalf("HEAD status = %d, want %d", headResp.StatusCode, http.StatusOK)
		}
		if body != "" {
			t.Errorf("HEAD body = %q, want empty", body)
		}
		if got := headResp.Header.Get("X-Content-Type"); got != "text/x-e2e-head" {
			t.Errorf("HEAD X-Content-Type = %q, want %q", got, "text/x-e2e-head")
		}
	}
}

func testR2GetMissing(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		// Never written by any subtest in this file.
		const key = "e2e-r2-never-written"

		resp, body := w.Get(t, "/r2/"+key)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNotFound, body)
		}
	}
}

func testR2Delete(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-r2-delete-me"

		putResp, _ := w.Do(t, http.MethodPut, "/r2/"+key, nil, strings.NewReader("v"))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
		}

		delResp, _ := w.Do(t, http.MethodDelete, "/r2/"+key, nil, nil)
		if delResp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, want %d", delResp.StatusCode, http.StatusNoContent)
		}

		getResp, body := w.Get(t, "/r2/"+key)
		if getResp.StatusCode != http.StatusNotFound {
			t.Errorf("GET after delete status = %d, want %d (body = %q)", getResp.StatusCode, http.StatusNotFound, body)
		}
	}
}

func testR2List(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		keys := []string{"e2e-r2-list-a", "e2e-r2-list-b"}
		for _, key := range keys {
			resp, _ := w.Do(t, http.MethodPut, "/r2/"+key, nil, strings.NewReader("v"))
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("PUT %s status = %d, want %d", key, resp.StatusCode, http.StatusNoContent)
			}
		}

		resp, body := w.Get(t, "/r2")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got r2ListResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal list response %q: %v", body, err)
		}
		for _, key := range keys {
			if !containsString(got.Keys, key) {
				t.Errorf("keys = %v, want to contain %q", got.Keys, key)
			}
		}
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
