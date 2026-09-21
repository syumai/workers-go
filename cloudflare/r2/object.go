package r2

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// R2Range represents the range of bytes returned for a request.
type R2Range struct {
	Offset int
	Length int
	Suffix int
}

// R2Checksums represents checksums for an R2 object.
type R2Checksums struct {
	MD5    []byte
	SHA1   []byte
	SHA256 []byte
	SHA384 []byte
	SHA512 []byte
}

// Object represents Cloudflare R2 object.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1094
type Object struct {
	instance       js.Value
	Key            string
	Version        string
	Size           int
	ETag           string
	HTTPETag       string
	Uploaded       time.Time
	HTTPMetadata   HTTPMetadata
	CustomMetadata map[string]string
	Range          *R2Range
	Checksums      *R2Checksums
	StorageClass   string // 'Standard' | 'InfrequentAccess'
	SSECKeyMD5     string
	// Body is a body of Object.
	// This value is nil for the result of the `Head` or `Put` method.
	Body io.Reader
}

// WriteHTTPMetadata writes the HTTP metadata from the object to the given headers.
func (o *Object) WriteHTTPMetadata(headers http.Header) {
	if o.HTTPMetadata.ContentType != "" {
		headers.Set("Content-Type", o.HTTPMetadata.ContentType)
	}
	if o.HTTPMetadata.ContentLanguage != "" {
		headers.Set("Content-Language", o.HTTPMetadata.ContentLanguage)
	}
	if o.HTTPMetadata.ContentDisposition != "" {
		headers.Set("Content-Disposition", o.HTTPMetadata.ContentDisposition)
	}
	if o.HTTPMetadata.ContentEncoding != "" {
		headers.Set("Content-Encoding", o.HTTPMetadata.ContentEncoding)
	}
	if o.HTTPMetadata.CacheControl != "" {
		headers.Set("Cache-Control", o.HTTPMetadata.CacheControl)
	}
	if !o.HTTPMetadata.CacheExpiry.IsZero() {
		headers.Set("Expires", o.HTTPMetadata.CacheExpiry.Format(http.TimeFormat))
	}
}

func (o *Object) BodyUsed() (bool, error) {
	v := o.instance.Get("bodyUsed")
	if v.IsUndefined() {
		return false, errors.New("bodyUsed doesn't exist for this Object")
	}
	return v.Bool(), nil
}

// toObject converts JavaScript side's Object to *Object.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1094
func toObject(v js.Value) (*Object, error) {
	if v.IsUndefined() || v.IsNull() {
		return nil, fmt.Errorf("object is undefined or null")
	}
	uploaded, err := jsutil.DateToTime(v.Get("uploaded"))
	if err != nil {
		return nil, fmt.Errorf("error converting uploaded: %w", err)
	}
	r2Meta, err := toHTTPMetadata(v.Get("httpMetadata"))
	if err != nil {
		return nil, fmt.Errorf("error converting httpMetadata: %w", err)
	}
	bodyVal := v.Get("body")
	var body io.Reader
	if !bodyVal.IsUndefined() {
		body = jsutil.ConvertReadableStreamToReadCloser(v.Get("body"))
	}

	// Parse range if present
	var r2Range *R2Range
	rangeVal := v.Get("range")
	if !rangeVal.IsUndefined() && !rangeVal.IsNull() {
		r2Range = &R2Range{
			Offset: jsutil.MaybeInt(rangeVal.Get("offset")),
			Length: jsutil.MaybeInt(rangeVal.Get("length")),
			Suffix: jsutil.MaybeInt(rangeVal.Get("suffix")),
		}
	}

	// Parse checksums if present
	var checksums *R2Checksums
	checksumsVal := v.Get("checksums")
	if !checksumsVal.IsUndefined() && !checksumsVal.IsNull() {
		checksums = &R2Checksums{}
		if md5 := checksumsVal.Get("md5"); !md5.IsUndefined() {
			checksums.MD5 = jsutil.ArrayBufferToBytes(md5)
		}
		if sha1 := checksumsVal.Get("sha1"); !sha1.IsUndefined() {
			checksums.SHA1 = jsutil.ArrayBufferToBytes(sha1)
		}
		if sha256 := checksumsVal.Get("sha256"); !sha256.IsUndefined() {
			checksums.SHA256 = jsutil.ArrayBufferToBytes(sha256)
		}
		if sha384 := checksumsVal.Get("sha384"); !sha384.IsUndefined() {
			checksums.SHA384 = jsutil.ArrayBufferToBytes(sha384)
		}
		if sha512 := checksumsVal.Get("sha512"); !sha512.IsUndefined() {
			checksums.SHA512 = jsutil.ArrayBufferToBytes(sha512)
		}
	}

	return &Object{
		instance:       v,
		Key:            jsutil.MaybeString(v.Get("key")),
		Version:        jsutil.MaybeString(v.Get("version")),
		Size:           jsutil.MaybeInt(v.Get("size")),
		ETag:           jsutil.MaybeString(v.Get("etag")),
		HTTPETag:       jsutil.MaybeString(v.Get("httpEtag")),
		Uploaded:       uploaded,
		HTTPMetadata:   r2Meta,
		CustomMetadata: jsutil.StrRecordToMap(v.Get("customMetadata")),
		Range:          r2Range,
		Checksums:      checksums,
		StorageClass:   jsutil.MaybeString(v.Get("storageClass")),
		SSECKeyMD5:     jsutil.MaybeString(v.Get("ssecKeyMd5")),
		Body:           body,
	}, nil
}

// HTTPMetadata represents metadata of Object.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1053
type HTTPMetadata struct {
	ContentType        string
	ContentLanguage    string
	ContentDisposition string
	ContentEncoding    string
	CacheControl       string
	CacheExpiry        time.Time
}

func toHTTPMetadata(v js.Value) (HTTPMetadata, error) {
	if v.IsUndefined() || v.IsNull() {
		return HTTPMetadata{}, nil
	}
	cacheExpiry, err := jsutil.MaybeDate(v.Get("cacheExpiry"))
	if err != nil {
		return HTTPMetadata{}, fmt.Errorf("error converting cacheExpiry: %w", err)
	}
	return HTTPMetadata{
		ContentType:        jsutil.MaybeString(v.Get("contentType")),
		ContentLanguage:    jsutil.MaybeString(v.Get("contentLanguage")),
		ContentDisposition: jsutil.MaybeString(v.Get("contentDisposition")),
		ContentEncoding:    jsutil.MaybeString(v.Get("contentEncoding")),
		CacheControl:       jsutil.MaybeString(v.Get("cacheControl")),
		CacheExpiry:        cacheExpiry,
	}, nil
}

func (md *HTTPMetadata) toJS() js.Value {
	obj := jsutil.NewObject()
	kv := map[string]string{
		"contentType":        md.ContentType,
		"contentLanguage":    md.ContentLanguage,
		"contentDisposition": md.ContentDisposition,
		"contentEncoding":    md.ContentEncoding,
		"cacheControl":       md.CacheControl,
	}
	for k, v := range kv {
		if v != "" {
			obj.Set(k, v)
		}
	}
	if !md.CacheExpiry.IsZero() {
		obj.Set("cacheExpiry", jsutil.TimeToDate(md.CacheExpiry))
	}
	return obj
}
