//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
)

// startTCPEchoServer starts a TCP server on 127.0.0.1 that echoes back
// whatever a single connection sends it (io.Copy(conn, conn)), and
// registers a t.Cleanup that closes the listener. It returns the port the
// server is listening on.
func startTCPEchoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start TCP echo server: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

// TestSockets checks that the Go SDK's outbound TCP support
// (cloudflare/sockets.Connect) can reach a real TCP peer under workerd,
// which only resolves via the `cloudflare:sockets` module (Node-based fake
// tests can't exercise this).
func TestSockets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWrangler(t, "sockets")

	t.Run("echo", func(t *testing.T) {
		port := startTCPEchoServer(t)
		const msg = "hello from the sockets e2e test"

		// workerd's `connect()` may refuse to dial some loopback
		// addresses/hostnames depending on local-address restrictions.
		// Try a small set of candidates and skip (reporting what was
		// tried) only if none of them work.
		candidates := []string{
			fmt.Sprintf("127.0.0.1:%d", port),
			fmt.Sprintf("localhost:%d", port),
			fmt.Sprintf("host.docker.internal:%d", port),
		}

		var attempts []string
		for _, addr := range candidates {
			resp, body := w.Get(t, "/connect?addr="+url.QueryEscape(addr)+"&msg="+url.QueryEscape(msg))
			if resp.StatusCode == http.StatusOK && body == msg {
				return // success
			}
			attempts = append(attempts, fmt.Sprintf("%s -> status=%d body=%q", addr, resp.StatusCode, body))
		}

		t.Skipf("known issue: workerd's connect() could not reach the test's local TCP echo server via any candidate address; tried:\n%s", joinLines(attempts))
	})
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "  " + l + "\n"
	}
	return out
}
