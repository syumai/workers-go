package hono

import (
	"io"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

func convertBodyToJS(body io.ReadCloser) js.Value {
	if sr, ok := body.(jsutil.RawJSBodyGetter); ok {
		return sr.GetRawJSBody()
	}
	return jsutil.ConvertReaderToReadableStream(body)
}

func NewJSResponse(body io.ReadCloser, statusCode int, headers http.Header) js.Value {
	bodyObj := convertBodyToJS(body)
	opts := jsutil.ObjectClass.New()
	if statusCode != 0 {
		opts.Set("status", statusCode)
	}
	if headers != nil {
		headersObj := jshttp.ToJSHeader(headers)
		opts.Set("headers", headersObj)
	}
	return jsutil.ResponseClass.New(bodyObj, opts)
}

func NewJSResponseWithBase(body io.ReadCloser, baseRespObj js.Value) js.Value {
	bodyObj := convertBodyToJS(body)
	return jsutil.ResponseClass.New(bodyObj, baseRespObj)
}
