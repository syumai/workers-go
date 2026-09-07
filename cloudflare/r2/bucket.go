package r2

import (
	"fmt"
	"io"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Bucket represents interface of Cloudflare Worker's R2 Bucket instance.
//   - https://developers.cloudflare.com/r2/runtime-apis/#bucket-method-definitions
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1006
type Bucket struct {
	instance *r2js.R2Bucket
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
	return &Bucket{instance: r2js.R2BucketFromJS(inst)}, nil
}

// Head returns the result of `head` call to Bucket.
//   - Body field of *Object is always nil for Head call.
//   - if the object for given key doesn't exist, returns nil.
//   - if a network error happens, returns error.
func (r *Bucket) Head(key string) (*Object, error) {
	obj, err := r.instance.Head(key)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, nil
	}
	return toObject(obj, nil), nil
}

// Get returns the result of `get` call to Bucket.
//   - if the object for given key doesn't exist, returns nil.
//   - if a network error happens, returns error.
func (r *Bucket) Get(key string) (*Object, error) {
	body, err := r.instance.Get(key, r2js.R2GetOptions{})
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, nil
	}
	return toObject(body, body.Body()), nil
}

// PutOptions represents Cloudflare R2 put options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1128
type PutOptions struct {
	HTTPMetadata   HTTPMetadata
	CustomMetadata map[string]string
	MD5            string
}

func (opts *PutOptions) toR2JS() r2js.R2PutOptions {
	if opts == nil {
		return r2js.R2PutOptions{}
	}
	out := r2js.R2PutOptions{
		CustomMetadata: opts.CustomMetadata,
		MD5:            opts.MD5,
	}
	if opts.HTTPMetadata != (HTTPMetadata{}) {
		meta := r2js.R2HTTPMetadata(opts.HTTPMetadata)
		out.HTTPMetadata = &meta
	}
	return out
}

// Put returns the result of `put` call to Bucket.
//   - This method copies all bytes into memory for implementation restriction.
//   - Body field of *Object is always nil for Put call.
//   - if a network error happens, returns error.
func (r *Bucket) Put(key string, value io.ReadCloser, opts *PutOptions) (*Object, error) {
	// fetch body cannot be ReadableStream. see: https://github.com/whatwg/fetch/issues/1438
	b, err := io.ReadAll(value)
	if err != nil {
		return nil, err
	}
	defer value.Close()
	ua := jsutil.BytesToJS(b)
	obj, err := r.instance.Put(key, ua.Get("buffer"), opts.toR2JS())
	if err != nil {
		return nil, err
	}
	return toObject(obj, nil), nil
}

// Delete returns the result of `delete` call to Bucket.
//   - if a network error happens, returns error.
func (r *Bucket) Delete(key string) error {
	return r.instance.Delete([]string{key})
}

// List returns the result of `list` call to Bucket.
//   - if a network error happens, returns error.
func (r *Bucket) List() (*Objects, error) {
	res, err := r.instance.List(r2js.R2ListOptions{})
	if err != nil {
		return nil, err
	}
	return toObjects(res), nil
}
