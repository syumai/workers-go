package jshttp

import (
	"io"
	"net/http"
	"strconv"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

func toResponse(res js.Value, body io.ReadCloser) (*http.Response, error) {
	status := res.Get("status").Int()
	header := ToHeader(res.Get("headers"))
	contentLength, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)

	return &http.Response{
		Status:        strconv.Itoa(status) + " " + res.Get("statusText").String(),
		StatusCode:    status,
		Header:        header,
		Body:          body,
		ContentLength: contentLength,
	}, nil
}

// ToResponse converts JavaScript sides Response to *http.Response.
//   - Response: https://developer.mozilla.org/docs/Web/API/Response
func ToResponse(res js.Value) (*http.Response, error) {
	body := jsutil.ConvertReadableStreamToReadCloser(res.Get("body"))
	return toResponse(res, body)
}

// ToJSResponse converts *http.Response to JavaScript sides Response class object.
func ToJSResponse(res *http.Response) js.Value {
	return newJSResponse(res.StatusCode, res.Header, res.ContentLength, res.Body, nil)
}

// newResponseInit builds the ResponseInit object passed to the JavaScript
// Response constructor.
//   - ResponseInit: https://developers.cloudflare.com/workers/runtime-apis/response/
func newResponseInit(statusCode int, headers http.Header) js.Value {
	status := statusCode
	if status == 0 {
		status = http.StatusOK
	}
	respInit := jsutil.NewObject()
	respInit.Set("status", status)
	respInit.Set("statusText", http.StatusText(status))
	respInit.Set("headers", ToJSHeader(headers))
	if headers.Get("Content-Encoding") != "" {
		// If the handler already set a Content-Encoding header, the body
		// bytes are assumed to already be encoded accordingly (e.g. a
		// gzip-compressed connect-go response, or output from a gzip
		// middleware). By default (encodeBody: "automatic"), workerd
		// treats the body as uncompressed and re-encodes it per
		// Content-Encoding, which would compress it a second time.
		// Setting encodeBody to "manual" tells the runtime to send the
		// body as-is and preserve the Content-Encoding header as set.
		// This is a Cloudflare Workers specific, non-standard field of
		// ResponseInit; other runtimes (node, browsers) ignore unknown
		// fields, so it is safe to set unconditionally.
		//   - https://developers.cloudflare.com/workers/runtime-apis/response/
		respInit.Set("encodeBody", "manual")
	}
	return respInit
}

// newJSResponse creates JavaScript sides Response class object.
//   - Response: https://developer.mozilla.org/docs/Web/API/Response
func newJSResponse(statusCode int, headers http.Header, contentLength int64, body io.ReadCloser, rawBody *js.Value) js.Value {
	status := statusCode
	if status == 0 {
		status = http.StatusOK
	}
	respInit := newResponseInit(statusCode, headers)
	if status == http.StatusSwitchingProtocols ||
		status == http.StatusNoContent ||
		status == http.StatusResetContent ||
		status == http.StatusNotModified {
		return jsutil.ResponseClass.New(jsutil.Null, respInit)
	}
	readableStream := func() js.Value {
		if rawBody != nil {
			return *rawBody
		}
		if !jsutil.MaybeFixedLengthStreamClass.IsUndefined() && contentLength > 0 {
			return jsutil.ConvertReaderToFixedLengthStream(body, contentLength)
		}
		return jsutil.ConvertReaderToReadableStream(body)
	}()
	return jsutil.ResponseClass.New(readableStream, respInit)
}
