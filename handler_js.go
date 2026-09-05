//go:build js && wasm

package workers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

var (
	httpHandler http.Handler
	doneCh      = make(chan struct{})
	doneOnce    sync.Once
)

func init() {
	var handleRequestCallback js.Func
	handleRequestCallback = js.FuncOf(func(this js.Value, args []js.Value) any {
		reqObj := args[0]
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				if len(args) > 1 {
					reject.Invoke(jsutil.Errorf("too many args given to handleRequest: %d", len(args)))
					return
				}
				res, err := handleRequest(reqObj)
				if err != nil {
					reject.Invoke(jsutil.Error(err.Error()))
					return
				}
				resolve.Invoke(res)
			}()
			return js.Undefined()
		})
		return jsutil.NewPromise(cb)
	})
	jsutil.Binding.Set("handleRequest", handleRequestCallback)
}

type appCloser struct {
	io.ReadCloser
}

func (c *appCloser) Close() error {
	doneOnce.Do(func() { close(doneCh) })
	return c.ReadCloser.Close()
}

// handleRequest accepts a Request object and returns Response object.
func handleRequest(reqObj js.Value) (js.Value, error) {
	if httpHandler == nil {
		return js.Value{}, fmt.Errorf("Serve must be called before handleRequest.")
	}
	req, err := jshttp.ToRequest(reqObj)
	if err != nil {
		return js.Value{}, err
	}
	ctx := runtimecontext.New(context.Background(), reqObj)
	req = req.WithContext(ctx)
	reader, writer := io.Pipe()
	w := &jshttp.ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      &appCloser{reader},
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	go func() {
		defer w.Ready()
		defer writer.Close()
		httpHandler.ServeHTTP(w, req)
	}()
	<-w.ReadyCh
	return w.ToJSResponse(), nil
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

// Done returns a channel which is closed when the handler is done.
func Done() <-chan struct{} {
	return doneCh
}
