package sockets

import (
	"context"
	"net"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

type SecureTransport string

const (
	// SecureTransportOn indicates "Use TLS".
	SecureTransportOn SecureTransport = "on"
	// SecureTransportOff indicates "Do not use TLS".
	SecureTransportOff SecureTransport = "off"
	// SecureTransportStartTLS indicates "Do not use TLS initially, but allow the socket to be upgraded
	// to use TLS by calling *Socket.StartTLS()".
	SecureTransportStartTLS SecureTransport = "starttls"
)

type SocketOptions struct {
	SecureTransport SecureTransport `json:"secureTransport"`
	AllowHalfOpen   bool            `json:"allowHalfOpen"`
}

const defaultDeadline = 999999 * time.Hour

// toJS converts o to the JS options object passed as the second argument of
// connect(). A nil *SocketOptions converts to an empty object, matching the
// previous inline behavior in Connect.
func (o *SocketOptions) toJS() js.Value {
	optionsObj := jsutil.NewObject()
	if o != nil {
		if o.AllowHalfOpen {
			optionsObj.Set("allowHalfOpen", true)
		}
		if o.SecureTransport != "" {
			optionsObj.Set("secureTransport", string(o.SecureTransport))
		}
	}
	return optionsObj
}

func Connect(ctx context.Context, addr string, opts *SocketOptions) (net.Conn, error) {
	connect, err := cfruntimecontext.GetRuntimeContextValue("connect")
	if err != nil {
		return nil, err
	}
	optionsObj := opts.toJS()
	sockVal, err := jsutil.TryCatch(js.FuncOf(func(_ js.Value, args []js.Value) any {
		return connect.Invoke(addr, optionsObj)
	}))
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(defaultDeadline)
	return newSocket(ctx, sockVal, deadline, deadline), nil
}
