package cloudflare

import (
	"github.com/syumai/workers-go/cloudflare/kv"
)

// KVNamespace represents interface of Cloudflare Worker's KV namespace instance.
// Deprecated: Use kv.Namespace instead.
type KVNamespace = kv.Namespace

// NewKVNamespace returns KVNamespace for given variable name.
// Deprecated: Use kv.NewNamespace instead.
func NewKVNamespace(varName string) (*kv.Namespace, error) {
	return kv.NewNamespace(varName)
}

// KVNamespaceGetOptions represents Cloudflare KV namespace get options.
// Deprecated: Use kv.GetOptions instead.
type KVNamespaceGetOptions = kv.GetOptions

// KVNamespaceListOptions represents Cloudflare KV namespace list options.
// Deprecated: Use kv.ListOptions instead.
type KVNamespaceListOptions = kv.ListOptions

// KVNamespaceListKey represents Cloudflare KV namespace list key.
// Deprecated: Use kv.ListKey instead.
type KVNamespaceListKey = kv.ListKey

// KVNamespaceListResult represents Cloudflare KV namespace list result.
// Deprecated: Use kv.ListResult instead.
type KVNamespaceListResult = kv.ListResult

// KVNamespacePutOptions represents Cloudflare KV namespace put options.
// Deprecated: Use kv.PutOptions instead.
type KVNamespacePutOptions = kv.PutOptions
