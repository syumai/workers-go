//go:build js && wasm

// Command durableobject is an e2e fixture Worker that exercises the Go SDK's
// Durable Object stub (cloudflare.NewDurableObjectNamespace) against a real
// JS-defined Durable Object class (do.mjs's Counter). Handlers only return
// the SDK's raw results as plain text; assertions live in the e2e tests,
// not here.
package main

import (
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
)

// counterBindingName is the Durable Object namespace binding name declared
// in wrangler.jsonc.
const counterBindingName = "COUNTER"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/counter", handleCounter)

	workers.ServeNonBlock(mux)
	workers.Ready()
	<-workers.Done()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleCounter looks up the Durable Object instance named by the "name"
// query parameter and forwards the request to its fetch() handler, which
// increments and returns a per-instance counter (see do.mjs's Counter
// class). The response body (the new count) is returned verbatim.
func handleCounter(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}

	ns, err := cloudflare.NewDurableObjectNamespace(counterBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := ns.IdFromName(name)
	obj, err := ns.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The Durable Object's fetch() is called with a GET request that has
	// no body; a nil Body avoids sending an already-consumed or
	// otherwise problematic stream through DurableObjectStub.Fetch.
	req, err := http.NewRequest(http.MethodGet, "https://do/", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := obj.Fetch(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(body)
}
