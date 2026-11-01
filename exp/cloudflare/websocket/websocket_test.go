//go:build js && wasm

package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// withLenientResponseClass swaps jsutil.ResponseClass for a fake constructor
// that just records its (body, init) arguments as JS properties on a plain
// object, restoring the original on cleanup. Node's real Response
// constructor (used by the wasm test harness) rejects status 101 outright,
// even though Cloudflare Workers' own Response class special-cases 101 for
// the WebSocket upgrade protocol.
func withLenientResponseClass(t *testing.T) {
	t.Helper()
	orig := jsutil.ResponseClass
	ctor := js.FuncOf(func(this js.Value, args []js.Value) any {
		obj := jsutil.NewObject()
		obj.Set("body", args[0])
		init := args[1]
		for _, k := range []string{"status", "statusText", "headers", "webSocket"} {
			obj.Set(k, init.Get(k))
		}
		return obj
	})
	jsutil.ResponseClass = ctor.Value
	t.Cleanup(func() {
		ctor.Release()
		jsutil.ResponseClass = orig
	})
}

// fakeWebSocketPair installs a fake global WebSocketPair constructor that
// returns a {0: client, 1: server} pair. server supports accept/send/close
// (recorded into the returned *fakeServer) and addEventListener (listeners
// are recorded, and can be invoked from tests to simulate JS-side events).
// It returns the fake client/server values plus a cleanup func that restores
// the previous WebSocketPair global.
func fakeWebSocketPair(t *testing.T) (client, server js.Value, fs *fakeServer) {
	t.Helper()
	fs = &fakeServer{listeners: map[string][]js.Value{}}

	client = js.ValueOf(map[string]any{})
	server = js.ValueOf(map[string]any{})

	server.Set("accept", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs.mu.Lock()
		fs.accepted = true
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("send", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs.mu.Lock()
		fs.sent = append(fs.sent, args[0])
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("close", js.FuncOf(func(this js.Value, args []js.Value) any {
		call := recordedClose{}
		if len(args) > 0 {
			call.hasCode = true
			call.code = args[0].Int()
		}
		if len(args) > 1 {
			call.reason = args[1].String()
		}
		fs.mu.Lock()
		fs.closed = true
		fs.closeCode = call.code
		fs.closeReason = call.reason
		fs.closes = append(fs.closes, call)
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("addEventListener", js.FuncOf(func(this js.Value, args []js.Value) any {
		name := args[0].String()
		fs.mu.Lock()
		fs.listeners[name] = append(fs.listeners[name], args[1])
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("removeEventListener", js.FuncOf(func(this js.Value, args []js.Value) any {
		name := args[0].String()
		fn := args[1]
		fs.mu.Lock()
		kept := make([]js.Value, 0, len(fs.listeners[name]))
		for _, l := range fs.listeners[name] {
			if !l.Equal(fn) {
				kept = append(kept, l)
			}
		}
		fs.listeners[name] = kept
		fs.mu.Unlock()
		return js.Undefined()
	}))

	pair := js.ValueOf(map[string]any{})
	pair.Set("0", client)
	pair.Set("1", server)

	prevWebSocketPair := js.Global().Get("WebSocketPair")
	ctor := js.FuncOf(func(this js.Value, args []js.Value) any {
		return pair
	})
	js.Global().Set("WebSocketPair", ctor)
	t.Cleanup(func() {
		ctor.Release()
		if prevWebSocketPair.IsUndefined() {
			js.Global().Delete("WebSocketPair")
		} else {
			js.Global().Set("WebSocketPair", prevWebSocketPair)
		}
	})

	return client, server, fs
}

type recordedClose struct {
	hasCode bool
	code    int
	reason  string
}

type fakeServer struct {
	mu          sync.Mutex
	accepted    bool
	sent        []js.Value
	closed      bool
	closeCode   int
	closeReason string
	closes      []recordedClose
	listeners   map[string][]js.Value
}

func (fs *fakeServer) dispatch(name string, event js.Value) {
	fs.mu.Lock()
	ls := append([]js.Value(nil), fs.listeners[name]...)
	fs.mu.Unlock()
	for _, l := range ls {
		l.Invoke(event)
	}
}

func newUpgradeRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	return r
}

func newResponseWriter() *jshttp.ResponseWriter {
	return &jshttp.ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
	}
}

func TestUpgrade_MissingHeader(t *testing.T) {
	fakeWebSocketPair(t)
	w := newResponseWriter()
	r := httptest.NewRequest(http.MethodGet, "/ws", nil) // no Upgrade header
	if _, err := Upgrade(w, r); err == nil {
		t.Fatal("Upgrade() with no Upgrade header succeeded, want error")
	}
}

func TestUpgrade_SetsWebSocketOnResponse(t *testing.T) {
	withLenientResponseClass(t)
	client, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	fs.mu.Lock()
	accepted := fs.accepted
	fs.mu.Unlock()
	if !accepted {
		t.Error("accept() was not called on the server end")
	}

	resp := w.ToJSResponse()
	if got := resp.Get("status").Int(); got != http.StatusSwitchingProtocols {
		t.Errorf("response status = %d, want %d", got, http.StatusSwitchingProtocols)
	}
	if got := resp.Get("webSocket"); !got.Equal(client) {
		t.Errorf("response webSocket = %v, want the WebSocketPair client end", got)
	}
}

func TestConn_WriteMessage(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	if err := conn.WriteMessage(TextMessage, []byte("hello")); err != nil {
		t.Fatalf("WriteMessage(TextMessage) failed: %v", err)
	}
	if err := conn.WriteMessage(BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatalf("WriteMessage(BinaryMessage) failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.sent) != 2 {
		t.Fatalf("server.send() called %d times, want 2", len(fs.sent))
	}
	if got := fs.sent[0].String(); got != "hello" {
		t.Errorf("first send() arg = %q, want %q", got, "hello")
	}
	if fs.sent[1].Get("constructor").Get("name").String() != "Uint8Array" {
		t.Errorf("second send() arg is not a Uint8Array: %v", fs.sent[1])
	}
}

func TestConn_ReadMessage_TextAndBinary(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	go fs.dispatch("message", js.ValueOf(map[string]any{"data": "hi there"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mt, data, err := conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage() failed: %v", err)
	}
	if mt != TextMessage {
		t.Errorf("message type = %v, want TextMessage", mt)
	}
	if string(data) != "hi there" {
		t.Errorf("message data = %q, want %q", data, "hi there")
	}

	buf := js.Global().Get("Uint8Array").New(3)
	buf.SetIndex(0, 9)
	buf.SetIndex(1, 8)
	buf.SetIndex(2, 7)
	arrayBuffer := buf.Get("buffer")
	go fs.dispatch("message", js.ValueOf(map[string]any{"data": arrayBuffer}))

	mt, data, err = conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage() (binary) failed: %v", err)
	}
	if mt != BinaryMessage {
		t.Errorf("message type = %v, want BinaryMessage", mt)
	}
	if len(data) != 3 || data[0] != 9 || data[1] != 8 || data[2] != 7 {
		t.Errorf("message data = %v, want [9 8 7]", data)
	}
}

func TestConn_ReadMessage_Close(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	go fs.dispatch("close", js.ValueOf(map[string]any{}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.ReadMessage(ctx); err != ErrClosed {
		t.Fatalf("ReadMessage() after close event returned err = %v, want ErrClosed", err)
	}
	// Sticky: a second call also returns ErrClosed immediately.
	if _, _, err := conn.ReadMessage(ctx); err != ErrClosed {
		t.Fatalf("second ReadMessage() returned err = %v, want ErrClosed", err)
	}
}

func TestConn_Close(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	if err := conn.Close(1000, "bye"); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if !fs.closed {
		t.Error("close() was not called on the server end")
	}
	if fs.closeCode != 1000 || fs.closeReason != "bye" {
		t.Errorf("close() called with (%d, %q), want (1000, \"bye\")", fs.closeCode, fs.closeReason)
	}
	if len(fs.closes) != 1 {
		t.Errorf("close() called %d times, want 1", len(fs.closes))
	}
}

func TestConn_Close_Idempotent(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	if err := conn.Close(1000, "bye"); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if err := conn.Close(4000, "again"); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times, want 1", len(fs.closes))
	}
	if fs.closes[0].code != 1000 || fs.closes[0].reason != "bye" {
		t.Errorf("close() called with (%d, %q), want (1000, \"bye\")", fs.closes[0].code, fs.closes[0].reason)
	}
}

func TestConn_ClientClose_Replies(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}

	// No Conn.Close yet: the close listener itself must reciprocate, which
	// is what a client close(1000) needs under wrangler dev.
	fs.dispatch("close", js.ValueOf(map[string]any{
		"code":   1000,
		"reason": "bye",
	}))

	fs.mu.Lock()
	if len(fs.closes) != 1 {
		fs.mu.Unlock()
		t.Fatalf("close() called %d times before Conn.Close, want 1", len(fs.closes))
	}
	got := fs.closes[0]
	fs.mu.Unlock()
	if !got.hasCode || got.code != 1000 || got.reason != "bye" {
		t.Fatalf("close() called with hasCode=%v (%d, %q), want (1000, \"bye\")", got.hasCode, got.code, got.reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.ReadMessage(ctx); err != ErrClosed {
		t.Fatalf("ReadMessage() after close event returned err = %v, want ErrClosed", err)
	}

	// The usual defer conn.Close(0, "") must not send a second frame or panic.
	if err := conn.Close(0, ""); err != nil {
		t.Fatalf("Close() after peer close failed: %v", err)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times after Conn.Close, want 1", len(fs.closes))
	}
}

func TestConn_ClientClose_NormalizesReservedCode(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	fs.dispatch("close", js.ValueOf(map[string]any{
		"code":   1005,
		"reason": "no status",
	}))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times, want 1", len(fs.closes))
	}
	got := fs.closes[0]
	if !got.hasCode || got.code != 1000 || got.reason != "no status" {
		t.Fatalf("close() called with hasCode=%v (%d, %q), want (1000, \"no status\")", got.hasCode, got.code, got.reason)
	}
}

func TestConn_ClientClose_EchoesApplicationCode(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	fs.dispatch("close", js.ValueOf(map[string]any{
		"code":   4000,
		"reason": "app",
	}))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times, want 1", len(fs.closes))
	}
	got := fs.closes[0]
	if !got.hasCode || got.code != 4000 || got.reason != "app" {
		t.Fatalf("close() called with hasCode=%v (%d, %q), want (4000, \"app\")", got.hasCode, got.code, got.reason)
	}
}

func TestConn_ClientClose_TruncatesReason(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	reason := strings.Repeat("x", 200)
	fs.dispatch("close", js.ValueOf(map[string]any{
		"code":   1000,
		"reason": reason,
	}))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times, want 1", len(fs.closes))
	}
	if got := fs.closes[0].reason; len(got) != 123 || got != strings.Repeat("x", 123) {
		t.Fatalf("close reason = %q (len %d), want 123 xs", got, len(got))
	}
}

func TestConn_ErrorThenCloseStillCloses(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}

	fs.dispatch("error", js.ValueOf(map[string]any{}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, readErr := conn.ReadMessage(ctx)
	if readErr == nil || readErr == ErrClosed {
		t.Fatalf("ReadMessage() after error event returned err = %v, want a non-ErrClosed error", readErr)
	}

	fs.mu.Lock()
	closedBefore := len(fs.closes)
	fs.mu.Unlock()
	if closedBefore != 0 {
		t.Fatalf("error event sent %d close frame(s), want 0", closedBefore)
	}

	if err := conn.Close(1000, "after-error"); err != nil {
		t.Fatalf("Close() after error failed: %v", err)
	}
	if err := conn.Close(1000, "after-error"); err != nil {
		t.Fatalf("second Close() after error failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.closes) != 1 {
		t.Fatalf("close() called %d times, want 1", len(fs.closes))
	}
	if fs.closes[0].code != 1000 || fs.closes[0].reason != "after-error" {
		t.Errorf("close() called with (%d, %q), want (1000, \"after-error\")", fs.closes[0].code, fs.closes[0].reason)
	}
}
