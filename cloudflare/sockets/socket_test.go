//go:build js && wasm

package sockets

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// These tests build *Socket directly (rather than via newSocket) so that
// each func field can be swapped out independently.

func TestSocket_Close_callsClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })

	var closeCalled bool
	s := &Socket{
		ctx:           ctx,
		cancel:        cancel,
		reader:        pr,
		readDeadline:  time.Now().Add(time.Hour),
		writeDeadline: time.Now().Add(time.Hour),
		close:         func() { closeCalled = true },
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closeCalled {
		t.Error("close func field was not called")
	}
	if ctx.Err() == nil {
		t.Error("ctx was not canceled after Close")
	}

	// Close cancels the Socket's context; a Read afterwards should fail
	// rather than block forever.
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Read() after Close error = %v, want errors.Is(_, os.ErrDeadlineExceeded)", err)
	}
}

func TestSocket_CloseRead(t *testing.T) {
	var called bool
	s := &Socket{closeRead: func() { called = true }}

	if err := s.CloseRead(); err != nil {
		t.Fatalf("CloseRead() error = %v", err)
	}
	if !called {
		t.Error("closeRead func field was not called")
	}
}

func TestSocket_CloseWrite(t *testing.T) {
	var called bool
	s := &Socket{closeWrite: func() { called = true }}

	if err := s.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite() error = %v", err)
	}
	if !called {
		t.Error("closeWrite func field was not called")
	}
}

func TestSocket_SetDeadline_read(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })

	s := &Socket{
		ctx:    ctx,
		cancel: cancel,
		reader: pr,
	}

	// Nothing is ever written to pw, so the only way Read can return is via
	// the deadline.
	if err := s.SetReadDeadline(time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Read() error = %v, want errors.Is(_, os.ErrDeadlineExceeded)", err)
	}
}

func TestSocket_SetDeadline_write(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Write's internal goroutine stays blocked forever on this Promise
	// (nothing ever calls resolve/reject), so it can still be running after
	// this test returns via the deadline below. Deliberately never Release
	// these js.Func values (unlike jstest.Func's t.Cleanup): if they were
	// released while that goroutine is still in flight, its later call
	// would panic with "call to released function" instead of just leaking
	// harmlessly until the test binary exits.
	writerVal := jsutil.NewObject()
	writerVal.Set("write", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		return jsutil.NewPromise(js.FuncOf(func(_ js.Value, _ []js.Value) any {
			return js.Undefined()
		}))
	}))

	s := &Socket{
		ctx:       ctx,
		cancel:    cancel,
		writerVal: writerVal,
	}

	if err := s.SetWriteDeadline(time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("SetWriteDeadline() error = %v", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("Write() error = %v, want errors.Is(_, os.ErrDeadlineExceeded)", err)
	}
}

// TestSocket_LocalAddr_RemoteAddr fixes the current behavior: neither
// address is available yet.
func TestSocket_LocalAddr_RemoteAddr(t *testing.T) {
	s := &Socket{}
	if got := s.LocalAddr(); got != nil {
		t.Errorf("LocalAddr() = %v, want nil", got)
	}
	if got := s.RemoteAddr(); got != nil {
		t.Errorf("RemoteAddr() = %v, want nil", got)
	}
}

func TestSocket_StartTLS(t *testing.T) {
	upgraded := newFakeSocket(nil, js.Value{})

	var startTLSCalled bool
	s := &Socket{
		ctx: context.Background(),
		startTLS: func() js.Value {
			startTLSCalled = true
			return upgraded.val
		},
		readDeadline:  time.Now().Add(time.Hour),
		writeDeadline: time.Now().Add(time.Hour),
	}

	got := s.StartTLS()
	if !startTLSCalled {
		t.Error("startTLS func field was not called")
	}
	if got == nil {
		t.Fatal("StartTLS() = nil")
	}
	if got == s {
		t.Error("StartTLS() returned the same *Socket instance")
	}
}
