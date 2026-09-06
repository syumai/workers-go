package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/r2"
)

// bucketName is R2 bucket name defined in wrangler.toml.
const bucketName = "BUCKET"

func handleErr(w http.ResponseWriter, msg string, err error) {
	log.Println(err)
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(msg))
}

type server struct{}

func (s *server) bucket() (*r2.Bucket, error) {
	return r2.NewBucket(bucketName)
}

func (s *server) post(w http.ResponseWriter, req *http.Request, key string) {
	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize R2Bucket\n", err)
		return
	}
	objects, err := bucket.List()
	if err != nil {
		handleErr(w, "failed to list R2Objects\n", err)
		return
	}
	for _, obj := range objects.Objects {
		if obj.Key == key {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "key %s already exists\n", key)
			return
		}
	}
	_, err = bucket.Put(key, req.Body, &r2.PutOptions{
		HTTPMetadata: r2.HTTPMetadata{
			ContentType: req.Header.Get("Content-Type"),
		},
		CustomMetadata: map[string]string{"custom-key": "custom-value"},
	})
	if err != nil {
		handleErr(w, "failed to put R2Object\n", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("successfully uploaded image"))
}

func (s *server) get(w http.ResponseWriter, key string) {
	// get image object from R2
	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize R2Bucket\n", err)
		return
	}
	imgObj, err := bucket.Get(key)
	if err != nil {
		handleErr(w, "failed to get R2Object\n", err)
		return
	}
	if imgObj == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("image not found: %s", key)))
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=14400")
	w.Header().Set("ETag", fmt.Sprintf("W/%s", imgObj.HTTPETag))
	contentType := "application/octet-stream"
	if imgObj.HTTPMetadata.ContentType != "" {
		contentType = imgObj.HTTPMetadata.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	if imgObj.Body != nil {
		defer imgObj.Body.Close()
		io.Copy(w, imgObj.Body)
	}
}

func (s *server) delete(w http.ResponseWriter, key string) {
	// delete image object from R2
	bucket, err := s.bucket()
	if err != nil {
		handleErr(w, "failed to initialize R2Bucket\n", err)
		return
	}
	if err := bucket.Delete(key); err != nil {
		handleErr(w, "failed to delete R2Object\n", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("successfully deleted image"))
}

func (s *server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	key := strings.TrimPrefix(req.URL.Path, "/")
	switch req.Method {
	case "GET":
		s.get(w, key)
		return
	case "DELETE":
		s.delete(w, key)
		return
	case "POST":
		s.post(w, req, key)
		return
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("url not found\n"))
		return
	}
}

func main() {
	workers.Serve(&server{})
}
