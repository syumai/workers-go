//go:build js && wasm

// Package websocket implements the server side of the Cloudflare Workers
// WebSocket upgrade protocol
// (https://developers.cloudflare.com/workers/runtime-apis/websockets/).
//
// Unlike the rest of exp/cloudflare, this package is entirely hand-written:
// WebSocketPair/WebSocket are EventTarget-based JS APIs (addEventListener
// callbacks, not Promise-returning methods), which don't fit cfgen's
// method/property-to-Go-method mapping. It lives under exp/cloudflare
// because it wraps a runtime API that has no Go binding elsewhere yet, not
// because any of it is generated.
//
// # Upgrade must return promptly
//
// Cloudflare only returns the 101 Switching Protocols response once the
// handler that called Upgrade returns; workers-go's own request/response
// plumbing (handler_js.go) waits for the http.Handler's ServeHTTP call to
// finish before converting the ResponseWriter to a JS Response. So a
// handler must call Upgrade, start a goroutine to run its read/write loop
// against the returned *Conn, and return immediately — it must NOT read or
// write the connection inline before returning, or the 101 response (and
// therefore the whole WebSocket handshake) will never be sent:
//
//	func handleWS(w http.ResponseWriter, r *http.Request) {
//		conn, err := websocket.Upgrade(w, r)
//		if err != nil {
//			http.Error(w, err.Error(), http.StatusBadRequest)
//			return
//		}
//		go func() {
//			defer conn.Close(0, "")
//			for {
//				mt, data, err := conn.ReadMessage(context.Background())
//				if err != nil {
//					return
//				}
//				if err := conn.WriteMessage(mt, data); err != nil {
//					return
//				}
//			}
//		}()
//		// return immediately: the goroutine above keeps the Worker alive
//		// via its own event-loop-driven I/O, cloudflare.WaitUntil is not
//		// needed for it.
//	}
//
// A Close frame from the client is reciprocated by the close listener
// (the peer's code and reason when that code may be sent, otherwise
// 1000). The deferred Close releases listeners and still sends a Close
// frame when the connection ends because of an error rather than a peer
// Close. Calling Close more than once does not send a second frame.
package websocket

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// MessageType identifies whether a WebSocket message is text or binary, as
// passed to WriteMessage and returned from ReadMessage.
type MessageType int

const (
	// TextMessage denotes a UTF-8-encoded text message (JS string data).
	TextMessage MessageType = iota + 1
	// BinaryMessage denotes a binary message (JS ArrayBuffer data).
	BinaryMessage
)

func (t MessageType) String() string {
	switch t {
	case TextMessage:
		return "TextMessage"
	case BinaryMessage:
		return "BinaryMessage"
	default:
		return fmt.Sprintf("MessageType(%d)", int(t))
	}
}

// ErrClosed is returned by ReadMessage once the connection's close event
// has been observed (or Close has been called), and by any ReadMessage
// call made afterwards.
var ErrClosed = errors.New("websocket: connection closed")

// webSocketSetter is implemented by *jshttp.ResponseWriter (via
// jshttp.ResponseWriter.SetWebSocket). Upgrade type-asserts against this
// interface instead of importing internal/jshttp's concrete type directly,
// so that an http.ResponseWriter wrapping a *jshttp.ResponseWriter (e.g. a
// middleware's own wrapper type) still works, as long as it forwards
// SetWebSocket.
type webSocketSetter interface {
	SetWebSocket(js.Value)
}

// Upgrade upgrades an incoming HTTP request to a WebSocket connection.
//
// It validates that r has an "Upgrade: websocket" header, creates a
// WebSocketPair, accepts the server end, and attaches the client end to w
// via SetWebSocket so that the eventual Response carries status 101 and
// ResponseInit.webSocket, then returns a *Conn wrapping the server end.
//
// w must be a *jshttp.ResponseWriter, or another type that forwards
// SetWebSocket to one (see webSocketSetter); this is always true for the
// http.ResponseWriter passed to an http.Handler registered with
// workers.Serve. Upgrade returns an error if w does not support this, if
// the Upgrade header is missing/wrong, or if the runtime has no
// WebSocketPair global (i.e. this isn't running in Cloudflare Workers).
//
// See the package doc for the constraint that the caller must not block on
// the returned *Conn before returning from its handler.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if got := r.Header.Get("Upgrade"); !strings.EqualFold(got, "websocket") {
		return nil, fmt.Errorf("websocket: Upgrade: expected request header Upgrade: websocket, got %q", got)
	}
	setter, ok := w.(webSocketSetter)
	if !ok {
		return nil, fmt.Errorf("websocket: Upgrade: %T does not support attaching a WebSocket to its response", w)
	}
	pairClass := js.Global().Get("WebSocketPair")
	if pairClass.IsUndefined() {
		return nil, errors.New("websocket: Upgrade: WebSocketPair is not available in this runtime")
	}
	pair := pairClass.New()
	client := pair.Get("0")
	server := pair.Get("1")

	if _, err := jsrt.Call(server, "accept"); err != nil {
		return nil, fmt.Errorf("websocket: Upgrade: accept: %w", err)
	}

	conn := newConn(server)
	setter.SetWebSocket(client)
	return conn, nil
}
