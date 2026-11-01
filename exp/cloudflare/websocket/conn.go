//go:build js && wasm

package websocket

import (
	"context"
	"fmt"
	"sync"
	"syscall/js"
	"unicode/utf8"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// Conn is the server side of an accepted WebSocket connection, wrapping the
// "server" end of a WebSocketPair. Obtain one via Upgrade.
type Conn struct {
	server js.Value

	msgCh chan wsMessage
	done  chan struct{}

	// doneOnce closes done and publishes closeErr. It is separate from
	// closeSent: the peer's close event and an error event both finish the
	// connection, and that must not swallow the one WebSocket.close() call.
	doneOnce sync.Once
	closeErr error

	// closeMu guards closeSent.
	closeMu   sync.Mutex
	closeSent bool

	releaseOnce sync.Once
	listeners   []jsListener
}

type jsListener struct {
	event string
	fn    js.Func
}

type wsMessage struct {
	msgType MessageType
	data    []byte
}

// newConn wraps server (the "1" end of a WebSocketPair, already accepted)
// and registers its message/close/error listeners.
func newConn(server js.Value) *Conn {
	c := &Conn{
		server: server,
		msgCh:  make(chan wsMessage),
		done:   make(chan struct{}),
	}

	onMessage := js.FuncOf(func(this js.Value, args []js.Value) any {
		msg := decodeMessage(args[0])
		select {
		case c.msgCh <- msg:
		case <-c.done:
		}
		return js.Undefined()
	})
	onClose := js.FuncOf(func(this js.Value, args []js.Value) any {
		// Reciprocate before waking readers. On compatibility dates before
		// web_socket_auto_reply_to_close (2026-04-07), the runtime leaves
		// the socket half-open and the client reports 1006 about 30s later
		// unless we send a Close frame. On newer dates close() here is a
		// silent no-op because the runtime already replied.
		var event js.Value
		if len(args) > 0 {
			event = args[0]
		}
		c.replyToPeerClose(event)
		c.terminate(ErrClosed)
		return js.Undefined()
	})
	onError := js.FuncOf(func(this js.Value, args []js.Value) any {
		c.terminate(fmt.Errorf("websocket: connection closed after error"))
		return js.Undefined()
	})
	c.listeners = []jsListener{
		{event: "message", fn: onMessage},
		{event: "close", fn: onClose},
		{event: "error", fn: onError},
	}
	for _, l := range c.listeners {
		server.Call("addEventListener", l.event, l.fn)
	}

	return c
}

// decodeMessage converts a WebSocket "message" event into a wsMessage: a JS
// string payload becomes TextMessage, anything else (an ArrayBuffer, since
// binaryType defaults to "arraybuffer" on the Workers runtime) becomes
// BinaryMessage.
func decodeMessage(event js.Value) wsMessage {
	data := event.Get("data")
	if data.Type() == js.TypeString {
		return wsMessage{msgType: TextMessage, data: []byte(data.String())}
	}
	// js.CopyBytesToGo (via jsrt.BytesFromJS) requires a Uint8Array view,
	// not a bare ArrayBuffer.
	view := js.Global().Get("Uint8Array").New(data)
	return wsMessage{msgType: BinaryMessage, data: jsrt.BytesFromJS(view)}
}

// terminate records err (if this is the first termination) and wakes up any
// blocked ReadMessage call. Safe to call more than once. It does not send a
// Close frame and does not consume the one-shot close send: Conn.Close, and
// the close listener's reply, still need to be able to call WebSocket.close().
func (c *Conn) terminate(err error) {
	c.finish(err)
}

// finish publishes err to later ReadMessage calls and unblocks any current
// one. The first call wins.
func (c *Conn) finish(err error) {
	c.doneOnce.Do(func() {
		c.closeErr = err
		close(c.done)
	})
}

// replyToPeerClose sends the Close frame required to finish a client-initiated
// handshake. RFC 6455 says to echo the peer's code and reason; codes an
// endpoint must not send (and codes workerd's strict mode rejects) become a
// normal 1000 so the reply itself cannot throw and skip the handshake.
func (c *Conn) replyToPeerClose(event js.Value) {
	code, reason := peerCloseCodeReason(event)
	_ = c.sendClose(replyCloseCode(code), truncateCloseReason(reason))
}

// peerCloseCodeReason reads CloseEvent.code and CloseEvent.reason. A missing
// event or code yields 0, which replyCloseCode turns into 1000.
func peerCloseCodeReason(event js.Value) (int, string) {
	if event.IsUndefined() || event.IsNull() || event.Type() != js.TypeObject {
		return 0, ""
	}
	code := 0
	if cv := event.Get("code"); cv.Type() == js.TypeNumber {
		code = cv.Int()
	}
	reason := ""
	if rv := event.Get("reason"); rv.Type() == js.TypeString {
		reason = rv.String()
	}
	return code, reason
}

// replyCloseCode maps a peer close code onto one this side is allowed to
// send. 1000 and the application range 3000-4999 are echoed. 1004, 1005,
// 1006, and 1015 must not appear on the wire (RFC 6455), and workerd's
// strict WebSocket mode rejects every other code, so those become 1000.
func replyCloseCode(code int) int {
	if code == 1000 || (code >= 3000 && code <= 4999) {
		return code
	}
	return 1000
}

// truncateCloseReason limits a close reason to the 123-byte UTF-8 maximum
// from RFC 6455 so WebSocket.close does not throw SyntaxError.
func truncateCloseReason(reason string) string {
	if len(reason) <= 123 {
		return reason
	}
	b := []byte(reason[:123])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// sendClose calls WebSocket.close at most once. code 0 omits the arguments
// (the runtime then sends a bodyless Close, which the peer reports as 1005).
// A non-zero code is passed through with reason. Concurrent and repeated
// calls do not call close() again, so a peer-close reply followed by
// Conn.Close cannot double-close.
func (c *Conn) sendClose(code int, reason string) error {
	c.closeMu.Lock()
	if c.closeSent {
		c.closeMu.Unlock()
		return nil
	}
	c.closeSent = true
	c.closeMu.Unlock()

	if code == 0 {
		_, err := jsrt.Call(c.server, "close")
		return err
	}
	_, err := jsrt.Call(c.server, "close", code, reason)
	return err
}

// ReadMessage blocks until a message arrives, the connection is closed, ctx
// is done, or an error event is observed. Once the connection is closed,
// ReadMessage returns ErrClosed (or, for an error event, a wrapping error)
// on this and every subsequent call.
func (c *Conn) ReadMessage(ctx context.Context) (MessageType, []byte, error) {
	select {
	case msg := <-c.msgCh:
		return msg.msgType, msg.data, nil
	case <-c.done:
		return 0, nil, c.closeErr
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// WriteMessage sends a single WebSocket message: TextMessage sends data as
// a JS string, BinaryMessage sends it as a Uint8Array.
func (c *Conn) WriteMessage(t MessageType, data []byte) error {
	switch t {
	case TextMessage:
		_, err := jsrt.Call(c.server, "send", string(data))
		return err
	case BinaryMessage:
		_, err := jsrt.Call(c.server, "send", jsrt.BytesToJS(data))
		return err
	default:
		return fmt.Errorf("websocket: WriteMessage: unknown message type %v", t)
	}
}

// Close closes the connection, calling the JS WebSocket close(code, reason)
// method (or close() with no arguments when code is 0) and releasing the
// event listeners registered by Upgrade. It is safe to call more than once;
// only the first call sends a Close frame, and a frame already sent in
// reply to the peer is left as-is.
//
// A Close frame from the peer is reciprocated before ReadMessage returns
// ErrClosed. The reply echoes the peer's code and reason when that code may
// be sent (1000 or 3000-4999); otherwise it is a normal closure (1000).
// defer conn.Close(0, "") after the read loop therefore does not send a
// second frame. An error event does not send a Close frame by itself;
// Close still does.
func (c *Conn) Close(code int, reason string) error {
	err := c.sendClose(code, reason)
	c.finish(ErrClosed)
	c.release()
	return err
}

// release removes the event listeners and frees their js.Funcs once.
// removeEventListener runs first so a Close frame that arrives after a
// server-initiated Close cannot invoke a released function.
func (c *Conn) release() {
	c.releaseOnce.Do(func() {
		for _, l := range c.listeners {
			_, _ = jsrt.Call(c.server, "removeEventListener", l.event, l.fn)
			l.fn.Release()
		}
	})
}
