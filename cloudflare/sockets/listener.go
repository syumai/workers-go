package sockets

import (
	"context"
	"errors"
	"net"
	"sync"

	workers "github.com/syumai/workers-go"
)

// errAlreadyDelivered is returned by deliver when a connection was already
// delivered to this instance; a connect() invocation only ever carries one
// connection.
var errAlreadyDelivered = errors.New("sockets: a connection was already delivered to this instance")

// Listener provides the inbound TCP socket delivered to the Worker's
// connect() handler as a net.Listener. One invocation carries exactly one
// connection.
type Listener struct {
	mu        sync.Mutex
	listening bool
	delivered bool
	conn      net.Conn
	ctx       context.Context

	connCh    chan net.Conn
	doneOnce  sync.Once
	doneCh    chan struct{}
	closeOnce sync.Once
	closedCh  chan struct{}
}

var _ net.Listener = (*Listener)(nil)

func newListener() *Listener {
	return &Listener{
		ctx:      context.Background(),
		connCh:   make(chan net.Conn, 1),
		doneCh:   make(chan struct{}),
		closedCh: make(chan struct{}),
	}
}

var defaultListener = newListener()

var readyOnce sync.Once

// signalReady notifies the runtime that the Go side's setup is complete. It
// is idempotent and safe to call from multiple goroutines.
func signalReady() {
	readyOnce.Do(workers.Ready)
}

// Listen returns the process-wide Listener for inbound sockets. Calling it
// multiple times returns the same Listener.
func Listen() *Listener {
	defaultListener.mu.Lock()
	defaultListener.listening = true
	defaultListener.mu.Unlock()
	return defaultListener
}

// isListening reports whether Listen (or Serve/ServeNonBlock) has been
// called for this instance.
func (l *Listener) isListening() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.listening
}

// deliver hands the given conn to the (single) pending or future Accept
// call. It returns errAlreadyDelivered if a connection was already delivered
// in this invocation, or net.ErrClosed if the Listener has been closed in
// the meantime.
func (l *Listener) deliver(ctx context.Context, conn net.Conn) error {
	l.mu.Lock()
	if l.delivered {
		l.mu.Unlock()
		return errAlreadyDelivered
	}
	l.delivered = true
	l.conn = conn
	l.ctx = ctx
	l.mu.Unlock()

	select {
	case l.connCh <- conn:
		return nil
	case <-l.closedCh:
		return net.ErrClosed
	}
}

// connClosed must be called exactly once, after the delivered connection's
// Close method has run to completion, to unblock any pending/future Accept
// call with net.ErrClosed.
func (l *Listener) connClosed() {
	l.doneOnce.Do(func() {
		close(l.doneCh)
	})
}

// Accept returns this invocation's connection once. It signals readiness to
// the runtime on first call (idempotent). After that connection has been
// closed, or after the Listener is closed, Accept returns net.ErrClosed. In
// invocations that are not connect events (e.g. fetch), Accept blocks
// forever; that is fine because the instance is discarded.
func (l *Listener) Accept() (net.Conn, error) {
	signalReady()
	select {
	case conn, ok := <-l.connCh:
		if !ok {
			return nil, net.ErrClosed
		}
		return conn, nil
	case <-l.doneCh:
		return nil, net.ErrClosed
	case <-l.closedCh:
		return nil, net.ErrClosed
	}
}

// Close closes the listener. Subsequent Accept calls return net.ErrClosed.
func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closedCh)
	})
	return nil
}

// Addr returns the local network address of the delivered connection. It
// never returns nil. Before a connection has been delivered, it returns a
// zero Addr (Network() == "tcp", String() == "").
func (l *Listener) Addr() net.Addr {
	l.mu.Lock()
	conn := l.conn
	l.mu.Unlock()
	if conn == nil {
		return Addr{}
	}
	return conn.LocalAddr()
}

// Context returns a context carrying the inbound socket as the trigger
// object (see internal/runtimecontext), once a connection has been
// delivered. Before that, it returns a context.Background()-derived
// context.
func (l *Listener) Context() context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ctx
}

// resetForTest resets the package-level listener and Serve/ServeNonBlock
// state so tests can run independently of each other. It is not exported: it
// is only meant to be used by this package's own tests.
func resetForTest() {
	defaultListener = newListener()
	readyOnce = sync.Once{}
	resetServeStateForTest()
}
