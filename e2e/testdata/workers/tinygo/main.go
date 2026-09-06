//go:build js && wasm

// Command tinygo is an e2e fixture Worker built with the tinygo toolchain
// (workers-assets-gen -mode=tinygo + `tinygo build`) instead of the
// standard go toolchain, to check that the tinygo-targeted wasm_exec.js
// and runtime wiring still work end-to-end under a real workerd. It is a
// deliberately separate, smaller source than
// testdata/workers/kitchensink/main.go (which is owned by another PR and
// must not be edited here): only the hello/echo/kv subset is reproduced.
//
// Handlers only return the SDK's raw results as plain text; assertions
// live in the e2e tests, not here.
package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// kvBindingName is the KV namespace binding name declared in
// wrangler.jsonc.
const kvBindingName = "KV"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/hello", handleHello)
	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/kv/", handleKV)

	workers.Serve(mux)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, "hello")
}

// handleEcho reads the whole request body before writing it back.
// io.Copy(w, req.Body) would pass the raw request ReadableStream through
// to the JS Response, which fails under `wrangler dev`/`wrangler pages
// dev` (miniflare) with "Body has already been used". See
// _examples/pages-functions/main.go and
// https://github.com/syumai/workers-go/issues/176.
func handleEcho(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body = b
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(body)
}

func handleKV(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body []byte
		if r.Body != nil {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body = b
		}
		if err := ns.PutString(key, string(body), nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		v, err := ns.GetString(key, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// See testdata/workers/kitchensink/main.go's jsNullString: a
		// missing key resolves to a literal "<null>" string.
		if v == "<null>" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, v)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
