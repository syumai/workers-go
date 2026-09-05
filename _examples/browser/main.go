package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go"
)

type AddRequest struct {
	A int `json:"a"`
	B int `json:"b"`
}

func main() {
	http.HandleFunc("POST /add", func(w http.ResponseWriter, req *http.Request) {
		var addReq AddRequest
		if err := json.NewDecoder(req.Body).Decode(&addReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result := addReq.A + addReq.B
		if err := json.NewEncoder(w).Encode(result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})
	workers.Serve(nil)
}
