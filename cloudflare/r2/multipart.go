package r2

import (
	"syscall/js"

	"github.com/syumai/workers/internal/jsutil"
)

// R2MultipartOptions represents options for creating a multipart upload.
// According to the docs, R2MultipartOptions includes:
// - httpMetadata (optional)
// - customMetadata (optional)
// - storageClass (optional)
// - ssecKey (optional)
type R2MultipartOptions struct {
	HTTPMetadata   HTTPMetadata      `json:"httpMetadata,omitempty"`
	CustomMetadata map[string]string `json:"customMetadata,omitempty"`
	StorageClass   string            `json:"storageClass,omitempty"`
	SSECKey        string            `json:"ssecKey,omitempty"`
}

func (opts *R2MultipartOptions) toJS() js.Value {
	if opts == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()
	if opts.HTTPMetadata != (HTTPMetadata{}) {
		obj.Set("httpMetadata", opts.HTTPMetadata.toJS())
	}
	if opts.CustomMetadata != nil {
		// convert map[string]string to map[string]any.
		// This makes the map convertible to JS.
		// see: https://pkg.go.dev/syscall/js#ValueOf
		customMeta := make(map[string]any, len(opts.CustomMetadata))
		for k, v := range opts.CustomMetadata {
			customMeta[k] = v
		}
		obj.Set("customMetadata", customMeta)
	}
	if opts.StorageClass != "" {
		obj.Set("storageClass", opts.StorageClass)
	}
	if opts.SSECKey != "" {
		obj.Set("ssecKey", opts.SSECKey)
	}
	return obj
}

// R2MultipartUpload represents an ongoing multipart upload.
type R2MultipartUpload struct {
	instance js.Value
}

// UploadID returns the upload ID of this multipart upload.
func (m *R2MultipartUpload) UploadID() string {
	return m.instance.Get("uploadId").String()
}

// Key returns the key of this multipart upload.
func (m *R2MultipartUpload) Key() string {
	return m.instance.Get("key").String()
}

// UploadPart uploads a part of the multipart upload.
// According to the docs: uploadPart(partNumber: number, value: ReadableStream | ArrayBuffer | ArrayBufferView | string | Blob, options?: R2MultipartOptions): Promise<R2UploadedPart>
func (m *R2MultipartUpload) UploadPart(partNumber int, data []byte, options ...*R2MultipartOptions) (*R2UploadedPart, error) {
	ua := jsutil.NewUint8Array(len(data))
	js.CopyBytesToJS(ua, data)

	var p js.Value
	if len(options) > 0 && options[0] != nil {
		// Pass options if provided
		p = m.instance.Call("uploadPart", partNumber, ua.Get("buffer"), options[0].toJS())
	} else {
		// Call without options
		p = m.instance.Call("uploadPart", partNumber, ua.Get("buffer"))
	}

	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}

	// The returned object should have partNumber and etag fields
	return &R2UploadedPart{
		PartNumber: jsutil.MaybeInt(v.Get("partNumber")),
		ETag:       jsutil.MaybeString(v.Get("etag")),
	}, nil
}

// R2UploadedPart represents a successfully uploaded part.
type R2UploadedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

// Complete completes the multipart upload with the given parts.
func (m *R2MultipartUpload) Complete(parts []R2UploadedPart) (*Object, error) {
	// Convert parts to JavaScript array
	jsArray := jsutil.NewArray(len(parts))
	for i, part := range parts {
		partObj := jsutil.NewObject()
		partObj.Set("partNumber", part.PartNumber)
		partObj.Set("etag", part.ETag)
		jsArray.SetIndex(i, partObj)
	}

	p := m.instance.Call("complete", jsArray)
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}

	return toObject(v)
}

// Abort aborts the multipart upload.
func (m *R2MultipartUpload) Abort() error {
	p := m.instance.Call("abort")
	_, err := jsutil.AwaitPromise(p)
	return err
}
