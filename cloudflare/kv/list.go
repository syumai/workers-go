package kv

import (
	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"
)

// ListOptions represents Cloudflare KV namespace list options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L946
type ListOptions struct {
	Limit  int
	Prefix string
	Cursor string
}

func (opts *ListOptions) toKVJS() kvjs.KVNamespaceListOptions {
	if opts == nil {
		return kvjs.KVNamespaceListOptions{}
	}
	return kvjs.KVNamespaceListOptions{
		Limit:  float64(opts.Limit),
		Prefix: opts.Prefix,
		Cursor: opts.Cursor,
	}
}

// ListKey represents Cloudflare KV namespace list key.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L940
type ListKey struct {
	Name string
	// Expiration is an expiration of KV value cache. The value `0` means no expiration.
	Expiration int
	// Metadata   map[string]any // TODO: implement
}

// toListKey converts a decoded kvjs.KVNamespaceListKey to *ListKey.
func toListKey(k kvjs.KVNamespaceListKey) *ListKey {
	return &ListKey{
		Name:       k.Name,
		Expiration: int(k.Expiration),
		// Metadata // TODO: implement.
	}
}

// ListResult represents Cloudflare KV namespace list result.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L952
type ListResult struct {
	Keys         []*ListKey
	ListComplete bool
	Cursor       string
}

// List lists keys stored into the KV namespace.
func (ns *Namespace) List(opts *ListOptions) (*ListResult, error) {
	res, err := ns.instance.List(opts.toKVJS())
	if err != nil {
		return nil, err
	}
	keys := make([]*ListKey, len(res.Keys))
	for i, k := range res.Keys {
		keys[i] = toListKey(k)
	}
	return &ListResult{
		Keys:         keys,
		ListComplete: res.ListComplete,
		Cursor:       res.Cursor,
	}, nil
}
