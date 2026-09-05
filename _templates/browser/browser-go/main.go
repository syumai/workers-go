package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/syumai/workers-go"
)

func main() {
	http.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		name := req.URL.Query().Get("name")
		if name == "" {
			name = "World"
		}
		msg := fmt.Sprintf("Hello, %s!", name)
		w.Write([]byte(msg))
	})
	http.HandleFunc("/echo", func(w http.ResponseWriter, req *http.Request) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		io.Copy(w, bytes.NewReader(b))
	})
	workers.Serve(nil) // use http.DefaultServeMux
}
