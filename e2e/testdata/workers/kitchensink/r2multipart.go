//go:build js && wasm

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/syumai/workers-go/cloudflare/r2"
)

// r2MultipartCreateRequest is the JSON body of POST /r2/multipart.
type r2MultipartCreateRequest struct {
	Key            string            `json:"key"`
	ContentType    string            `json:"contentType"`
	CustomMetadata map[string]string `json:"customMetadata"`
}

// r2MultipartCreateResponse is the JSON body returned by POST /r2/multipart.
type r2MultipartCreateResponse struct {
	Key      string `json:"key"`
	UploadID string `json:"uploadId"`
}

// r2UploadedPartJSON is one part, both as the result of PUT /r2/multipart
// and as an element of the complete request.
type r2UploadedPartJSON struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

// r2MultipartCompleteRequest is the JSON body of POST /r2/multipart/complete.
type r2MultipartCompleteRequest struct {
	Key      string               `json:"key"`
	UploadID string               `json:"uploadId"`
	Parts    []r2UploadedPartJSON `json:"parts"`
}

// r2MultipartCompleteResponse mirrors the Object returned by Complete, so
// the e2e test can see that the binding decoded the finished object.
type r2MultipartCompleteResponse struct {
	Key            string            `json:"key"`
	Size           int               `json:"size"`
	ETag           string            `json:"etag"`
	ContentType    string            `json:"contentType"`
	CustomMetadata map[string]string `json:"customMetadata"`
}

// r2MultipartAbortRequest is the JSON body of POST /r2/multipart/abort.
type r2MultipartAbortRequest struct {
	Key      string `json:"key"`
	UploadID string `json:"uploadId"`
}

// handleR2Multipart dispatches the multipart routes. Each request resumes
// the upload by id (CreateMultipartUpload's handle does not survive the
// next request), which is the path ResumeMultipartUpload exists for.
func handleR2Multipart(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/r2/multipart":
		switch r.Method {
		case http.MethodPost:
			handleR2MultipartCreate(w, r)
		case http.MethodPut:
			handleR2MultipartPart(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "/r2/multipart/complete":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleR2MultipartComplete(w, r)
	case "/r2/multipart/abort":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleR2MultipartAbort(w, r)
	default:
		http.NotFound(w, r)
	}
}

func handleR2MultipartCreate(w http.ResponseWriter, r *http.Request) {
	var req r2MultipartCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	opts := &r2.MultipartOptions{
		HTTPMetadata: r2.HTTPMetadata{
			ContentType: req.ContentType,
		},
		CustomMetadata: req.CustomMetadata,
	}
	upload, err := bucket.CreateMultipartUpload(req.Key, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(r2MultipartCreateResponse{
		Key:      upload.Key,
		UploadID: upload.UploadID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleR2MultipartPart(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	uploadID := r.URL.Query().Get("uploadId")
	partNumber, err := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if key == "" || uploadID == "" || err != nil || partNumber < 1 {
		http.Error(w, "missing key, uploadId, or partNumber", http.StatusBadRequest)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body := r.Body
	if body == nil { // see the NOTE in handleEcho about nil Body.
		body = io.NopCloser(strings.NewReader(""))
	}
	upload := bucket.ResumeMultipartUpload(key, uploadID)
	part, err := upload.UploadPart(partNumber, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(r2UploadedPartJSON{
		PartNumber: part.PartNumber,
		ETag:       part.ETag,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleR2MultipartComplete(w http.ResponseWriter, r *http.Request) {
	var req r2MultipartCompleteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" || req.UploadID == "" || len(req.Parts) == 0 {
		http.Error(w, "missing key, uploadId, or parts", http.StatusBadRequest)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	parts := make([]r2.UploadedPart, len(req.Parts))
	for i, p := range req.Parts {
		parts[i] = r2.UploadedPart{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	upload := bucket.ResumeMultipartUpload(req.Key, req.UploadID)
	obj, err := upload.Complete(parts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(r2MultipartCompleteResponse{
		Key:            obj.Key,
		Size:           obj.Size,
		ETag:           obj.ETag,
		ContentType:    obj.HTTPMetadata.ContentType,
		CustomMetadata: obj.CustomMetadata,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleR2MultipartAbort(w http.ResponseWriter, r *http.Request) {
	var req r2MultipartAbortRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" || req.UploadID == "" {
		http.Error(w, "missing key or uploadId", http.StatusBadRequest)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	upload := bucket.ResumeMultipartUpload(req.Key, req.UploadID)
	if err := upload.Abort(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSONBody reads a JSON request body. A nil body (GET-style
// requests; see the NOTE in handleEcho) is reported as an error so
// callers can answer 400 instead of panicking on the decoder.
func decodeJSONBody(r *http.Request, dest any) error {
	if r.Body == nil {
		return io.EOF
	}
	return json.NewDecoder(r.Body).Decode(dest)
}
