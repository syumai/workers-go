package jsutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"syscall/js"
)

type RawJSBodyWriter interface {
	WriteRawJSBody(body js.Value)
}

type RawJSBodyGetter interface {
	GetRawJSBody() js.Value
}

// readableStreamToReadCloser implements io.Reader sourced from ReadableStreamDefaultReader.
//   - ReadableStreamDefaultReader: https://developer.mozilla.org/en-US/docs/Web/API/ReadableStreamDefaultReader
//   - This implementation is based on: https://deno.land/std@0.139.0/streams/conversion.ts#L76
type readableStreamToReadCloser struct {
	buf          bytes.Buffer
	stream       js.Value
	streamReader *js.Value
	// ctx, when non-nil, bounds Read: once it is done, a pending or future
	// Read returns ctx.Err() and the stream is canceled.
	ctx context.Context
	// onClose, when non-nil, runs once from Close (e.g. to release an
	// AbortController's context watcher).
	onClose func()
}

var (
	_ io.ReadCloser   = (*readableStreamToReadCloser)(nil)
	_ io.WriterTo     = (*readableStreamToReadCloser)(nil)
	_ RawJSBodyGetter = (*readableStreamToReadCloser)(nil)
)

// Read reads bytes from ReadableStreamDefaultReader.
func (sr *readableStreamToReadCloser) Read(p []byte) (n int, err error) {
	if sr.streamReader == nil {
		r := sr.stream.Call("getReader")
		sr.streamReader = &r
	}
	// Keep pulling chunks until the buffer has data. Some chunks may be
	// empty (e.g. the priming chunk enqueued by readerToReadableStream.Pull
	// on its first call); an empty chunk must not be treated as EOF, which
	// is what bytes.Buffer.Read would return if we fell through with an
	// empty buffer.
	for sr.buf.Len() == 0 {
		if sr.ctx != nil {
			if err := sr.ctx.Err(); err != nil {
				sr.cancel()
				return 0, err
			}
		}
		// Buffered so the callbacks never block (leaking their js.Func)
		// when Read returned early because ctx was done.
		resultCh := make(chan js.Value, 1)
		errCh := make(chan error, 1)
		promise := sr.streamReader.Call("read")
		var then, catch js.Func
		then = js.FuncOf(func(_ js.Value, args []js.Value) any {
			defer then.Release()
			result := args[0]
			if result.Get("done").Bool() {
				errCh <- io.EOF
				return js.Undefined()
			}
			resultCh <- result.Get("value")
			return js.Undefined()
		})
		catch = js.FuncOf(func(_ js.Value, args []js.Value) any {
			defer catch.Release()
			result := args[0]
			errCh <- fmt.Errorf("JavaScript error on read: %s", errorString(result))
			return js.Undefined()
		})
		promise.Call("then", then).Call("catch", catch)
		select {
		case result := <-resultCh:
			chunk := make([]byte, result.Get("byteLength").Int())
			_ = js.CopyBytesToGo(chunk, result)
			// The length written is always the same as the length of chunk, so it can be discarded.
			//   - https://pkg.go.dev/bytes#Buffer.Write
			_, err := sr.buf.Write(chunk)
			if err != nil {
				return 0, err
			}
		case err := <-errCh:
			return 0, err
		case <-sr.ctxDone():
			sr.cancel()
			return 0, sr.ctx.Err()
		}
	}
	return sr.buf.Read(p)
}

// ctxDone returns ctx.Done(), or a nil channel (never ready) without a ctx.
func (sr *readableStreamToReadCloser) ctxDone() <-chan struct{} {
	if sr.ctx == nil {
		return nil
	}
	return sr.ctx.Done()
}

// cancel cancels the underlying stream, swallowing the returned promise's
// rejection so it does not surface as an unhandled rejection.
func (sr *readableStreamToReadCloser) cancel() {
	var target js.Value
	if sr.streamReader != nil {
		target = *sr.streamReader
	} else {
		// A locked stream cannot be canceled directly, and an unread
		// stream has no reader yet.
		target = sr.stream
	}
	ignoreRejection(target.Call("cancel"))
}

func (sr *readableStreamToReadCloser) Close() error {
	if sr.onClose != nil {
		sr.onClose()
		sr.onClose = nil
	}
	if sr.streamReader == nil {
		return nil
	}
	ignoreRejection(sr.streamReader.Call("cancel"))
	return nil
}

