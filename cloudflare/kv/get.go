package kv

import (
	"io"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// GetOptions represents Cloudflare KV namespace get options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L930
type GetOptions struct {
	CacheTTL int
}

func (opts *GetOptions) toJS(type_ string) js.Value {
	obj := jsutil.NewObject()
	obj.Set("type", type_)
	if opts == nil {
		return obj
	}
	if opts.CacheTTL != 0 {
		obj.Set("cacheTtl", opts.CacheTTL)
	}
	return obj
}

// GetString gets string value by the specified key.
//   - if a network error happens, returns error.
func (ns *Namespace) GetString(key string, opts *GetOptions) (string, error) {
	p := ns.instance.Call("get", key, opts.toJS("text"))
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// GetReader gets stream value by the specified key.
//   - if a network error happens, returns error.
func (ns *Namespace) GetReader(key string, opts *GetOptions) (io.Reader, error) {
	p := ns.instance.Call("get", key, opts.toJS("stream"))
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return jsutil.ConvertReadableStreamToReadCloser(v), nil
}
