//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/exp/cloudflare/ai"
)

// aiEmbeddingsModel is a text-embeddings model that still runs on Workers
// AI. @cf/meta/llama-3.1-8b-instruct was removed (error 5028, deprecated
// 2026-05-30), so the fixture does not call it.
const aiEmbeddingsModel = "@cf/baai/bge-base-en-v1.5"

// aiResponse is the JSON shape of GET /ai. length is the embedding
// dimension (the inner vector), not the numeric values themselves.
type aiResponse struct {
	Model  string    `json:"model"`
	Length int       `json:"length"`
	Shape  []float64 `json:"shape,omitempty"`
}

// handleAI calls exp/cloudflare/ai Ai.Run for one short string and returns
// the embedding length. A binding or upstream failure is returned as 502
// with the error text so the e2e test can skip when no Cloudflare account
// is configured.
func handleAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	binding, err := ai.NewAi("AI")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	inputs := js.ValueOf(map[string]any{
		"text": []any{"hello from workers-go"},
	})
	out, err := binding.Run(aiEmbeddingsModel, inputs, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	length, shape, err := embeddingLength(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(aiResponse{
		Model:  aiEmbeddingsModel,
		Length: length,
		Shape:  shape,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// embeddingLength reads the dimension of a Workers AI embeddings result.
// data is either one flat vector or a batch of vectors (number[][]). A
// {result: ...} wrapper, which some responses still use, is unwrapped.
// The numeric values are ignored.
func embeddingLength(v js.Value) (int, []float64, error) {
	if v.IsUndefined() || v.IsNull() || v.Type() != js.TypeObject {
		return 0, nil, fmt.Errorf("ai: empty response")
	}
	data := v.Get("data")
	if data.IsUndefined() || data.IsNull() {
		if res := v.Get("result"); res.Type() == js.TypeObject {
			return embeddingLength(res)
		}
		return 0, nil, fmt.Errorf("ai: response has no data")
	}
	if data.Type() != js.TypeObject || data.Length() == 0 {
		return 0, nil, fmt.Errorf("ai: data is empty")
	}
	var shape []float64
	if s := v.Get("shape"); s.Type() == js.TypeObject && s.Length() > 0 {
		shape = make([]float64, s.Length())
		for i := range shape {
			shape[i] = s.Index(i).Float()
		}
	}
	first := data.Index(0)
	if first.Type() == js.TypeNumber {
		return data.Length(), shape, nil
	}
	if first.Type() != js.TypeObject || first.Length() == 0 {
		return 0, shape, fmt.Errorf("ai: first embedding is empty")
	}
	return first.Length(), shape, nil
}
