//go:build js && wasm

// Command pages is an e2e fixture Worker that exercises the Go SDK's
// Pages Functions entry point (workers.Serve's onRequest wiring), which
// only resolves under `wrangler pages dev`. Routes are registered with the
// "/api" prefix because Pages Functions forwards the full incoming
// request, including the path segment functions/api/[[routes]].mjs
// matched on (see functions/api/[[routes]].mjs and
// cmd/workers-assets-gen/assets/common/worker.mjs's onRequest).
package main

import (
	"net/http"

	"github.com/syumai/workers-go"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", handleHealthz)
	mux.HandleFunc("/api/hello", handleHello)

	workers.Serve(mux)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("hello from pages"))
}
