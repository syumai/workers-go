package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/fetch"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p, err := fetch.NewIncomingProperties(req.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		encoder := json.NewEncoder(w)
		w.Header().Set("Content-Type", "application/json")
		if err := encoder.Encode(p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	workers.Serve(handler)
}
