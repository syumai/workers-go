//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// kvListResponse mirrors testdata/workers/kitchensink/main.go's
// kvListResponse.
type kvListResponse struct {
	Keys []string `json:"keys"`
}

func testKVPutGet(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-put-get"
		const value = "put-get-value"

		putResp, _ := w.Do(t, http.MethodPut, "/kv/"+key, nil, strings.NewReader(value))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
		}

		getResp, body := w.Get(t, "/kv/"+key)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
		}
		if body != value {
			t.Errorf("GET body = %q, want %q", body, value)
		}
	}
}

func testKVGetMissing(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		// This key is never written by any subtest in this file.
		const key = "e2e-never-written"

		// KV.get() resolves to null for a missing key, and
		// kv.Namespace.GetString maps that to kv.ErrNotFound. The
		// fixture's GET /kv/{key} handler answers 404 for that error --
		// this subtest confirms that's what actually happens against
		// real KV, not just what the code intends.
		resp, body := w.Get(t, "/kv/"+key)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusNotFound, body)
		}
	}
}

func testKVListPrefix(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		keys := []string{"e2e-list-a", "e2e-list-b"}
		for _, key := range keys {
			resp, _ := w.Do(t, http.MethodPut, "/kv/"+key, nil, strings.NewReader("v"))
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("PUT %s status = %d, want %d", key, resp.StatusCode, http.StatusNoContent)
			}
		}

		resp, body := w.Get(t, "/kv?prefix=e2e-list-")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got kvListResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal list response %q: %v", body, err)
		}
		sort.Strings(got.Keys)
		if !equalStringSlices(got.Keys, keys) {
			t.Errorf("keys = %v, want %v", got.Keys, keys)
		}
	}
}

func testKVGetStrings(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		// An empty string is a present value and must stay in the map.
		// A key that was never written must be omitted: GetStrings has no
		// single ErrNotFound for a mixed batch.
		puts := map[string]string{
			"e2e-bulk-a":     "alpha",
			"e2e-bulk-b":     "beta",
			"e2e-bulk-empty": "",
		}
		for key, value := range puts {
			resp, body := w.Do(t, http.MethodPut, "/kv/"+key, nil, strings.NewReader(value))
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("PUT %s status = %d, want %d (body = %q)", key, resp.StatusCode, http.StatusNoContent, body)
			}
		}

		const reqBody = `{"keys":["e2e-bulk-a","e2e-bulk-b","e2e-bulk-empty","e2e-bulk-missing"]}`
		resp, body := w.Do(t, http.MethodPost, "/kv/get-strings", nil, strings.NewReader(reqBody))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /kv/get-strings status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal get-strings response %q: %v", body, err)
		}
		for key, value := range puts {
			if got[key] != value {
				t.Errorf("GetStrings[%q] = %q, want %q", key, got[key], value)
			}
		}
		if _, ok := got["e2e-bulk-missing"]; ok {
			t.Errorf("GetStrings included %q for a missing key, want it omitted (map = %v)", "e2e-bulk-missing", got)
		}
		if len(got) != len(puts) {
			t.Errorf("GetStrings returned %d keys, want %d (map = %v)", len(got), len(puts), got)
		}
	}
}

func testKVDelete(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-delete-me"

		putResp, _ := w.Do(t, http.MethodPut, "/kv/"+key, nil, strings.NewReader("v"))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
		}

		delResp, _ := w.Do(t, http.MethodDelete, "/kv/"+key, nil, nil)
		if delResp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, want %d", delResp.StatusCode, http.StatusNoContent)
		}

		getResp, body := w.Get(t, "/kv/"+key)
		if getResp.StatusCode != http.StatusNotFound {
			t.Errorf("GET after delete status = %d, want %d (body = %q)", getResp.StatusCode, http.StatusNotFound, body)
		}
	}
}
