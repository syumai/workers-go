package sockets

import (
	"context"
	"net"
	"sync"
)

// Handler handles one inbound connection. The connection is closed when
// ServeConn returns.
type Handler interface {
	ServeConn(ctx context.Context, conn net.Conn)
}

// HandlerFunc is an adapter to allow the use of ordinary functions as a
// Handler.
type HandlerFunc func(ctx context.Context, conn net.Conn)

// ServeConn calls f(ctx, conn).
func (f HandlerFunc) ServeConn(ctx context.Context, conn net.Conn) {
	f(ctx, conn)
}

var (
	doneCh   = make(chan struct{})
	doneOnce sync.Once
)

// Serve registers h for inbound connections, signals readiness and blocks
// until the connection has been handled (same style as workers.Serve).
func Serve(h Handler) {
	ServeNonBlock(h)
	signalReady()
	<-Done()
}

// ServeNonBlock registers h without signalling readiness or blocking;
// combine with workers.Ready() / Done() like cron.ScheduleTaskNonBlock.
func ServeNonBlock(h Handler) {
	if h == nil {
		panic("sockets: Serve/ServeNonBlock called with a nil Handler")
	}
	lis := Listen()
	go func() {
		defer doneOnce.Do(func() { close(doneCh) })
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		h.ServeConn(lis.Context(), conn)
	}()
}

// Done returns a channel closed after the connection handled by
// Serve/ServeNonBlock has been closed.
func Done() <-chan struct{} {
	return doneCh
}

// resetServeStateForTest resets Serve/ServeNonBlock's package-level state.
// Called from Listener's resetForTest; not exported.
func resetServeStateForTest() {
	doneCh = make(chan struct{})
	doneOnce = sync.Once{}
}
