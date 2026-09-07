package r2

import (
	"errors"
	"io"
	"syscall/js"
	"time"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"
)

// Object represents Cloudflare R2 object.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1094
type Object struct {
	instance       r2ObjectLike
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
	// The caller is responsible for closing the Body when it is not nil.
	Body io.ReadCloser
}

// r2ObjectLike is the subset of *r2js.R2Object's and *r2js.R2ObjectBody's
// getters used to populate an *Object; R2ObjectBody's handle-extends
// relationship to R2Object means both types implement it, so toObject
// below can build an *Object from either a Head/Put result (R2Object) or a
// Get result (R2ObjectBody).
type r2ObjectLike interface {
	JSValue() js.Value
	Key() string
	Version() string
	Size() int
	Etag() string
	HTTPEtag() string
	Uploaded() time.Time
	HTTPMetadata() r2js.R2HTTPMetadata
	CustomMetadata() map[string]string
}

// TODO: implement
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L1106
// func (o *Object) WriteHTTPMetadata(headers http.Header) {
// }

func (o *Object) BodyUsed() (bool, error) {
	v := o.instance.JSValue().Get("bodyUsed")
	if v.IsUndefined() {
		return false, errors.New("bodyUsed doesn't exist for this Object")
	}
	return v.Bool(), nil
}

// toObject converts o (an *r2js.R2Object, from Head/Put, or an
// *r2js.R2ObjectBody, from Get) to an *Object. body is the object's stream
// body, if any; callers pass nil for Head/Put results, which never have one.
func toObject(o r2ObjectLike, body io.ReadCloser) *Object {
	return &Object{
		instance:       o,
		Key:            o.Key(),
		Version:        o.Version(),
		Size:           o.Size(),
		ETag:           o.Etag(),
		HTTPETag:       o.HTTPEtag(),
		Uploaded:       o.Uploaded(),
		HTTPMetadata:   HTTPMetadata(o.HTTPMetadata()),
		CustomMetadata: o.CustomMetadata(),
		Body:           body,
	}
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
