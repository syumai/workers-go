package r2

import (
	"fmt"
	"io"
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Bucket represents interface of Cloudflare Worker's R2 Bucket instance.
//   - https://developers.cloudflare.com/r2/runtime-apis/#bucket-method-definitions
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1006
type Bucket struct {
	instance js.Value
}

// NewBucket returns Bucket for given variable name.
//   - variable name must be defined in wrangler.toml.
//   - see example: https://github.com/syumai/workers-go/tree/main/_examples/r2-image-viewer
//   - if the given variable name doesn't exist on runtime context, returns error.
//   - This function panics when a runtime context is not found.
func NewBucket(varName string) (*Bucket, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &Bucket{instance: inst}, nil
}

// Head returns the result of `head` call to Bucket.
//   - Body field of *Object is always nil for Head call.
//   - if the object for given key doesn't exist, returns nil.
//   - if a network error happens, returns error.
func (r *Bucket) Head(key string) (*Object, error) {
	p := r.instance.Call("head", key)
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	if v.IsNull() {
		return nil, nil
	}
	return toObject(v)
}

// Get returns the result of `get` call to Bucket.
//   - Returns ObjectBody which includes the object's body.
//   - if the object for given key doesn't exist, returns nil.
//   - if a network error happens, returns error.
func (r *Bucket) Get(key string) (*ObjectBody, error) {
	p := r.instance.Call("get", key)
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	if v.IsNull() {
		return nil, nil
	}
	return toObjectBody(v)
}

// Put returns the result of `put` call to Bucket.
//   - This method copies all bytes into memory for implementation restriction.
//   - Body field of *Object is always nil for Put call.
//   - if a network error happens, returns error.
func (r *Bucket) Put(key string, value io.ReadCloser, opts *R2PutOptions) (*Object, error) {
	// fetch body cannot be ReadableStream. see: https://github.com/whatwg/fetch/issues/1438
	b, err := io.ReadAll(value)
	if err != nil {
		return nil, err
	}
	defer value.Close()
	ua := jsutil.NewUint8Array(len(b))
	js.CopyBytesToJS(ua, b)
	p := r.instance.Call("put", key, ua.Get("buffer"), opts.toJS())
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return toObject(v)
}

// Delete returns the result of `delete` call to Bucket.
//   - if a network error happens, returns error.
func (r *Bucket) Delete(key string) error {
	p := r.instance.Call("delete", key)
	if _, err := jsutil.AwaitPromise(p); err != nil {
		return err
	}
	return nil
}

// List returns the result of `list` call to Bucket.
//   - if a network error happens, returns error.
func (r *Bucket) List() (*Objects, error) {
	p := r.instance.Call("list")
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return toObjects(v)
}

// CreateMultipartUpload creates a multipart upload.
//   - Returns Promise which resolves to an R2MultipartUpload object representing the newly
//     created multipart upload. Once the multipart upload has been created, the multipart
//     upload can be immediately interacted with globally, either through the Workers API, or
//     through the S3 API.
//   - if a network error happens, returns error.
func (r *Bucket) CreateMultipartUpload(key string, options *R2MultipartOptions) (*R2MultipartUpload, error) {
	p := r.instance.Call("createMultipartUpload", key, options.toJS())
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return &R2MultipartUpload{instance: v}, nil
}

// ResumeMultipartUpload returns an object representing a multipart upload with the given key and uploadId.
//   - The resumeMultipartUpload operation does not perform any checks to ensure the
//     validity of the uploadId, nor does it verify the existence of a corresponding active
//     multipart upload. This is done to minimize latency before being able to call subsequent
//     operations on the R2MultipartUpload object.
func (r *Bucket) ResumeMultipartUpload(key string, uploadId string) *R2MultipartUpload {
	v := r.instance.Call("resumeMultipartUpload", key, uploadId)
	return &R2MultipartUpload{instance: v}
}
