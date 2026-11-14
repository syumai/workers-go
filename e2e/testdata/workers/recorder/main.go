//go:build js && wasm

// Command recorder is an e2e fixture Worker that drives generated bindings
// whose real services wrangler dev cannot run locally (Vectorize, Workers for
// Platforms dispatch namespaces) or that always proxy to Cloudflare (Workers
// AI). Each is bound, under its production binding name, to a fake in
// callee.mjs that reports the JS values it received. That makes the
// Go -> JS conversion done by the generated code observable in a real
// workerd, without needing a Cloudflare account.
//
// Handlers only call the generated API with fixed Go values and return the
// fake's description verbatim; assertions live in the e2e tests.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/ai"
	"github.com/syumai/workers-go/exp/cloudflare/dispatch"
	"github.com/syumai/workers-go/exp/cloudflare/vectorize"
	"github.com/syumai/workers-go/internal/jsutil"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/vectorize/insert", handleVectorizeInsert)
	mux.HandleFunc("/vectorize/query", handleVectorizeQuery)
	mux.HandleFunc("/dispatch/get", handleDispatchGet)
	mux.HandleFunc("/ai/gateway", handleAIGateway)
	workers.Serve(mux)
}

// metadataValues is the Go-side metadata shared by the handlers. Vectorize
// documents string, number, boolean and string-array metadata values, so
// those are included alongside the []byte and time.Time that js.ValueOf
// cannot represent.
func metadataValues() map[string]any {
	if only := currentOnly; only != "" {
		all := allMetadataValues()
		return map[string]any{only: all[only]}
	}
	return allMetadataValues()
}

// currentOnly narrows metadataValues to one key; set per request from ?only=.
var currentOnly string

func allMetadataValues() map[string]any {
	return map[string]any{
		"str":      "text",
		"num":      1.5,
		"int":      int64(7),
		"bool":     true,
		"nil":      nil,
		"strs":     []string{"a", "b"},
		"anys":     []any{"a", 2.0},
		"nested":   map[string]any{"k": "v"},
		"bytes":    []byte{1, 2, 3},
		"time":     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		"jsvalue":  js.ValueOf("raw"),
		"nestedby": map[string]any{"b": []byte{9}},
		// No JS representation: the generated call must return an error
		// rather than panic outside jsrt.Call's recover.
		"unconvertible":        make(chan int),
		"unconvertible_nested": map[string]any{"k": []any{func() {}}},
	}
}

// guard runs fn and converts a panic into a reported error. Generated struct
// toJS methods build their JS object before jsrt.Call is reached, so a
// js.ValueOf panic there is NOT covered by jsrt.Call's recover and would
// otherwise take the whole request down. Reporting it as "panic: ..." keeps
// that distinct from an error the generated method returned.
func guard(fn func() (js.Value, error)) (reply js.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			reply = js.Value{}
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

// awaitReply resolves a JS Promise returned by an RPC call on the fake.
func awaitReply(p js.Value) (js.Value, error) {
	return jsutil.AwaitPromise(p)
}

// respond writes the fake's reply, or the Go-side error when the generated
// call itself failed (e.g. a panic in js.ValueOf recovered by jsrt.Call).
func respond(w http.ResponseWriter, reply js.Value, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"go_error": err.Error()})
		return
	}
	w.Write([]byte(js.Global().Get("JSON").Call("stringify", reply).String()))
}

func handleVectorizeInsert(w http.ResponseWriter, r *http.Request) {
	currentOnly = r.URL.Query().Get("only")
	idx, err := vectorize.NewVectorize("VECTORIZE")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Vectorize.Insert returns a typed mutation, which the fake's reply
	// cannot populate; read the raw reply from the underlying binding by
	// calling the generated method and then ignoring its decoded value.
	// The reply is fetched separately through JSValue() so it can be shown.
	reply, callErr := guard(func() (js.Value, error) {
		if _, err := idx.Insert([]vectorize.VectorizeVector{{
			ID:       "v1",
			Values:   []float32{1, 2},
			Metadata: metadataValues(),
		}}); err != nil {
			return js.Value{}, err
		}
		return awaitReply(idx.JSValue().Call("lastInsert"))
	})
	respond(w, reply, callErr)
}

func handleVectorizeQuery(w http.ResponseWriter, r *http.Request) {
	currentOnly = r.URL.Query().Get("only")
	idx, err := vectorize.NewVectorize("VECTORIZE")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply, callErr := guard(func() (js.Value, error) {
		if _, err := idx.Query([]float32{1, 2}, vectorize.VectorizeQueryOptions{
			TopK:   3,
			Filter: metadataValues(),
		}); err != nil {
			return js.Value{}, err
		}
		return awaitReply(idx.JSValue().Call("lastQuery"))
	})
	respond(w, reply, callErr)
}

func handleDispatchGet(w http.ResponseWriter, r *http.Request) {
	currentOnly = r.URL.Query().Get("only")
	ns, err := dispatch.NewDispatchNamespace("DISPATCH")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply, callErr := guard(func() (js.Value, error) {
		if _, err := ns.Get("customer", metadataValues(), dispatch.DynamicDispatchOptions{
			Outbound: metadataValues(),
		}); err != nil {
			return js.Value{}, err
		}
		return awaitReply(ns.JSValue().Call("lastGet"))
	})
	respond(w, reply, callErr)
}

func handleAIGateway(w http.ResponseWriter, r *http.Request) {
	currentOnly = r.URL.Query().Get("only")
	binding, err := ai.NewAi("AI")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply, callErr := guard(func() (js.Value, error) {
		return binding.Run("@cf/test/model", js.ValueOf(map[string]any{"text": "hi"}), &ai.AiOptions{
			Gateway: &ai.GatewayOptions{ID: "gw", Metadata: metadataValues()},
		})
	})
	respond(w, reply, callErr)
}
