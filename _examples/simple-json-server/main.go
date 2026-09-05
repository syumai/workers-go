package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/syumai/workers-go"
)

type HelloRequest struct {
	Name string `json:"name"`
}

type HelloResponse struct {
	Message string `json:"message"`
}

func HelloHandler(w http.ResponseWriter, req *http.Request) {
	var helloReq HelloRequest
	if err := json.NewDecoder(req.Body).Decode(&helloReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("request format is invalid"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	msg := fmt.Sprintf("Hello, %s!", helloReq.Name)

	helloRes := HelloResponse{Message: msg}
	if err := json.NewEncoder(w).Encode(helloRes); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode response: %w\n", err)
	}
}

func main() {
	http.HandleFunc("/hello", HelloHandler)
	workers.Serve(nil) // use http.DefaultServeMux
}
