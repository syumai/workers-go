//go:build !js

package workers

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// freePort asks the OS for an unused TCP port on 127.0.0.1 and returns it.
// The listener is closed immediately so that Serve can bind to it.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}
	return port
}

// waitForListen polls the given address until a TCP connection succeeds or
// the deadline is reached.
func waitForListen(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to start listening", addr)
}

// TestServe_listens confirms that Serve starts an HTTP server on the port
// given via the PORT environment variable, and that it actually serves the
// handler passed to it. Serve never returns (it calls http.ListenAndServe
// directly), so it is invoked from a goroutine and its readiness is
// polled for instead of awaited.
func TestServe_listens(t *testing.T) {
	port := freePort(t)
	t.Setenv("PORT", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	// Serve prints a warning to stderr about running in non-JS mode; this
	// is expected behavior and not an indication of test failure.
	go Serve(mux)

	addr := "127.0.0.1:" + port
	waitForListen(t, addr, 5*time.Second)

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestServeNonBlock_panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("ServeNonBlock() did not panic, want panic in non-JS environment")
		}
	}()
	ServeNonBlock(nil)
}

func TestReady_panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Ready() did not panic, want panic in non-JS environment")
		}
	}()
	Ready()
}

func TestDone_panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Done() did not panic, want panic in non-JS environment")
		}
	}()
	Done()
}
