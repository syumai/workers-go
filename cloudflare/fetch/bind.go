package fetch

import (
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

var globalFetchFunc = js.Global().Get("fetch")

// fetch is a function that reproduces cloudflare fetch.
// Docs: https://developers.cloudflare.com/workers/runtime-apis/fetch/
func fetch(namespace js.Value, req *http.Request, init *RequestInit) (*http.Response, error) {
	// The Request object to fetch.
	// Docs: https://developers.cloudflare.com/workers/runtime-apis/request
	reqValue := jshttp.ToJSRequest(req)
	// The content of the request.
	// Docs: https://developers.cloudflare.com/workers/runtime-apis/request#requestinit
	initValue := init.ToJS()

	promise := func() js.Value {
		if namespace.IsUndefined() {
			return globalFetchFunc.Invoke(reqValue, initValue)
		}
		return namespace.Call("fetch", reqValue, initValue)
	}()

	jsRes, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return nil, err
	}

	return jshttp.ToResponse(jsRes)
}
