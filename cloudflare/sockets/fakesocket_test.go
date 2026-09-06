//go:build js && wasm

package sockets

import (
	"bytes"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeSocket is a JS object shaped like the Workers `Socket` class
// (readable, writable, opened, closed, close(), startTls()), built entirely
// from Go via syscall/js. It is used to exercise Connect and *Socket without
// a real Workers runtime.
type fakeSocket struct {
	val js.Value

	mu      sync.Mutex
	written [][]byte

	closeMu    sync.Mutex
	closeCount int
}

// newFakeSocket builds a fake socket whose readable stream enqueues chunks
// (in order) and then closes. openedVal is used as the `opened` property; if
// it is the zero js.Value, `opened` resolves to an empty object (no
// addresses reported).
func newFakeSocket(chunks [][]byte, openedVal js.Value) *fakeSocket {
	fs := &fakeSocket{}

	rsInit := jsutil.NewObject()
	rsInit.Set("start", js.FuncOf(func(_ js.Value, args []js.Value) any {
		controller := args[0]
		for _, c := range chunks {
			arr := jsutil.NewUint8Array(len(c))
			js.CopyBytesToJS(arr, c)
			controller.Call("enqueue", arr)
		}
		controller.Call("close")
		return js.Undefined()
	}))
	readable := jsutil.ReadableStreamClass.New(rsInit)

	wsInit := jsutil.NewObject()
	wsInit.Set("write", js.FuncOf(func(_ js.Value, args []js.Value) any {
		chunk := args[0]
		b := make([]byte, chunk.Get("byteLength").Int())
		js.CopyBytesToGo(b, chunk)
		fs.mu.Lock()
		fs.written = append(fs.written, b)
		fs.mu.Unlock()
		return js.Undefined()
	}))
	writable := js.Global().Get("WritableStream").New(wsInit)

	if openedVal.IsUndefined() {
		openedVal = jsutil.PromiseClass.Call("resolve", jsutil.NewObject())
	}
	closedVal := jsutil.PromiseClass.Call("resolve", js.Undefined())

	obj := jsutil.NewObject()
	obj.Set("readable", readable)
	obj.Set("writable", writable)
	obj.Set("opened", openedVal)
	obj.Set("closed", closedVal)
	obj.Set("close", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		fs.closeMu.Lock()
		fs.closeCount++
		fs.closeMu.Unlock()
		return jsutil.PromiseClass.Call("resolve", js.Undefined())
	}))
	obj.Set("startTls", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		return js.Undefined()
	}))
	fs.val = obj
	return fs
}

// closeCalled reports whether the fake's close() method has been invoked.
func (fs *fakeSocket) closeCalled() bool {
	fs.closeMu.Lock()
	defer fs.closeMu.Unlock()
	return fs.closeCount > 0
}

// writtenBytes returns the concatenation of every chunk written so far.
func (fs *fakeSocket) writtenBytes() []byte {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var buf bytes.Buffer
	for _, b := range fs.written {
		buf.Write(b)
	}
	return buf.Bytes()
}
