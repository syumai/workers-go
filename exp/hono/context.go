package hono

import (
	"context"
	"io"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

type Context struct {
	ctxObj  js.Value
	reqFunc func() *http.Request
}

func newContext(ctxObj js.Value) *Context {
	return &Context{
		ctxObj: ctxObj,
		reqFunc: sync.OnceValue(func() *http.Request {
			reqObj := ctxObj.Get("req").Get("raw")
			req, err := jshttp.ToRequest(reqObj)
			if err != nil {
				panic(err)
			}
			ctx := runtimecontext.New(context.Background(), reqObj)
			req = req.WithContext(ctx)
			return req
		}),
	}
}

func (c *Context) Request() *http.Request {
	return c.reqFunc()
}

func (c *Context) SetHeader(key, value string) {
	c.ctxObj.Call("header", key, value)
}

func (c *Context) SetStatus(statusCode int) {
	c.ctxObj.Call("status", statusCode)
}

func (c *Context) RawResponse() js.Value {
	return c.ctxObj.Get("res")
}

func (c *Context) ResponseBody() io.ReadCloser {
	return jsutil.ConvertReadableStreamToReadCloser(c.ctxObj.Get("res").Get("body"))
}

func (c *Context) SetBody(body io.ReadCloser) {
	bodyObj := convertBodyToJS(body)
	respObj := c.ctxObj.Call("body", bodyObj)
	c.ctxObj.Set("res", respObj)
}

func (c *Context) SetResponse(respObj js.Value) {
	c.ctxObj.Set("res", respObj)
}
