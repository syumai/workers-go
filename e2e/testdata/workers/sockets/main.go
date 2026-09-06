//go:build js && wasm

// Command sockets is an e2e fixture Worker that exercises the Go SDK's
// outbound TCP support (cloudflare/sockets.Connect), which only resolves
// under a real workerd runtime via the `cloudflare:sockets` module.
// Handlers only return the SDK's raw results as plain text; assertions
// live in the e2e tests, not here.
package main

import (
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/sockets"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/connect", handleConnect)

	workers.ServeNonBlock(mux)
	workers.Ready()
	<-workers.Done()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleConnect opens a TCP connection to addr, writes msg, reads back
// exactly len(msg) bytes, closes the connection, and returns what it read.
// This exercises connect()/Socket end-to-end against a real TCP peer
// (which the e2e test runs in-process).
func handleConnect(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("addr")
	msg := r.URL.Query().Get("msg")
	if addr == "" || msg == "" {
		http.Error(w, "missing addr or msg", http.StatusBadRequest)
		return
	}

	conn, err := sockets.Connect(r.Context(), addr, nil)
	if err != nil {
		http.Error(w, "connect: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(msg)); err != nil {
		http.Error(w, "write: "+err.Error(), http.StatusBadGateway)
		return
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusBadGateway)
		return
	}

	// The deferred conn.Close() above runs after this handler returns,
	// completing the write -> read -> close sequence.
	w.Header().Set("Content-Type", "text/plain")
	w.Write(buf)
}