// ignoreRejection attaches a no-op rejection handler to p.
//
// ReadableStream(DefaultReader).cancel() returns a Promise, and that Promise
// can reject: for example when the stream is already errored (after a
// fetch is aborted, the body stream errors with an AbortError), or when the
// stream's own cancel callback throws. A rejection nobody handles is
// reported by the runtime as an unhandled rejection, which workerd logs
// (and may surface on the request) and which can terminate a Node process.
//
// It is safe to discard the error because cancel() is only called while
// tearing a body down (Close, or the context being done). By then the caller
// has already been given the error that matters (ctx.Err() or the earlier
// read error), and a failed cancel changes nothing the Go side could act on.
func ignoreRejection(p js.Value) {
	if p.IsUndefined() || p.IsNull() {
		return
	}
	var catch js.Func
	catch = js.FuncOf(func(js.Value, []js.Value) any {
		catch.Release()
		return js.Undefined()
	})
	p.Call("catch", catch)
}

// readerWrapper is wrapper to disable readableStreamToReadCloser's WriteTo method.
type readerWrapper struct {
	io.Reader
}

func (sr *readableStreamToReadCloser) WriteTo(w io.Writer) (n int64, err error) {
	if w, ok := w.(RawJSBodyWriter); ok {
		w.WriteRawJSBody(sr.stream)
		return 0, nil
	}
	return io.Copy(w, &readerWrapper{sr})
}

func (sr *readableStreamToReadCloser) GetRawJSBody() js.Value {
	return sr.stream
}

// ConvertReadableStreamToReadCloser converts ReadableStream to io.ReadCloser.
func ConvertReadableStreamToReadCloser(stream js.Value) io.ReadCloser {
	return &readableStreamToReadCloser{
		stream: stream,
	}
}

// ConvertReadableStreamToReadCloserContext is like
// ConvertReadableStreamToReadCloser, but Read stops blocking and returns
// ctx.Err() once ctx is done, canceling the underlying stream. onClose, if
// non-nil, is called once when the returned ReadCloser is closed.
func ConvertReadableStreamToReadCloserContext(ctx context.Context, stream js.Value, onClose func()) io.ReadCloser {
	return &readableStreamToReadCloser{
		stream:  stream,
		ctx:     ctx,
		onClose: onClose,
	}
}

// readerToReadableStream implements ReadableStream sourced from io.Reader.
//   - ReadableStream: https://developer.mozilla.org/docs/Web/API/ReadableStream
//   - This implementation is based on: https://deno.land/std@0.139.0/streams/conversion.ts#L230
type readerToReadableStream struct {
	initialized bool
	reader      io.Reader
	chunkBuf    []byte
}

// Pull implements ReadableStream's pull method.
//   - https://developer.mozilla.org/en-US/docs/Web/API/ReadableStream/ReadableStream#pull
func (rs *readerToReadableStream) Pull(controller js.Value) error {
	if !rs.initialized {
		ua := NewUint8Array(0)
		controller.Call("enqueue", ua)
		rs.initialized = true
		return nil
	}
	n, err := rs.reader.Read(rs.chunkBuf)
	if n != 0 {
		ua := NewUint8Array(n)
		js.CopyBytesToJS(ua, rs.chunkBuf[:n])
		controller.Call("enqueue", ua)
	}
	// Cloudflare Workers sometimes call `pull` to closed ReadableStream.
	// When the call happens, `io.ErrClosedPipe` should be ignored.
	if err == io.EOF || err == io.ErrClosedPipe {
		controller.Call("close")
		if err := rs.closeReader(); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		controller.Call("error", Error(err.Error()))
		if err := rs.closeReader(); err != nil {
			return err
		}
		return err
	}
	return nil
}

// Cancel implements ReadableStream's cancel method.
//   - https://developer.mozilla.org/en-US/docs/Web/API/ReadableStream/ReadableStream#cancel
func (rs *readerToReadableStream) Cancel() error {
	return rs.closeReader()
}

