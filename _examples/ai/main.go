// Command ai calls Workers AI (exp/cloudflare/ai) from a fetch handler.
//
// GET / runs @cf/baai/bge-base-en-v1.5 and returns the embedding length.
// The vector itself is not returned: its values are not stable, and
// @cf/meta/llama-3.1-8b-instruct no longer runs (error 5028, deprecated
// 2026-05-30).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/ai"
)

const embeddingsModel = "@cf/baai/bge-base-en-v1.5"

type response struct {
	Model  string    `json:"model"`
	Length int       `json:"length"`
	Shape  []float64 `json:"shape,omitempty"`
}

func main() {
	workers.Serve(http.HandlerFunc(handle))
}

func handle(w http.ResponseWriter, r *http.Request) {
	binding, err := ai.NewAi("AI")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	inputs := js.ValueOf(map[string]any{
		"text": []any{"hello from workers-go"},
	})
	out, err := binding.Run(embeddingsModel, inputs, nil)
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
	if err := json.NewEncoder(w).Encode(response{
		Model:  embeddingsModel,
		Length: length,
		Shape:  shape,
	}); err != nil {
		log.Println(err)
	}
}

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
