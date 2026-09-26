//go:build js && wasm

package ai

import (
	"fmt"
	"io"
	"syscall/js"

	"github.com/syumai/workers/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers/internal/jsutil"
)

// Namespace represents interface of Cloudflare Worker's KV namespace instance.
//   - https://developers.cloudflare.com/workers-ai/configuration/bindings/#methods
//   - https://github.com/cloudflare/workerd/blob/v1.20250421.0/types/defines/ai.d.ts#L1247
type AI struct {
	instance js.Value
}

// New returns Namespace for given variable name.
//   - variable name must be defined in wrangler.toml as `ai` binding.
//   - if the given variable name doesn't exist on runtime context, returns error.
//   - This function panics when a runtime context is not found.
func New(varName string) (*AI, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &AI{instance: inst}, nil
}

func mapToJS(opts map[string]any, type_ string) js.Value {
	obj := jsutil.NewObject()
	for k, v := range opts {
		if b, ok := v.([]byte); ok {
			ua := jsutil.NewUint8Array(len(b))
			js.CopyBytesToJS(ua, b)
			obj.Set(k, ua)
		} else {
			obj.Set(k, v)
		}
	}
	switch type_ {
	case "stream":
		obj.Set("stream", true)
	}
	return obj
}

func (ns *AI) Run(key string, opts map[string]any) (string, error) {
	p := ns.instance.Call("run", key, mapToJS(opts, ""))
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return "", err
	}
	respString := js.Global().Get("JSON").Call("stringify", v).String()
	return respString, nil
}

func (ns *AI) RunReader(key string, opts map[string]any) (io.Reader, error) {
	p := ns.instance.Call("run", key, mapToJS(opts, "stream"))
	v, err := jsutil.AwaitPromise(p)
	if err != nil {
		return nil, err
	}
	return jsutil.ConvertReadableStreamToReadCloser(v), nil
}
