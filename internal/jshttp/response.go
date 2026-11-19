package jshttp

import (
	"context"
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

// ToResponseContext is like ToResponse, but the response body's Read returns
// ctx.Err() (and cancels the stream) once ctx is done. onClose, if non-nil,
// is called once when the body is closed.
func ToResponseContext(ctx context.Context, res js.Value, onClose func()) (*http.Response, error) {
	body := jsutil.ConvertReadableStreamToReadCloserContext(ctx, res.Get("body"), onClose)
	return toResponse(res, body)
}

// ToJSResponse converts *http.Response to JavaScript sides Response class object.
func ToJSResponse(res *http.Response) js.Value {
	return newJSResponse(res.StatusCode, res.Header, res.ContentLength, res.Body, nil, nil)
}

// newJSResponse creates JavaScript sides Response class object.
//   - Response: https://developer.mozilla.org/docs/Web/API/Response
//
// webSocket, when non-nil, is attached as ResponseInit.webSocket and forces
// status 101 regardless of statusCode (see ResponseWriter.SetWebSocket).
func newJSResponse(statusCode int, headers http.Header, contentLength int64, body io.ReadCloser, rawBody *js.Value, webSocket *js.Value) js.Value {
	status := statusCode
	if status == 0 {
		status = http.StatusOK
	}
	if webSocket != nil {
		status = http.StatusSwitchingProtocols
	}
	respInit := jsutil.NewObject()
	respInit.Set("status", status)
	respInit.Set("statusText", http.StatusText(status))
	respInit.Set("headers", ToJSHeader(headers))
	if webSocket != nil {
		respInit.Set("webSocket", *webSocket)
	}
	if status == http.StatusSwitchingProtocols ||
		status == http.StatusNoContent ||
		status == http.StatusResetContent ||
		status == http.StatusNotModified {
		// For a WebSocket upgrade, body is intentionally left open: it wraps
		// the *bodyCloser ServeRequest hands to ResponseWriter, whose Close
		// (via onBodyClosed) signals the top-level handler's response body
		// is fully consumed so the Go program may exit (see serve.go /
		// handler_js.go). Closing it here would fire that signal
		// immediately and tear down the goroutines a WebSocket handler just
		// started (e.g. an echo loop) before they get to run. For the
		// other bodyless statuses there is no such handler still in
		// flight, so close body here — otherwise nothing else ever will,
		// and onBodyClosed (and anything blocked on it, like Serve) never
		// fires for these responses.
		if webSocket == nil && body != nil {
			body.Close()
		}
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
