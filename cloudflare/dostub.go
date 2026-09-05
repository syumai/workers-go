package cloudflare

import (
	"fmt"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// DurableObjectNamespace represents the namespace of the durable object.
type DurableObjectNamespace struct {
	instance js.Value
}

// NewDurableObjectNamespace returns the namespace for the `varName` binding.
//
// This binding must be defined in the `wrangler.toml` file. The method will
// return an `error` when there is no binding defined by `varName`.
func NewDurableObjectNamespace(varName string) (*DurableObjectNamespace, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &DurableObjectNamespace{instance: inst}, nil
}

// IdFromName returns a `DurableObjectId` for the given `name`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#deriving-ids-from-names
func (ns *DurableObjectNamespace) IdFromName(name string) *DurableObjectId {
	id := ns.instance.Call("idFromName", name)
	return &DurableObjectId{val: id}
}

// Get obtains the durable object stub for `id`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#obtaining-an-object-stub
func (ns *DurableObjectNamespace) Get(id *DurableObjectId) (*DurableObjectStub, error) {
	if id == nil || id.val.IsUndefined() {
		return nil, fmt.Errorf("invalid UniqueGlobalId")
	}
	stub := ns.instance.Call("get", id.val)
	return &DurableObjectStub{val: stub}, nil
}

// DurableObjectId represents an identifier for a durable object.
type DurableObjectId struct {
	val js.Value
}

// DurableObjectStub represents the stub to communicate with the durable object.
type DurableObjectStub struct {
	val js.Value
}

// Fetch calls the durable objects `fetch()` method.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#sending-http-requests
func (s *DurableObjectStub) Fetch(req *http.Request) (*http.Response, error) {
	jsReq := jshttp.ToJSRequest(req)

	promise := s.val.Call("fetch", jsReq)
	jsRes, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return nil, err
	}

	return jshttp.ToResponse(jsRes)
}
