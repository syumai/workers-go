//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
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

// r2MultipartCreateResponse mirrors
// testdata/workers/kitchensink/r2multipart.go.
type r2MultipartCreateResponse struct {
	Key      string `json:"key"`
	UploadID string `json:"uploadId"`
}

type r2UploadedPartJSON struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

type r2MultipartCompleteResponse struct {
	Key            string            `json:"key"`
	Size           int               `json:"size"`
	ETag           string            `json:"etag"`
	ContentType    string            `json:"contentType"`
	CustomMetadata map[string]string `json:"customMetadata"`
}

// testR2MultipartComplete uploads two parts out of order, completes them,
// and checks the assembled object. Part 2 is uploaded first so a binding
// that ignored part numbers would concatenate in upload order.
func testR2MultipartComplete(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-r2-multipart"
		// Every part except the last must be at least 5 MiB. Part 1 is
		// that minimum; part 2 is the short tail. Part 2 is uploaded
		// first so a binding that ignored part numbers would assemble
		// in upload order instead of part-number order.
		part1 := strings.Repeat("a", 5<<20)
		const part2 = "world"
		const contentType = "text/x-e2e-multipart"

		created := r2MultipartCreate(t, w, key, contentType, map[string]string{"Color": "teal"})
		if created.Key != key {
			t.Errorf("create key = %q, want %q", created.Key, key)
		}
		if !wellFormedVersionString(created.UploadID, false) {
			t.Fatalf("uploadId = %q, want a non-empty plain string", created.UploadID)
		}

		second := r2MultipartUploadPart(t, w, key, created.UploadID, 2, part2)
		first := r2MultipartUploadPart(t, w, key, created.UploadID, 1, part1)
		if first.PartNumber != 1 || second.PartNumber != 2 {
			t.Fatalf("part numbers = %d, %d, want 1, 2", first.PartNumber, second.PartNumber)
		}
		if !wellFormedVersionString(first.ETag, false) || !wellFormedVersionString(second.ETag, false) {
			t.Fatalf("etags = %q, %q, want plain strings", first.ETag, second.ETag)
		}

		completed := r2MultipartComplete(t, w, key, created.UploadID, []r2UploadedPartJSON{first, second})
		if completed.Key != key {
			t.Errorf("complete key = %q, want %q", completed.Key, key)
		}
		if completed.Size != len(part1)+len(part2) {
			t.Errorf("complete size = %d, want %d", completed.Size, len(part1)+len(part2))
		}
		if !wellFormedVersionString(completed.ETag, false) {
			t.Errorf("complete etag = %q, want a plain string", completed.ETag)
		}
		if completed.ContentType != contentType {
			t.Errorf("complete contentType = %q, want %q", completed.ContentType, contentType)
		}
		if completed.CustomMetadata["Color"] != "teal" {
			t.Errorf("complete customMetadata = %v, want Color=teal", completed.CustomMetadata)
		}

		getResp, body := w.Get(t, "/r2/"+key)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("GET status = %d, want %d (body = %q)", getResp.StatusCode, http.StatusOK, body)
		}
		wantLen := len(part1) + len(part2)
		if len(body) != wantLen || !strings.HasPrefix(body, part1[:32]) || !strings.HasSuffix(body, part2) {
			t.Errorf("GET body length = %d, want %d (%d bytes of 'a' then %q)", len(body), wantLen, len(part1), part2)
		}
		if got := getResp.Header.Get("X-Content-Type"); got != contentType {
			t.Errorf("GET X-Content-Type = %q, want %q", got, contentType)
		}
		if got := getResp.Header.Get("X-Meta-Color"); got != "teal" {
			t.Errorf("GET X-Meta-Color = %q, want %q", got, "teal")
		}
	}
}

// testR2MultipartAbort starts an upload, writes a part, aborts, and checks
// that the key stays absent and complete then fails.
func testR2MultipartAbort(w *worker) func(t *testing.T) {
	return func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping e2e test in short mode")
		}
		const key = "e2e-r2-multipart-abort"

		created := r2MultipartCreate(t, w, key, "", nil)
		part := r2MultipartUploadPart(t, w, key, created.UploadID, 1, "abandoned")

		abortRaw, err := json.Marshal(map[string]string{
			"key":      key,
			"uploadId": created.UploadID,
		})
		if err != nil {
			t.Fatalf("marshal abort body: %v", err)
		}
		abortResp, abortText := w.Do(t, http.MethodPost, "/r2/multipart/abort", nil, strings.NewReader(string(abortRaw)))
		if abortResp.StatusCode != http.StatusNoContent {
			t.Fatalf("abort status = %d, want %d (body = %q)", abortResp.StatusCode, http.StatusNoContent, abortText)
		}

		getResp, getBody := w.Get(t, "/r2/"+key)
		if getResp.StatusCode != http.StatusNotFound {
			t.Errorf("GET after abort status = %d, want %d (body = %q)", getResp.StatusCode, http.StatusNotFound, getBody)
		}

		completeBody, err := json.Marshal(map[string]any{
			"key":      key,
			"uploadId": created.UploadID,
			"parts":    []r2UploadedPartJSON{part},
		})
		if err != nil {
			t.Fatalf("marshal complete body: %v", err)
		}
		completeResp, completeText := w.Do(t, http.MethodPost, "/r2/multipart/complete", nil, strings.NewReader(string(completeBody)))
		if completeResp.StatusCode != http.StatusInternalServerError {
			t.Errorf("complete after abort status = %d, want %d (body = %q)", completeResp.StatusCode, http.StatusInternalServerError, completeText)
		}
	}
}

func r2MultipartCreate(t *testing.T, w *worker, key, contentType string, meta map[string]string) r2MultipartCreateResponse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"key":            key,
		"contentType":    contentType,
		"customMetadata": meta,
	})
	if err != nil {
		t.Fatalf("marshal create body: %v", err)
	}
	resp, body := w.Do(t, http.MethodPost, "/r2/multipart", nil, strings.NewReader(string(raw)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
	}
	var got r2MultipartCreateResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to unmarshal create response %q: %v", body, err)
	}
	return got
}

func r2MultipartUploadPart(t *testing.T, w *worker, key, uploadID string, partNumber int, payload string) r2UploadedPartJSON {
	t.Helper()
	path := "/r2/multipart?key=" + url.QueryEscape(key) +
		"&uploadId=" + url.QueryEscape(uploadID) +
		"&partNumber=" + strconv.Itoa(partNumber)
	resp, body := w.Do(t, http.MethodPut, path, nil, strings.NewReader(payload))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload part %d status = %d, want %d (body = %q)", partNumber, resp.StatusCode, http.StatusOK, body)
	}
	var got r2UploadedPartJSON
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to unmarshal part response %q: %v", body, err)
	}
	return got
}

func r2MultipartComplete(t *testing.T, w *worker, key, uploadID string, parts []r2UploadedPartJSON) r2MultipartCompleteResponse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"key":      key,
		"uploadId": uploadID,
		"parts":    parts,
	})
	if err != nil {
		t.Fatalf("marshal complete body: %v", err)
	}
	resp, body := w.Do(t, http.MethodPost, "/r2/multipart/complete", nil, strings.NewReader(string(raw)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
	}
	var got r2MultipartCompleteResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("failed to unmarshal complete response %q: %v", body, err)
	}
	return got
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
