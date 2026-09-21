package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/syumai/workers"
	"github.com/syumai/workers/cloudflare/r2"
)

// bucketName is R2 bucket name defined in wrangler.toml.
const bucketName = "BUCKET"

// Constants for multipart upload
const (
	// 5MB - minimum size for multipart upload parts (except last part)
	MinPartSize = 5 * 1024 * 1024
	// 10MB - default part size
	DefaultPartSize = 10 * 1024 * 1024
)

type server struct{}

func (s *server) bucket() (*r2.Bucket, error) {
	return r2.NewBucket(bucketName)
}

func handleErr(w http.ResponseWriter, msg string, err error) {
	log.Printf("Error: %s - %v", msg, err)
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s: %v", msg, err)
}

func handleJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// InitiateMultipartUploadResponse represents the response for initiating a multipart upload
type InitiateMultipartUploadResponse struct {
	UploadID string `json:"uploadId"`
	Key      string `json:"key"`
}

// UploadPartResponse represents the response for uploading a part
type UploadPartResponse struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

// CompleteMultipartUploadRequest represents the request to complete a multipart upload
type CompleteMultipartUploadRequest struct {
	Parts []r2.R2UploadedPart `json:"parts"`
}

// Handle POST /multipart/initiate?key=<key>
func (s *server) initiateMultipartUpload(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter is required", http.StatusBadRequest)
		return
	}

	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	// Create multipart upload with metadata
	multipartUpload, err := bucket.CreateMultipartUpload(key, &r2.R2MultipartOptions{
		HTTPMetadata: r2.HTTPMetadata{
			ContentType: "application/octet-stream",
		},
		CustomMetadata: map[string]string{
			"uploaded-via": "multipart-api",
		},
	})
	if err != nil {
		handleErr(w, "failed to create multipart upload", err)
		return
	}

	handleJSON(w, http.StatusOK, InitiateMultipartUploadResponse{
		UploadID: multipartUpload.UploadID(),
		Key:      key,
	})
}

// Handle PUT /multipart/upload?key=<key>&uploadId=<uploadId>&partNumber=<partNumber>
func (s *server) uploadPart(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("key")
	uploadID := req.URL.Query().Get("uploadId")
	partNumberStr := req.URL.Query().Get("partNumber")

	if key == "" || uploadID == "" || partNumberStr == "" {
		http.Error(w, "key, uploadId, and partNumber parameters are required", http.StatusBadRequest)
		return
	}

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 {
		http.Error(w, "invalid partNumber", http.StatusBadRequest)
		return
	}

	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	// Resume the multipart upload
	multipartUpload := bucket.ResumeMultipartUpload(key, uploadID)

	// Read the part data
	partData, err := io.ReadAll(req.Body)
	if err != nil {
		handleErr(w, "failed to read part data", err)
		return
	}
	defer req.Body.Close()

	// Upload the part
	uploadedPart, err := multipartUpload.UploadPart(partNumber, partData)
	if err != nil {
		handleErr(w, "failed to upload part", err)
		return
	}

	handleJSON(w, http.StatusOK, UploadPartResponse{
		PartNumber: uploadedPart.PartNumber,
		ETag:       uploadedPart.ETag,
	})
}

// Handle POST /multipart/complete?key=<key>&uploadId=<uploadId>
func (s *server) completeMultipartUpload(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("key")
	uploadID := req.URL.Query().Get("uploadId")

	if key == "" || uploadID == "" {
		http.Error(w, "key and uploadId parameters are required", http.StatusBadRequest)
		return
	}

	var completeReq CompleteMultipartUploadRequest
	if err := json.NewDecoder(req.Body).Decode(&completeReq); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	// Resume the multipart upload
	multipartUpload := bucket.ResumeMultipartUpload(key, uploadID)

	// Complete the multipart upload
	object, err := multipartUpload.Complete(completeReq.Parts)
	if err != nil {
		handleErr(w, "failed to complete multipart upload", err)
		return
	}

	// Return the completed object info
	handleJSON(w, http.StatusOK, map[string]interface{}{
		"key":          object.Key,
		"size":         object.Size,
		"etag":         object.ETag,
		"httpEtag":     object.HTTPETag,
		"uploaded":     object.Uploaded,
		"storageClass": object.StorageClass,
	})
}

// Handle DELETE /multipart/abort?key=<key>&uploadId=<uploadId>
func (s *server) abortMultipartUpload(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("key")
	uploadID := req.URL.Query().Get("uploadId")

	if key == "" || uploadID == "" {
		http.Error(w, "key and uploadId parameters are required", http.StatusBadRequest)
		return
	}

	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	// Resume the multipart upload
	multipartUpload := bucket.ResumeMultipartUpload(key, uploadID)

	// Abort the multipart upload
	if err := multipartUpload.Abort(); err != nil {
		handleErr(w, "failed to abort multipart upload", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Handle GET /<key> - download object
func (s *server) getObject(w http.ResponseWriter, key string) {
	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	obj, err := bucket.Get(key)
	if err != nil {
		handleErr(w, "failed to get object", err)
		return
	}
	if obj == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("object not found: %s", key)))
		return
	}

	// Set headers
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf("W/%s", obj.HTTPETag))
	contentType := "application/octet-stream"
	if obj.HTTPMetadata.ContentType != "" {
		contentType = obj.HTTPMetadata.ContentType
	}
	w.Header().Set("Content-Type", contentType)

	// Copy object body to response
	io.Copy(w, obj.Body)
}

// Handle PUT /<key> - simple upload (non-multipart)
func (s *server) putObject(w http.ResponseWriter, req *http.Request, key string) {
	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize bucket", err)
		return
	}

	_, err = bucket.Put(key, req.Body, &r2.R2PutOptions{
		HTTPMetadata: r2.HTTPMetadata{
			ContentType: req.Header.Get("Content-Type"),
		},
	})
	if err != nil {
		handleErr(w, "failed to put object", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("successfully uploaded object"))
}

func (s *server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// Handle multipart upload endpoints
	if strings.HasPrefix(path, "/multipart/") {
		switch {
		case path == "/multipart/initiate" && req.Method == "POST":
			s.initiateMultipartUpload(w, req)
			return
		case path == "/multipart/upload" && req.Method == "PUT":
			s.uploadPart(w, req)
			return
		case path == "/multipart/complete" && req.Method == "POST":
			s.completeMultipartUpload(w, req)
			return
		case path == "/multipart/abort" && req.Method == "DELETE":
			s.abortMultipartUpload(w, req)
			return
		}
	}

	// Handle regular object operations
	key := strings.TrimPrefix(path, "/")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("key is required"))
		return
	}

	switch req.Method {
	case "GET":
		s.getObject(w, key)
		return
	case "PUT":
		s.putObject(w, req, key)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("method not allowed"))
		return
	}
}

func main() {
	workers.Serve(&server{})
}
