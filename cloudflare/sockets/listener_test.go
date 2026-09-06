package sockets

import (
	"context"
	"io"
	"net"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestHandleConnect_FailFastWithoutListen(t *testing.T) {
	resetForTest()

	fs := newFakeSocket(nil, js.Value{})
	promise := jsutil.Binding.Get("handleConnect").Invoke(fs.val)
	_, err := jsutil.AwaitPromise(promise)
	if err == nil {
		t.Fatalf("expected handleConnect to reject, got nil error")
	}
	if !fs.closeCalled() {
		t.Fatalf("expected socket.close() to have been called")
	}
}

func TestListener_HappyPath(t *testing.T) {
	resetForTest()

	lis := Listen()

	fs := newFakeSocket([][]byte{[]byte("hello "), []byte("world")}, openedInfo("127.0.0.1:4242", "203.0.113.9:51000"))
	promise := jsutil.Binding.Get("handleConnect").Invoke(fs.val)

	conn, err := lis.Accept()
	if err != nil {
		t.Fatalf("Accept() failed: %v", err)
	}

	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("read = %q, want %q", got, "hello world")
	}

	if _, err := conn.Write([]byte("response")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if got := fs.writtenBytes(); string(got) != "response" {
		t.Fatalf("written = %q, want %q", got, "response")
	}

	remoteAddr := conn.RemoteAddr()
	tcpAddr, ok := remoteAddr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("RemoteAddr() = %#v (%T), want *net.TCPAddr", remoteAddr, remoteAddr)
	}
	if tcpAddr.String() != "203.0.113.9:51000" {
		t.Fatalf("RemoteAddr() = %v, want %v", tcpAddr, "203.0.113.9:51000")
	}

	localAddr := conn.LocalAddr()
	localTCPAddr, ok := localAddr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("LocalAddr() = %#v (%T), want *net.TCPAddr", localAddr, localAddr)
	}
	if localTCPAddr.String() != "127.0.0.1:4242" {
		t.Fatalf("LocalAddr() = %v, want %v", localTCPAddr, "127.0.0.1:4242")
	}

	if lisAddr := lis.Addr(); lisAddr == nil {
		t.Fatalf("Listener.Addr() = nil, want non-nil")
	} else if lisAddr.String() != "127.0.0.1:4242" {
		t.Fatalf("Listener.Addr() = %v, want %v", lisAddr, "127.0.0.1:4242")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if !fs.closeCalled() {
		t.Fatalf("expected socket.close() to have been called")
	}

	if _, err := jsutil.AwaitPromise(promise); err != nil {
		t.Fatalf("expected handleConnect's Promise to resolve, got error: %v", err)
	}

	if _, err := lis.Accept(); err != net.ErrClosed {
		t.Fatalf("second Accept() error = %v, want %v", err, net.ErrClosed)
	}
}

func TestSocket_RemoteAddr_NoAddresses(t *testing.T) {
	resetForTest()

	lis := Listen()
	fs := newFakeSocket(nil, jsutil.PromiseClass.Call("resolve", jsutil.NewObject()))
	jsutil.Binding.Get("handleConnect").Invoke(fs.val)

	conn, err := lis.Accept()
	if err != nil {
		t.Fatalf("Accept() failed: %v", err)
	}
	defer conn.Close()

	remoteAddr := conn.RemoteAddr()
	if remoteAddr == nil {
		t.Fatalf("RemoteAddr() = nil, want non-nil")
	}
	if remoteAddr.Network() != "tcp" {
		t.Fatalf("RemoteAddr().Network() = %v, want %v", remoteAddr.Network(), "tcp")
	}
	if remoteAddr.String() != "" {
		t.Fatalf("RemoteAddr().String() = %q, want %q", remoteAddr.String(), "")
	}
}

func TestServeNonBlock_Echo(t *testing.T) {
	resetForTest()

	echoed := make(chan struct{})
	ServeNonBlock(HandlerFunc(func(ctx context.Context, conn net.Conn) {
		io.Copy(conn, conn)
		close(echoed)
	}))

	fs := newFakeSocket([][]byte{[]byte("ping")}, js.Value{})
	promise := jsutil.Binding.Get("handleConnect").Invoke(fs.val)

	select {
	case <-echoed:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the handler to finish echoing")
	}

	select {
	case <-Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for Done()")
	}

	if got := fs.writtenBytes(); string(got) != "ping" {
		t.Fatalf("written = %q, want %q", got, "ping")
	}
	if !fs.closeCalled() {
		t.Fatalf("expected socket.close() to have been called")
	}

	if _, err := jsutil.AwaitPromise(promise); err != nil {
		t.Fatalf("expected handleConnect's Promise to resolve, got error: %v", err)
	}
}
