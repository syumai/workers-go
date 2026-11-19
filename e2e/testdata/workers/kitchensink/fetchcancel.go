//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

// fetchCancelResponse reports how an outbound fetch bounded by a context
// deadline ended. Assertions live in the e2e test.
type fetchCancelResponse struct {
	// Stage is where the deadline hit: "headers" (fetch never returned a
	// response) or "body" (headers arrived, then the body stalled).
	Stage string `json:"stage"`
	// DeadlineExceeded is errors.Is(err, context.DeadlineExceeded).
	DeadlineExceeded bool `json:"deadlineExceeded"`
	// Err is the error text, empty when the fetch succeeded.
	Err string `json:"err"`
	// ElapsedMs is how long the fetch took, to show the deadline (not the
	// server) ended it.
	ElapsedMs int64 `json:"elapsedMs"`
}

// handleFetchCancel fetches ?url= with a ?timeout= (a Go duration) bound on
// the request context. The target is a server that never completes its
// response, so the only way out is the context aborting the fetch.
func handleFetchCancel(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	timeout, err := time.ParseDuration(r.URL.Query().Get("timeout"))
	if target == "" || err != nil {
		http.Error(w, "url and timeout are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	start := time.Now()
	res := fetchCancelResponse{Stage: "headers"}
	req, err := fetch.NewRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, fetchErr := fetch.NewClient().Do(req, nil)
	if fetchErr == nil {
		res.Stage = "body"
		_, fetchErr = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	if fetchErr != nil {
		res.Err = fetchErr.Error()
		res.DeadlineExceeded = errors.Is(fetchErr, context.DeadlineExceeded)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
