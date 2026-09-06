package sockets

import (
	"context"
	"net"
	"sync"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

// inboundConn wraps a *Socket delivered through the connect() handler. Its
// Close method additionally notifies the owning Listener and resolves the
// JS Promise returned to the runtime from handleConnect, exactly once.
type inboundConn struct {
	*Socket
	closeOnce sync.Once
	onClose   func()
}

var _ net.Conn = (*inboundConn)(nil)

// Close closes the underlying socket. Exactly once, it also runs onClose,
// which notifies the Listener that this invocation's connection is done and
// resolves the connect() handler's Promise so the runtime can end the
// invocation.
func (c *inboundConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.Socket.Close()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return err
}

func init() {
	handleConnectCallback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		sockVal := args[0]
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				if len(args) > 1 {
					reject.Invoke(jsutil.Errorf("too many args given to handleConnect: %d", len(args)))
					return
				}
				if !defaultListener.isListening() {
					// Fail fast: don't hold the connection open (or pay for
					// the Wasm instantiation) if this Worker never set up a
					// Listener/Handler for inbound sockets. This guards
					// against unexpected connect events, e.g. delivered via
					// a service binding or as an outbound Worker for
					// Workers for Platforms.
					sockVal.Call("close")
					reject.Invoke(jsutil.Error("sockets.Listen or sockets.Serve must be called before handleConnect."))
					return
				}

				ctx := runtimecontext.New(context.Background(), sockVal)
				deadline := time.Now().Add(defaultDeadline)
				sock := newSocket(ctx, sockVal, deadline, deadline)
				conn := &inboundConn{Socket: sock}
				conn.onClose = func() {
					defaultListener.connClosed()
					resolve.Invoke(js.Undefined())
				}

				if err := defaultListener.deliver(ctx, conn); err != nil {
					sockVal.Call("close")
					reject.Invoke(jsutil.Error(err.Error()))
					return
				}
				// The Promise stays pending until conn.Close() runs (see
				// inboundConn.Close), which is when the Go side is done
				// with this connection.
			}()
			return js.Undefined()
		})
		return jsutil.NewPromise(cb)
	})
	jsutil.Binding.Set("handleConnect", handleConnectCallback)
}
