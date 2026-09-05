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

// newJSResponse creates JavaScript sides Response class object.
//   - Response: https://developer.mozilla.org/docs/Web/API/Response
func newJSResponse(statusCode int, headers http.Header, contentLength int64, body io.ReadCloser, rawBody *js.Value) js.Value {
	status := statusCode
	if status == 0 {
		status = http.StatusOK
	}
	respInit := jsutil.NewObject()
	respInit.Set("status", status)
	respInit.Set("statusText", http.StatusText(status))
	respInit.Set("headers", ToJSHeader(headers))
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
