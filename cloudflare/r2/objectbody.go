package r2

import (
	"encoding/json"
	"io"
	"syscall/js"

	"github.com/syumai/workers/internal/jsutil"
)

// ObjectBody represents an object's metadata combined with its body.
// It is returned when you GET an object from an R2 bucket.
// ObjectBody extends Object with additional methods for reading the body.
type ObjectBody struct {
	*Object
}

// ArrayBuffer returns the body as an ArrayBuffer.
func (o *ObjectBody) ArrayBuffer() ([]byte, error) {
	if o.instance.IsUndefined() {
		return nil, ErrObjectBodyNotAvailable
	}

	p := o.instance.Call("arrayBuffer")
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}

	return jsutil.ArrayBufferToBytes(v), nil
}

// Text returns the body as a string.
func (o *ObjectBody) Text() (string, error) {
	if o.instance.IsUndefined() {
		return "", ErrObjectBodyNotAvailable
	}

	p := o.instance.Call("text")
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return "", err
	}

	return v.String(), nil
}

// JSON decodes the body as JSON into the provided value.
func (o *ObjectBody) JSON(v any) error {
	text, err := o.Text()
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(text), v)
}

// Blob returns the body as a Blob (JavaScript value).
func (o *ObjectBody) Blob() (js.Value, error) {
	if o.instance.IsUndefined() {
		return js.Undefined(), ErrObjectBodyNotAvailable
	}

	p := o.instance.Call("blob")
	return jsutil.AwaitPromise(p)
}

// toObjectBody converts a JavaScript value to an ObjectBody.
func toObjectBody(v js.Value) (*ObjectBody, error) {
	obj, err := toObject(v)
	if err != nil {
		return nil, err
	}

	return &ObjectBody{Object: obj}, nil
}

// ErrObjectBodyNotAvailable is returned when trying to access body methods on an object without a body.
var ErrObjectBodyNotAvailable = io.EOF
