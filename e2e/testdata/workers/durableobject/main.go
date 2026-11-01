//go:build js && wasm

// Command durableobject is an e2e fixture Worker that exercises the Go SDK's
// Durable Object stub (cloudflare.NewDurableObjectNamespace) against a real
// JS-defined Durable Object class (do.mjs's Counter) and hosts a Go Durable
// Object class (Notebook) for SQL storage, transactions, and
// blockConcurrencyWhile. Handlers only return the SDK's raw results;
// assertions live in the e2e tests, not here.
package main

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
)

// counterBindingName is the Durable Object namespace binding name declared
// in wrangler.jsonc for the JS Counter class.
const counterBindingName = "COUNTER"

// notebookBindingName is the binding for the Go-hosted Notebook class.
const notebookBindingName = "NOTEBOOK"

func main() {
	// Register must run before workers.Ready: the Notebook wasm instance
	// constructs its Object only after run() resolves, and it looks the
	// constructor up at that point.
	durableobjects.Register(notebookClassName, newNotebook)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/counter", handleCounter)
	mux.HandleFunc("/counter/rpc", handleCounterRPC)
	mux.HandleFunc("/notebook/", handleNotebook)

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

// handleCounterRPC calls Counter.add through DurableObjectStub.RPC and
// writes the returned count as plain text. "n" is the amount to add.
func handleCounterRPC(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil {
		http.Error(w, "invalid n", http.StatusBadRequest)
		return
	}

	ns, err := cloudflare.NewDurableObjectNamespace(counterBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	obj, err := ns.Get(ns.IdFromName(name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var value int
	if err := obj.RPC().CallJSON("add", &value, n); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(strconv.Itoa(value)))
}

// handleNotebook forwards /notebook/<path> to the Notebook Durable Object
// named by the "name" query parameter. The rest of the query string and the
// body are passed through; the object's response status and body are
// returned verbatim.
func handleNotebook(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	q.Del("name")

	ns, err := cloudflare.NewDurableObjectNamespace(notebookBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	obj, err := ns.Get(ns.IdFromName(name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/notebook")
	if path == "" {
		path = "/"
	}
	target := "https://notebook" + path
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}

	var body io.Reader
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(b) > 0 {
			body = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(r.Method, target, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	res, err := obj.Fetch(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ct := res.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(res.StatusCode)
	w.Write(respBody)
}
