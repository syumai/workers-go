package r2

import (
	"io"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"

	"github.com/syumai/workers-go/internal/jsutil"
)

// MultipartUpload represents an in-progress multipart upload of an R2
// object.
//   - https://developers.cloudflare.com/r2/api/workers/workers-multipart-usage/
type MultipartUpload struct {
	instance *r2js.R2MultipartUpload
	Key      string
	UploadID string
}

// UploadedPart is the result of a successful UploadPart call, and an input
// to Complete.
type UploadedPart struct {
	PartNumber int
	ETag       string
}

// MultipartOptions represents options for CreateMultipartUpload.
type MultipartOptions struct {
	HTTPMetadata   HTTPMetadata
	CustomMetadata map[string]string
	StorageClass   string
}

func (opts *MultipartOptions) toR2JS() r2js.R2MultipartOptions {
	if opts == nil {
		return r2js.R2MultipartOptions{}
	}
	out := r2js.R2MultipartOptions{
		CustomMetadata: opts.CustomMetadata,
		StorageClass:   opts.StorageClass,
	}
	if opts.HTTPMetadata != (HTTPMetadata{}) {
		meta := r2js.R2HTTPMetadata(opts.HTTPMetadata)
		out.HTTPMetadata = &meta
	}
	return out
}

// toMultipartUpload converts an *r2js.R2MultipartUpload to a
// *MultipartUpload.
func toMultipartUpload(u *r2js.R2MultipartUpload) *MultipartUpload {
	return &MultipartUpload{
		instance: u,
		Key:      u.Key(),
		UploadID: u.UploadID(),
	}
}

// CreateMultipartUpload starts a new multipart upload for key.
//   - if a network error happens, returns error.
func (r *Bucket) CreateMultipartUpload(key string, opts *MultipartOptions) (*MultipartUpload, error) {
	u, err := r.instance.CreateMultipartUpload(key, opts.toR2JS())
	if err != nil {
		return nil, err
	}
	return toMultipartUpload(u), nil
}

// ResumeMultipartUpload returns a handle to an existing multipart upload,
// identified by key and uploadID.
//   - this call itself never fails: it does not validate that the upload
//     exists on the server. A mistaken key/uploadID surfaces as an error
//     from the first UploadPart, Complete, or Abort call instead.
func (r *Bucket) ResumeMultipartUpload(key, uploadID string) *MultipartUpload {
	// resumeMultipartUpload is synchronous and, per the R2 API, never
	// throws for a well-formed key/uploadID; the generated binding still
	// returns an error for symmetry with the rest of R2MultipartUpload's
	// methods; go-vet/JS-runtime-panic aside, there's nothing meaningful
	// to surface here (see the doc comment above).
	u, _ := r.instance.ResumeMultipartUpload(key, uploadID)
	return toMultipartUpload(u)
}

// UploadPart uploads part partNumber (an integer between 1 and 10,000
// inclusive) of the multipart upload, reading its content from value.
// The bytes are sent as an ArrayBuffer. workerd rejects a ReadableStream
// that does not already know its length, which is what an io.Reader
// converted with ReadableStreamFromReader produces.
//   - if a network error happens, returns error.
func (u *MultipartUpload) UploadPart(partNumber int, value io.Reader) (*UploadedPart, error) {
	if c, ok := value.(io.Closer); ok {
		defer c.Close()
	}
	b, err := io.ReadAll(value)
	if err != nil {
		return nil, err
	}
	ua := jsutil.BytesToJS(b)
	part, err := u.instance.UploadPart(float64(partNumber), ua.Get("buffer"), r2js.R2UploadPartOptions{})
	if err != nil {
		return nil, err
	}
	return &UploadedPart{
		PartNumber: part.PartNumber,
		ETag:       part.Etag,
	}, nil
}

// Complete completes the multipart upload, assembling the given parts (in
// order of PartNumber) into the final object.
//   - if a network error happens, returns error.
func (u *MultipartUpload) Complete(parts []UploadedPart) (*Object, error) {
	uploaded := make([]r2js.R2UploadedPart, len(parts))
	for i, p := range parts {
		uploaded[i] = r2js.R2UploadedPart{PartNumber: p.PartNumber, Etag: p.ETag}
	}
	obj, err := u.instance.Complete(uploaded)
	if err != nil {
		return nil, err
	}
	return toObject(obj, nil), nil
}

// Abort aborts the multipart upload.
//   - if a network error happens, returns error.
func (u *MultipartUpload) Abort() error {
	return u.instance.Abort()
}
