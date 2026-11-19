package fetch

import (
	"context"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

var globalFetchFunc = js.Global().Get("fetch")

// fetch is a function that reproduces cloudflare fetch.
// Docs: https://developers.cloudflare.com/workers/runtime-apis/fetch/
//
// The request's context (req.Context()) bounds the whole exchange: when it is
// done, the JavaScript fetch is aborted through an AbortSignal, a pending
// wait for response headers returns ctx.Err(), and reading the response body
// returns ctx.Err(). Without this, a server that never responds would pin
// the calling goroutine forever.
func fetch(namespace js.Value, req *http.Request, init *RequestInit) (*http.Response, error) {
	ctx := req.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The Request object to fetch.
	// Docs: https://developers.cloudflare.com/workers/runtime-apis/request
	reqValue := jshttp.ToJSRequest(req)

	// Link ctx to an AbortController whose signal is passed to fetch(). A
	// context that can never be canceled (Done() == nil) needs no signal.
	// context.AfterFunc runs abort only if ctx is done, without parking a
	// goroutine per request; stop must be called once the exchange is over
	// (body closed or fetch failed) so the registration does not outlive it.
	release := func() {}
	if ctx.Done() != nil {
		controller := jsutil.AbortControllerClass.New()
		withSignal := RequestInit{}
		if init != nil {
			withSignal = *init
		}
		withSignal.signal = controller.Get("signal")
		init = &withSignal
		stop := context.AfterFunc(ctx, func() { controller.Call("abort") })
		release = func() { stop() }
	}

	// The content of the request.
	// Docs: https://developers.cloudflare.com/workers/runtime-apis/request#requestinit
	initValue := init.ToJS()

	promise := func() js.Value {
		if namespace.IsUndefined() {
			return globalFetchFunc.Invoke(reqValue, initValue)
		}
		return namespace.Call("fetch", reqValue, initValue)
	}()

	jsRes, err := jsutil.AwaitPromiseContext(ctx, promise)
	if err != nil {
		release()
		return nil, err
	}

	res, err := jshttp.ToResponseContext(ctx, jsRes, release)
	if err != nil {
		release()
		return nil, err
	}
	return res, nil
}
