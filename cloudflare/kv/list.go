package kv

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// ListOptions represents Cloudflare KV namespace list options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L946
type ListOptions struct {
	Limit  int
	Prefix string
	Cursor string
}

func (opts *ListOptions) toJS() js.Value {
	if opts == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()
	if opts.Limit != 0 {
		obj.Set("limit", opts.Limit)
	}
	if opts.Prefix != "" {
		obj.Set("prefix", opts.Prefix)
	}
	if opts.Cursor != "" {
		obj.Set("cursor", opts.Cursor)
	}
	return obj
}

// ListKey represents Cloudflare KV namespace list key.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L940
type ListKey struct {
	Name string
	// Expiration is an expiration of KV value cache. The value `0` means no expiration.
	Expiration int
	// Metadata   map[string]any // TODO: implement
}

// toListKey converts JavaScript side's KVNamespaceListKey to *ListKey.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L940
func toListKey(v js.Value) (*ListKey, error) {
	return &ListKey{
		Name:       v.Get("name").String(),
		Expiration: jsutil.MaybeInt(v.Get("expiration")),
		// Metadata // TODO: implement. This may return an error, so this func signature has an error in return parameters.
	}, nil
}

// ListResult represents Cloudflare KV namespace list result.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L952
type ListResult struct {
	Keys         []*ListKey
	ListComplete bool
	Cursor       string
}

// toListResult converts JavaScript side's KVNamespaceListResult to *ListResult.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L952
func toListResult(v js.Value) (*ListResult, error) {
	keysVal := v.Get("keys")
	keys := make([]*ListKey, keysVal.Length())
	for i := 0; i < len(keys); i++ {
		key, err := toListKey(keysVal.Index(i))
		if err != nil {
			return nil, fmt.Errorf("error converting to ListKey: %w", err)
		}
		keys[i] = key
	}

	return &ListResult{
		Keys:         keys,
		ListComplete: v.Get("list_complete").Bool(),
		Cursor:       jsutil.MaybeString(v.Get("cursor")),
	}, nil
}

// List lists keys stored into the KV namespace.
func (ns *Namespace) List(opts *ListOptions) (*ListResult, error) {
	p := ns.instance.Call("list", opts.toJS())
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return toListResult(v)
}
