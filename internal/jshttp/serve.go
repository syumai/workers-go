package jshttp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/runtimecontext"
)

// bodyCloser wraps the io.Pipe reader handed to the JS-side Response body so
// that, once the JS runtime has fully consumed/closed it, onClosed (if
// non-nil) runs exactly once. This is how ServeRequest's caller learns that
// the Response body has been read to completion (or abandoned) by the JS
// side, since the handler's ServeHTTP goroutine may finish writing well
// before that happens for a streamed response.
type bodyCloser struct {
	io.ReadCloser
	onClosed func()
	once     sync.Once
}

func (c *bodyCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.onClosed != nil {
		c.once.Do(c.onClosed)
	}
	return err
}

// ServeRequest converts reqObj (a JS Request) to an *http.Request (via
// ToRequest), attaches reqObj to its context (via runtimecontext.New, so
// e.g. cloudflare/fetch.FromRequest and exp/cloudflare/cf.FromRequest can
// recover it later), runs handler against it on a new goroutine, and
// converts the result back to a JS Response (via ResponseWriter.ToJSResponse).
//
// If onBodyClosed is non-nil, it is called exactly once when the returned
// Response's body is closed (e.g. once the JS runtime has fully read a
// streamed response, or the request is otherwise torn down) — this is how a
// caller can be notified that reqObj's connection/instance is done with the
// response, without having to poll. Passing nil (as durableobjects/host.go
// does for a Durable Object's fetch handler, whose Go instance stays alive
// across multiple triggers rather than exiting when one response is done)
// simply skips that notification.
func ServeRequest(handler http.Handler, reqObj js.Value, onBodyClosed func()) (js.Value, error) {
	req, err := ToRequest(reqObj)
	if err != nil {
		// The JS runtime does not validate percent-escapes in request.url,
		// but Go's url.Parse does (e.g. "/healthz%zz"). Go's own net/http
		// server answers a request whose target fails to parse with a bare
		// "400 Bad Request", without calling the handler; do the same
		// instead of rejecting the Promise, which the runtime reports as
		// an uncaught exception and a 500. The response goes through the
		// same ResponseWriter path as any other, with http.Error as the
		// "handler".
		log.Printf("jshttp: rejecting request with unparseable URL: %v", err)
		handler = badRequestHandler
		req = &http.Request{}
	} else {
		ctx := runtimecontext.New(context.Background(), reqObj)
		req = req.WithContext(ctx)
	}

	reader, writer := io.Pipe()
	w := &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      &bodyCloser{ReadCloser: reader, onClosed: onBodyClosed},
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	go func() {
		// An unrecovered panic here would crash the whole wasm program: for
		// handler_js.go's top-level handler that kills just the one
		// request, but for durableobjects/host.go's handleFetch (which
		// passes onBodyClosed == nil because the same Go instance keeps
		// serving that Durable Object's future fetch/alarm/webSocket*
		// triggers for its whole lifetime) it would permanently take that
		// Durable Object instance down. Recovering keeps a handler bug
		// scoped to this one request.
		//
		// This replaces the plain "defer w.Ready(); defer writer.Close()"
		// pair: on the non-panic path it still runs writer.Close() before
		// w.Ready(), in that order, exactly as before.
		defer func() {
			r := recover()
			if r == nil {
				writer.Close()
				w.Ready()
				return
			}
			err := fmt.Errorf("panic: %v", r)
			log.Printf("jshttp: recovered panic in ServeHTTP: %v", r)
			select {
			case <-w.ReadyCh:
				// Headers/body already committed to the JS side (Write or
				// WriteHeader ran, or Ready() already fired some other
				// way); too late to change the status, so just tear down
				// the stream below.
			default:
				// Nothing written yet: report the panic as a fresh 500,
				// the way a handler that itself called http.Error(w, ...,
				// 500) would have.
				w.HeaderValue = http.Header{}
				w.StatusCode = http.StatusInternalServerError
			}
			// CloseWithError (rather than a clean Close) makes the JS-side
			// ReadableStream observe an error/abort instead of looking
			// like a normal, truncated-but-successful stream — see
			// jsutil.readerToReadableStream.Pull, which calls
			// controller.error(...) when the underlying Read returns a
			// non-EOF error.
			writer.CloseWithError(err)
			w.Ready()
		}()
		handler.ServeHTTP(w, req)
	}()
	<-w.ReadyCh
	return w.ToJSResponse(), nil
}

// badRequestHandler answers 400 Bad Request without consulting the request,
// like net/http's server does for a request it cannot parse.
var badRequestHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
})
