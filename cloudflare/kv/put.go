package kv

import (
	"io"
	"syscall/js"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"

	"github.com/syumai/workers-go/internal/jsutil"
)

// PutOptions represents Cloudflare KV namespace put options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L958
type PutOptions struct {
	Expiration    int
	ExpirationTTL int
	// Metadata // TODO: implement
}

func (opts *PutOptions) toKVJS() kvjs.KVNamespacePutOptions {
	if opts == nil {
		return kvjs.KVNamespacePutOptions{}
	}
	return kvjs.KVNamespacePutOptions{
		Expiration:    opts.Expiration,
		ExpirationTTL: opts.ExpirationTTL,
	}
}

// PutString puts string value into KV with key.
//   - if a network error happens, returns error.
func (ns *Namespace) PutString(key string, value string, opts *PutOptions) error {
	return ns.instance.Put(key, js.ValueOf(value), opts.toKVJS())
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
	ua := jsutil.BytesToJS(b)
	return ns.instance.Put(key, ua.Get("buffer"), opts.toKVJS())
}
