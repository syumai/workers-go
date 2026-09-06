//go:build js && wasm

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/syumai/workers-go/cloudflare/r2"
)

// customMetadataHeaderPrefix is the request/response header prefix used to
// carry R2 custom metadata. A request header "X-Meta-Color: red" round
// trips as CustomMetadata["Color"] = "red", and is mirrored back on GET as
// the response header "X-Meta-Color: red".
const customMetadataHeaderPrefix = "X-Meta-"

// contentTypeHeader carries the value to store as PutOptions.HTTPMetadata.
// ContentType. The plain "Content-Type" header is not used for this so
// that Go's http.Client can't confuse it with the request's own framing.
const contentTypeHeader = "X-Content-Type"

func handleR2Item(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/r2/")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodPut:
		handleR2Put(w, r, bucket, key)
	case http.MethodGet:
		handleR2Get(w, bucket, key)
	case http.MethodHead:
		handleR2Head(w, bucket, key)
	case http.MethodDelete:
		handleR2Delete(w, bucket, key)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleR2Put(w http.ResponseWriter, r *http.Request, bucket *r2.Bucket, key string) {
	body := r.Body
	if body == nil { // see the NOTE in handleEcho about nil Body.
		body = io.NopCloser(strings.NewReader(""))
	}
	opts := &r2.PutOptions{
		HTTPMetadata: r2.HTTPMetadata{
			ContentType: r.Header.Get(contentTypeHeader),
		},
		CustomMetadata: extractCustomMetadata(r.Header),
	}
	if _, err := bucket.Put(key, body, opts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleR2Get(w http.ResponseWriter, bucket *r2.Bucket, key string) {
	obj, err := bucket.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if obj == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeR2Metadata(w.Header(), obj)
	w.WriteHeader(http.StatusOK)
	if obj.Body != nil {
		io.Copy(w, obj.Body)
	}
}

func handleR2Head(w http.ResponseWriter, bucket *r2.Bucket, key string) {
	obj, err := bucket.Head(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if obj == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeR2Metadata(w.Header(), obj)
	w.WriteHeader(http.StatusOK)
}

func handleR2Delete(w http.ResponseWriter, bucket *r2.Bucket, key string) {
	if err := bucket.Delete(key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// extractCustomMetadata pulls "X-Meta-*" request headers into an R2
// CustomMetadata map, stripping the prefix. Returns nil (rather than an
// empty map) when there are none, so PutOptions.toJS omits the field
// entirely.
func extractCustomMetadata(h http.Header) map[string]string {
	var meta map[string]string
	for k, vs := range h {
		if !strings.HasPrefix(k, customMetadataHeaderPrefix) || len(vs) == 0 {
			continue
		}
		if meta == nil {
			meta = map[string]string{}
		}
		meta[strings.TrimPrefix(k, customMetadataHeaderPrefix)] = vs[0]
	}
	return meta
}

// writeR2Metadata mirrors an Object's HTTPMetadata.ContentType and
// CustomMetadata onto response headers, so GET/HEAD callers can observe
// them without a JSON envelope.
func writeR2Metadata(h http.Header, obj *r2.Object) {
	if obj.HTTPMetadata.ContentType != "" {
		h.Set(contentTypeHeader, obj.HTTPMetadata.ContentType)
	}
	for k, v := range obj.CustomMetadata {
		h.Set(customMetadataHeaderPrefix+k, v)
	}
}

type r2ListResponse struct {
	Keys []string `json:"keys"`
}

func handleR2List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	bucket, err := r2.NewBucket(r2BindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	objs, err := bucket.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	keys := make([]string, len(objs.Objects))
	for i, o := range objs.Objects {
		keys[i] = o.Key
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(r2ListResponse{Keys: keys}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
