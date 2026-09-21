package sockets

import (
	"context"
	"syscall/js"
	"testing"
	"time"
)

// TestSocket_ZeroDeadlineMeansNoTimeout verifies that, per the net.Conn
// convention, setting a zero time.Time deadline means "no timeout" rather
// than an already-expired deadline. Before the fix, Read/Write derived
// their per-call context via context.WithDeadline(ctx, deadline) even when
// deadline was the zero value, which produces a context that is already
// expired (time.Time{} is far in the past), so every Read/Write failed
// immediately with os.ErrDeadlineExceeded.
func TestSocket_ZeroDeadlineMeansNoTimeout(t *testing.T) {
	resetForTest()

	fs := newFakeSocket([][]byte{[]byte("hello")}, js.Value{})
	sock := newSocket(context.Background(), fs.val, time.Now().Add(time.Hour), time.Now().Add(time.Hour))

	if err := sock.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("SetDeadline(zero) failed: %v", err)
	}

	buf := make([]byte, 5)
	n, err := sock.Read(buf)
	if err != nil {
		t.Fatalf("Read after SetDeadline(zero) failed: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Fatalf("Read = %q, want %q", got, "hello")
	}

	if _, err := sock.Write([]byte("world")); err != nil {
		t.Fatalf("Write after SetDeadline(zero) failed: %v", err)
	}
	if got := fs.writtenBytes(); string(got) != "world" {
		t.Fatalf("written = %q, want %q", got, "world")
	}
}
