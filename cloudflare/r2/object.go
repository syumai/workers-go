package r2

import (
	"errors"
	"fmt"
	"io"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

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
	// Body is a body of Object.
	// This value is nil for the result of the `Head` or `Put` method.
	Body io.Reader
}

// TODO: implement
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1106
// func (o *Object) WriteHTTPMetadata(headers http.Header) {
// }

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
	return &Object{
		instance:       v,
		Key:            v.Get("key").String(),
		Version:        v.Get("version").String(),
		Size:           v.Get("size").Int(),
		ETag:           v.Get("etag").String(),
		HTTPETag:       v.Get("httpEtag").String(),
		Uploaded:       uploaded,
		HTTPMetadata:   r2Meta,
		CustomMetadata: jsutil.StrRecordToMap(v.Get("customMetadata")),
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
