package main

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/syumai/workers-go"
)

func main() {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Get("/hello", func(w http.ResponseWriter, req *http.Request) {
			name := req.URL.Query().Get("name")
			if name == "" {
				name = "Pages Functions"
			}
			fmt.Fprintf(w, "Hello, %s!", name)
		})
		r.Get("/hello2", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "Hello, Hello world!")
		})
		r.Get("/hello3", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "Hello, Hello, Hello world!")
		})
	})
	workers.Serve(r)
}