// closeReader closes the underlying reader if it implements io.Closer; a
// plain io.Reader has nothing to close.
func (rs *readerToReadableStream) closeReader() error {
	if c, ok := rs.reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// https://deno.land/std@0.139.0/streams/conversion.ts#L5
const defaultChunkSize = 16_640

// ConvertReaderToReadableStream converts an io.Reader to a ReadableStream. If
// reader also implements io.Closer (e.g. an io.ReadCloser), it is closed when
// the stream is closed or canceled.
func ConvertReaderToReadableStream(reader io.Reader) js.Value {
	stream := &readerToReadableStream{
		reader:   reader,
		chunkBuf: make([]byte, defaultChunkSize),
	}
	rsInit := NewObject()
	rsInit.Set("pull", js.FuncOf(func(_ js.Value, args []js.Value) any {
		var cb js.Func
		cb = js.FuncOf(func(this js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			controller := args[0]
			go func() {
				// A panic that escapes stream.Pull must not be allowed to
				// propagate out of this goroutine: an unrecovered panic
				// terminates the whole wasm program without ever calling
				// resolve/reject, leaving the JS side's Promise pending
				// forever instead of surfacing the failure. Converting it
				// into a rejection keeps the Promise contract intact and
				// lets the JS caller observe the error.
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(Errorf("panic in ReadableStream pull: %v", r))
					}
				}()
				err := stream.Pull(controller)
				if err != nil {
					reject.Invoke(Error(err.Error()))
					return
				}
				resolve.Invoke()
			}()
			return js.Undefined()
		})
		return NewPromise(cb)
	}))
	rsInit.Set("cancel", js.FuncOf(func(js.Value, []js.Value) any {
		var cb js.Func
		cb = js.FuncOf(func(this js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				// See the matching comment in pull's executor above: without
				// this, a panic in stream.Cancel would crash the wasm
				// program instead of rejecting the Promise.
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(Errorf("panic in ReadableStream cancel: %v", r))
					}
				}()
				err := stream.Cancel()
				if err != nil {
					reject.Invoke(Error(err.Error()))
					return
				}
				resolve.Invoke()
			}()
			return js.Undefined()
		})
		return NewPromise(cb)
	}))
	return ReadableStreamClass.New(rsInit)
}

// ConvertReaderToFixedLengthStream converts io.ReadCloser to TransformStream.
func ConvertReaderToFixedLengthStream(rc io.ReadCloser, size int64) js.Value {
	stream := MaybeFixedLengthStreamClass.New(js.ValueOf(size))
	go func(writer js.Value) {
		defer rc.Close()

		chunk := make([]byte, min(size, defaultChunkSize))
		AwaitPromise(writer.Get("ready"))
		for {
			n, err := rc.Read(chunk)
			if n > 0 {
				b := Uint8ArrayClass.New(n)
				js.CopyBytesToJS(b, chunk[:n])
				writer.Call("write", b)
			}
			if err != nil {
				AwaitPromise(writer.Get("ready"))
				writer.Call("close")
				return
			}
		}
	}(stream.Get("writable").Call("getWriter"))
	return stream.Get("readable")
}

// writableStreamToWriteCloser implements io.WriteCloser sourced from a
// WritableStreamDefaultWriter.
//   - WritableStreamDefaultWriter: https://developer.mozilla.org/docs/Web/API/WritableStreamDefaultWriter
type writableStreamToWriteCloser struct {
	writer js.Value
}

var _ io.WriteCloser = (*writableStreamToWriteCloser)(nil)

// Write copies p into a new Uint8Array and awaits the writer's write() call.
func (w *writableStreamToWriteCloser) Write(p []byte) (n int, err error) {
	ua := NewUint8Array(len(p))
	js.CopyBytesToJS(ua, p)
	if _, err := AwaitPromise(w.writer.Call("write", ua)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close awaits the writer's close() call, then releases its lock on the
// stream.
func (w *writableStreamToWriteCloser) Close() error {
	_, err := AwaitPromise(w.writer.Call("close"))
	w.writer.Call("releaseLock")
	return err
}

// ConvertWritableStreamToWriteCloser converts a JS WritableStream to an
// io.WriteCloser, via getWriter(): Write awaits write(Uint8Array), and Close
// awaits close() before releasing the writer's lock.
func ConvertWritableStreamToWriteCloser(stream js.Value) io.WriteCloser {
	return &writableStreamToWriteCloser{writer: stream.Call("getWriter")}
}
