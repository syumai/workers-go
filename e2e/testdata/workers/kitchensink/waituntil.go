//go:build js && wasm

package main

import (
	"io"
	"net/http"
	"time"

	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// waitUntilKey is the KV key the waitUntil task writes to, after the HTTP
// response for GET /waituntil has already been sent.
const waitUntilKey = "waituntil"

// waitUntilValue is what handleWaitUntilResult expects to read back once
// the deferred task has run.
const waitUntilValue = "done"

// handleWaitUntil returns 200 immediately, then 100ms later (via
// cloudflare.WaitUntil, i.e. after the response has gone out) writes
// waitUntilValue to KV. GET /waituntil/result lets a test observe when
// that write actually lands.
//
// NOTE: under a real `wrangler dev` (workerd) runtime, resuming this
// goroutine from time.Sleep's scheduled timer reliably fails with "Go
// program has already exited", and the request is then canceled by the
// runtime as hung -- reproduced with delays as short as 10ms. This is a
// real SDK/runtime interaction the e2e test intentionally exercises and
// reports as a known issue (see e2e/http_test.go's
// waituntil/runs_after_response subtest) rather than papering over here.
func handleWaitUntil(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cloudflare.WaitUntil(func() {
		time.Sleep(100 * time.Millisecond)
		ns, err := kv.NewNamespace(kvBindingName)
		if err != nil {
			return
		}
		_ = ns.PutString(waitUntilKey, waitUntilValue, nil)
	})
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "scheduled")
}

// handleWaitUntilResult reports whether the waitUntil task has written its
// value yet: 200 with the value once it has, 404 until then.
func handleWaitUntilResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v, err := ns.GetString(waitUntilKey, nil)
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
}
