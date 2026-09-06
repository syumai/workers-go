//go:build js && wasm

// Command kitchensink is an e2e fixture Worker that exposes a thin,
// operation-shaped HTTP API over a subset of the Go SDK (net/http handler,
// cloudflare.Getenv, cloudflare/kv). Handlers only return the SDK's raw
// results as JSON/plain text; assertions live in the e2e tests, not here.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// kvBindingName is the KV namespace binding name declared in wrangler.jsonc.
const kvBindingName = "KV"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/stream", handleStream)
	mux.HandleFunc("/fixed", handleFixed)
	mux.HandleFunc("/env/", handleEnv)
	mux.HandleFunc("/kv", handleKVList)
	mux.HandleFunc("/kv/", handleKVItem)

	workers.ServeNonBlock(mux)
	workers.Ready()
	<-workers.Done()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// echoResponse mirrors the shape of the inbound request so tests can assert
// on it. Headers are kept as map[string][]string (http.Header's underlying
// type) so multi-value headers survive the round trip.
type echoResponse struct {
	Method     string              `json:"method"`
	URL        string              `json:"url"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	RemoteAddr string              `json:"remoteAddr"`
	Host       string              `json:"host"`
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	// NOTE: internal/jshttp.ToBody (called from ToRequest) returns a true
	// nil io.ReadCloser when the incoming JS Request's body is null,
	// which is the case for GET/HEAD and other bodyless requests. Unlike
	// net/http's own server (which always sets a non-nil, empty Body),
	// this SDK does not paper over that, so callers must guard for it.
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body = b
	}
	resp := echoResponse{
		Method:     r.Method,
		URL:        r.URL.String(),
		Headers:    map[string][]string(r.Header),
		Body:       string(body),
		RemoteAddr: r.RemoteAddr,
		Host:       r.Host,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleStream writes n lines, flushing after each one, so the response is
// observable as it streams rather than only once fully buffered.
func handleStream(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || n < 0 {
		http.Error(w, "invalid n", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	flusher, _ := w.(http.Flusher)
	for i := 0; i < n; i++ {
		io.WriteString(w, "line "+strconv.Itoa(i)+"\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// handleFixed sets an explicit Content-Length so the SDK routes the
// response body through the FixedLengthStream path (see
// internal/jshttp/response.go's newJSResponse).
func handleFixed(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.Atoi(r.URL.Query().Get("size"))
	if err != nil || size < 0 {
		http.Error(w, "invalid size", http.StatusBadRequest)
		return
	}
	body := strings.Repeat("a", size)
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, body)
}

func handleEnv(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/env/")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, cloudflare.Getenv(name))
}

// jsNullString is the literal string syscall/js's Value.String() returns
// for a JS null value (see the switch in Go's src/syscall/js/js.go). KV's
// get() resolves to null on a miss, and kv.Namespace.GetString does not
// special-case that, so a miss surfaces here as exactly this string. This
// is deliberate: it is the real SDK behavior we want the e2e test to pin.
const jsNullString = "<null>"

func handleKVItem(w http.ResponseWriter, r *http.Request) {
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
		if r.Body != nil { // see the NOTE in handleEcho about nil Body.
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
		if v == jsNullString {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, v)
	case http.MethodDelete:
		if err := ns.Delete(key); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type kvListResponse struct {
	Keys []string `json:"keys"`
}

func handleKVList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	prefix := r.URL.Query().Get("prefix")
	result, err := ns.List(&kv.ListOptions{Prefix: prefix})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	keys := make([]string, len(result.Keys))
	for i, k := range result.Keys {
		keys[i] = k.Name
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(kvListResponse{Keys: keys}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
