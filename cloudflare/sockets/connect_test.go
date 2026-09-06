//go:build js && wasm

package sockets

import (
	"bytes"
	"context"
	"errors"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jstest"
)

func TestSocketOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts *SocketOptions
		want map[string]any
	}{
		"nil":             {opts: nil, want: map[string]any{}},
		"empty":           {opts: &SocketOptions{}, want: map[string]any{}},
		"secure_on":       {opts: &SocketOptions{SecureTransport: SecureTransportOn}, want: map[string]any{"secureTransport": "on"}},
		"secure_off":      {opts: &SocketOptions{SecureTransport: SecureTransportOff}, want: map[string]any{"secureTransport": "off"}},
		"secure_starttls": {opts: &SocketOptions{SecureTransport: SecureTransportStartTLS}, want: map[string]any{"secureTransport": "starttls"}},
		"allow_half_open": {opts: &SocketOptions{AllowHalfOpen: true}, want: map[string]any{"allowHalfOpen": true}},
		"combined": {
			opts: &SocketOptions{SecureTransport: SecureTransportOn, AllowHalfOpen: true},
			want: map[string]any{"secureTransport": "on", "allowHalfOpen": true},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toJS()
			jstest.AssertObjectEqual(t, got, tt.want)
		})
	}
}

func TestConnect_missingContext(t *testing.T) {
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{})

	_, err := Connect(context.Background(), "example.com:443", nil)
	if !errors.Is(err, cfruntimecontext.ErrValueNotFound) {
		t.Fatalf("Connect() error = %v, want errors.Is(_, cfruntimecontext.ErrValueNotFound)", err)
	}
}

func TestConnect_withFakeConnect(t *testing.T) {
	fs := newFakeSocket([][]byte{[]byte("pong")}, js.Value{})

	var lastAddr string
	var lastOpts js.Value
	connectFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		lastAddr = args[0].String()
		if len(args) > 1 {
			lastOpts = args[1]
		}
		return fs.val
	})
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Connect: connectFn.Value})

	conn, err := Connect(context.Background(), "example.com:443", &SocketOptions{AllowHalfOpen: true})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if lastAddr != "example.com:443" {
		t.Errorf("connect() called with addr %q, want %q", lastAddr, "example.com:443")
	}
	if got := lastOpts.Get("allowHalfOpen").Bool(); !got {
		t.Errorf("connect() options.allowHalfOpen = %v, want true", got)
	}

	wantWritten := []byte("ping")
	if _, err := conn.Write(wantWritten); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := fs.writtenBytes(); !bytes.Equal(got, wantWritten) {
		t.Errorf("written = %q, want %q", got, wantWritten)
	}

	buf := make([]byte, 4)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := string(buf[:n]); got != "pong" {
		t.Errorf("Read() = %q, want %q", got, "pong")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !fs.closeCalled() {
		t.Error("close was not called on the underlying fake socket")
	}
}
