package r2

import (
	"syscall/js"
	"time"

	"github.com/syumai/workers/internal/jsutil"
)

// R2GetOptions represents options for the get operation.
type R2GetOptions struct {
	OnlyIf  *R2Conditional
	Range   *R2Range
	SSECKey string
}

// R2Conditional represents conditional headers for R2 operations.
type R2Conditional struct {
	EtagMatches      string
	EtagDoesNotMatch string
	UploadedBefore   *time.Time
	UploadedAfter    *time.Time
}

// toJS converts R2Conditional to JavaScript value.
func (c *R2Conditional) toJS() js.Value {
	if c == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()
	if c.EtagMatches != "" {
		obj.Set("etagMatches", c.EtagMatches)
	}
	if c.EtagDoesNotMatch != "" {
		obj.Set("etagDoesNotMatch", c.EtagDoesNotMatch)
	}
	if c.UploadedBefore != nil {
		obj.Set("uploadedBefore", jsutil.TimeToDate(*c.UploadedBefore))
	}
	if c.UploadedAfter != nil {
		obj.Set("uploadedAfter", jsutil.TimeToDate(*c.UploadedAfter))
	}
	return obj
}

// Update PutOptions to include all fields from documentation
type R2PutOptions struct {
	OnlyIf         *R2Conditional
	HTTPMetadata   HTTPMetadata
	CustomMetadata map[string]string
	MD5            string
	SHA1           string
	SHA256         string
	SHA384         string
	SHA512         string
	StorageClass   string // 'Standard' | 'InfrequentAccess'
	SSECKey        string
}

func (opts *R2PutOptions) toJS() js.Value {
	if opts == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()

	if opts.OnlyIf != nil {
		obj.Set("onlyIf", opts.OnlyIf.toJS())
	}

	if opts.HTTPMetadata != (HTTPMetadata{}) {
		obj.Set("httpMetadata", opts.HTTPMetadata.toJS())
	}

	if opts.CustomMetadata != nil {
		// convert map[string]string to map[string]any.
		customMeta := make(map[string]any, len(opts.CustomMetadata))
		for k, v := range opts.CustomMetadata {
			customMeta[k] = v
		}
		obj.Set("customMetadata", customMeta)
	}

	// Checksums - only one can be specified at a time
	switch {
	case opts.MD5 != "":
		obj.Set("md5", opts.MD5)
	case opts.SHA1 != "":
		obj.Set("sha1", opts.SHA1)
	case opts.SHA256 != "":
		obj.Set("sha256", opts.SHA256)
	case opts.SHA384 != "":
		obj.Set("sha384", opts.SHA384)
	case opts.SHA512 != "":
		obj.Set("sha512", opts.SHA512)
	}

	if opts.StorageClass != "" {
		obj.Set("storageClass", opts.StorageClass)
	}

	if opts.SSECKey != "" {
		obj.Set("ssecKey", opts.SSECKey)
	}

	return obj
}

// R2ListOptions represents options for listing R2 objects.
type R2ListOptions struct {
	Limit     int
	Prefix    string
	Cursor    string
	Delimiter string
	Include   []string // Can include "httpMetadata" and/or "customMetadata"
}

func (opts *R2ListOptions) toJS() js.Value {
	if opts == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()

	if opts.Limit > 0 {
		obj.Set("limit", opts.Limit)
	}

	if opts.Prefix != "" {
		obj.Set("prefix", opts.Prefix)
	}

	if opts.Cursor != "" {
		obj.Set("cursor", opts.Cursor)
	}

	if opts.Delimiter != "" {
		obj.Set("delimiter", opts.Delimiter)
	}

	if len(opts.Include) > 0 {
		jsArray := jsutil.NewArray(len(opts.Include))
		for i, inc := range opts.Include {
			jsArray.SetIndex(i, inc)
		}
		obj.Set("include", jsArray)
	}

	return obj
}

// GetWithOptions returns the result of `get` call to Bucket with options.
func (r *Bucket) GetWithOptions(key string, options *R2GetOptions) (*ObjectBody, error) {
	var p js.Value
	if options != nil {
		optObj := jsutil.NewObject()

		if options.OnlyIf != nil {
			optObj.Set("onlyIf", options.OnlyIf.toJS())
		}

		if options.Range != nil {
			rangeObj := jsutil.NewObject()
			if options.Range.Offset > 0 {
				rangeObj.Set("offset", options.Range.Offset)
			}
			if options.Range.Length > 0 {
				rangeObj.Set("length", options.Range.Length)
			}
			if options.Range.Suffix > 0 {
				rangeObj.Set("suffix", options.Range.Suffix)
			}
			optObj.Set("range", rangeObj)
		}

		if options.SSECKey != "" {
			optObj.Set("ssecKey", options.SSECKey)
		}

		p = r.instance.Call("get", key, optObj)
	} else {
		p = r.instance.Call("get", key)
	}

	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	if v.IsNull() {
		return nil, nil
	}
	return toObjectBody(v)
}

// ListWithOptions returns the result of `list` call to Bucket with options.
func (r *Bucket) ListWithOptions(options *R2ListOptions) (*Objects, error) {
	var p js.Value
	if options != nil {
		p = r.instance.Call("list", options.toJS())
	} else {
		p = r.instance.Call("list")
	}

	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return toObjects(v)
}
