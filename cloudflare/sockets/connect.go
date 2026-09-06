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

func Connect(ctx context.Context, addr string, opts *SocketOptions) (net.Conn, error) {
	connect, err := cfruntimecontext.GetRuntimeContextValue("connect")
	if err != nil {
		return nil, err
	}
	optionsObj := jsutil.NewObject()
	if opts != nil {
		if opts.AllowHalfOpen {
			optionsObj.Set("allowHalfOpen", true)
		}
		if opts.SecureTransport != "" {
			optionsObj.Set("secureTransport", string(opts.SecureTransport))
		}
	}
	sockVal, err := jsutil.TryCatch(js.FuncOf(func(_ js.Value, args []js.Value) any {
		return connect.Invoke(addr, optionsObj)
	}))
	if err != nil {
		return nil, err
	}
	// A zero deadline means "no timeout" (see Socket.Read/Write).
	return newSocket(ctx, sockVal, time.Time{}, time.Time{}), nil
}
