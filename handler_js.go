//go:build js && wasm

package workers

import (
	"fmt"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

var (
	httpHandler http.Handler
	doneCh      = make(chan struct{})
	allDoneCh   = make(chan struct{})
	doneOnce    sync.Once
)

func init() {
	jsutil.RegisterAsyncHandler("handleRequest", 1, func(args []js.Value) (js.Value, error) {
		return handleRequest(args[0])
	})
	go func() {
		<-doneCh
		// The program must not exit while background tasks (e.g.
		// cloudflare.WaitUntil's) are still running: resuming one from a
		// timer after exit fails under workerd with "Go program has
		// already exited" and the request is canceled as hung.
		jsutil.WaitBackgroundTasks()
		close(allDoneCh)
	}()
}

// handleRequest accepts a Request object and returns Response object.
func handleRequest(reqObj js.Value) (js.Value, error) {
	if httpHandler == nil {
		return js.Value{}, fmt.Errorf("Serve must be called before handleRequest.")
	}
	return jshttp.ServeRequest(httpHandler, reqObj, func() {
		// Null-body statuses (204/205/304) close the response body from
		// inside newJSResponse, which runs on this handler's goroutine
		// while RegisterAsyncHandler is still inside the `new Promise`
		// (syscall/js.valueNew) that delivers the Response. Closing
		// doneCh here lets main return and wasmExit delete the instance
		// before that valueNew reads this._inst.exports — the
		// "Cannot read properties of undefined (reading 'exports')"
		// failure. A macrotask runs only after that stack, and the
		// caller's promise reactions, have unwound.
		scheduleAfterTurn(func() {
			doneOnce.Do(func() { close(doneCh) })
		})
	})
}

// scheduleAfterTurn runs fn in a later JavaScript turn (setTimeout 0).
// The callback releases its own js.Func before calling fn, so fn may let
// the program exit without a further syscall touching the instance.
func scheduleAfterTurn(fn func()) {
	var cb js.Func
	cb = js.FuncOf(func(js.Value, []js.Value) any {
		cb.Release()
		fn()
		return nil
	})
	// Call would bind `this` to Go's global proxy; workerd then rejects
	// setTimeout with "Illegal invocation". Invoke calls it as a free
	// function, which is the form that runtime allows.
	js.Global().Get("setTimeout").Invoke(cb, 0)
}

// Serve serves http.Handler on a JS runtime.
// if the given handler is nil, http.DefaultServeMux will be used.
func Serve(handler http.Handler) {
	ServeNonBlock(handler)
	Ready()
	<-Done()
}

// ServeNonBlock sets the http.Handler to be served but does not signal readiness or block
// indefinitely. The non-blocking form is meant to be used in conjunction with Ready and WaitForCompletion.
func ServeNonBlock(handler http.Handler) {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	httpHandler = handler
}

//go:wasmimport workers ready
func ready()

// Ready must be called after all setups of the Go side's handlers are done.
func Ready() {
	ready()
}

// Done returns a channel which is closed when the handler is done: the
// response body has been fully consumed and all background tasks
// registered via jsutil.TrackBackgroundTask (e.g. cloudflare.WaitUntil)
// have returned.
func Done() <-chan struct{} {
	return allDoneCh
}
