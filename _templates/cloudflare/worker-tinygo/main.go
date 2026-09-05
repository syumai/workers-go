package main

import (
	"bytes"
	"io"
	"net/http"

	"github.com/syumai/workers-go"
)

func main() {
	http.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		msg := "Hello!"
		w.Write([]byte(msg))
	})
	http.HandleFunc("/echo", func(w http.ResponseWriter, req *http.Request) {
		// Read the whole body first: io.Copy(w, req.Body) would pass the raw
		// request ReadableStream through to the JS Response, which fails under
		// `wrangler dev` (miniflare) with "Body has already been used".
		// See https://github.com/syumai/workers-go/issues/176.
		b, err := io.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		io.Copy(w, bytes.NewReader(b))
	})
	workers.Serve(nil) // use http.DefaultServeMux
}
