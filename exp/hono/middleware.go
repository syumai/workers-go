package hono

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

type Middleware func(c *Context, next func())

var middleware Middleware

func ChainMiddlewares(middlewares ...Middleware) Middleware {
	if len(middlewares) == 0 {
		return nil
	}
	if len(middlewares) == 1 {
		return middlewares[0]
	}
	return func(c *Context, next func()) {
		for i := len(middlewares) - 1; i > 0; i-- {
			i := i
			f := next
			next = func() {
				middlewares[i](c, f)
			}
		}
		middlewares[0](c, next)
	}
}

func init() {
	runHonoMiddlewareCallback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 1 {
			panic(fmt.Errorf("too many args given to handleRequest: %d", len(args)))
		}
		nextFnObj := args[0]
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			go func() {
				err := runHonoMiddleware(nextFnObj)
				if err != nil {
					panic(err)
				}
				resolve.Invoke(js.Undefined())
			}()
			return js.Undefined()
		})
		return jsutil.NewPromise(cb)
	})
	jsutil.Binding.Set("runHonoMiddleware", runHonoMiddlewareCallback)
}

func runHonoMiddleware(nextFnObj js.Value) error {
	if middleware == nil {
		return fmt.Errorf("ServeMiddleware must be called before runHonoMiddleware.")
	}
	c := newContext(jsutil.RuntimeContext.Get("ctx"))
	next := func() {
		jsutil.AwaitPromise(nextFnObj.Invoke())
	}
	middleware(c, next)
	return nil
}

//go:wasmimport workers ready
func ready()

// ServeMiddleware sets the Task to be executed
func ServeMiddleware(middleware_ Middleware) {
	middleware = middleware_
	ready()
	select {}
}
