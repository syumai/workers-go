package kv

import (
	"io"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// PutOptions represents Cloudflare KV namespace put options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L958
type PutOptions struct {
	Expiration    int
	ExpirationTTL int
	// Metadata // TODO: implement
}

func (opts *PutOptions) toJS() js.Value {
	if opts == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()
	if opts.Expiration != 0 {
		obj.Set("expiration", opts.Expiration)
	}
	if opts.ExpirationTTL != 0 {
		obj.Set("expirationTtl", opts.ExpirationTTL)
	}
	return obj
}

// PutString puts string value into KV with key.
//   - if a network error happens, returns error.
func (ns *Namespace) PutString(key string, value string, opts *PutOptions) error {
	p := ns.instance.Call("put", key, value, opts.toJS())
	_, err := jsutil.AwaitPromise(p)
	if err != nil {
		return err
	}
	return nil
}

// PutReader puts stream value into KV with key.
//   - This method copies all bytes into memory for implementation restriction.
//   - if a network error happens, returns error.
func (ns *Namespace) PutReader(key string, value io.Reader, opts *PutOptions) error {
	// fetch body cannot be ReadableStream. see: https://github.com/whatwg/fetch/issues/1438
	b, err := io.ReadAll(value)
	if err != nil {
		return err
	}
	ua := jsutil.NewUint8Array(len(b))
	js.CopyBytesToJS(ua, b)
	p := ns.instance.Call("put", key, ua.Get("buffer"), opts.toJS())
	_, err = jsutil.AwaitPromise(p)
	if err != nil {
		return err
	}
	return nil
}
