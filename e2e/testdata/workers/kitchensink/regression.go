//go:build js && wasm

package main

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// issue163Key is the KV key the post-response callback writes after it has
// successfully constructed a JavaScript object (syscall/js.valueNew).
const issue163Key = "issue163"

// issue163Value is js.TypeObject.String(): the callback records that the
// constructor returned an object, which is the call that used to crash
// with "Cannot read properties of undefined (reading 'exports')" once the
// instance had exited.
const issue163Value = "object"

// handleIssue101Empty is the handler from issue #101: it writes neither a
// status nor a body.
func handleIssue101Empty(http.ResponseWriter, *http.Request) {}

// handleIssue101Nobody sets a status and headers and does not write a
// body. OPTIONS mirrors the CORS preflight from issue #162 that surfaced
// the same crash as #163. Other methods use ?status= (default 204), which
// is a null-body status.
func handleIssue101Nobody(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Empty", "1")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	code := http.StatusNoContent
	if q := r.URL.Query().Get("status"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		code = n
	}
	w.WriteHeader(code)
}

// handleIssue163 responds immediately, then — after the handler has
// returned — constructs a JavaScript object. That syscall is valueNew,
// the frame named in issue #163. The result is written to KV so a later
// request can observe that the instance was still alive for the callback.
func handleIssue163(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cloudflare.WaitUntil(func() {
		time.Sleep(20 * time.Millisecond)
		obj := js.Global().Get("Object").New()
		if obj.Type().String() != issue163Value {
			return
		}
		ns, err := kv.NewNamespace(kvBindingName)
		if err != nil {
			return
		}
		_ = ns.PutString(issue163Key, issue163Value, nil)
	})
	w.Header().Set("X-Issue", "163")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "scheduled")
}

// handleIssue163Result reports whether the post-response callback has
// written its marker yet: 200 with the marker once it has, 404 until then.
func handleIssue163Result(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v, err := ns.GetString(issue163Key, nil)
	if errors.Is(err, kv.ErrNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, v)
}
