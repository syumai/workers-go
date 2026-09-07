package kv

import (
	"fmt"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
)

// Namespace represents interface of Cloudflare Worker's KV namespace instance.
//   - https://developers.cloudflare.com/workers/runtime-apis/kv/
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L850
type Namespace struct {
	instance *kvjs.KVNamespace
}

// NewNamespace returns Namespace for given variable name.
//   - variable name must be defined in wrangler.toml as kv_namespace's binding.
//   - if the given variable name doesn't exist on runtime context, returns error.
//   - This function panics when a runtime context is not found.
func NewNamespace(varName string) (*Namespace, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &Namespace{instance: kvjs.KVNamespaceFromJS(inst)}, nil
}
