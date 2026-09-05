package main

import (
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
)

func main() {
	workers.Serve(&MyHandler{})
}

func canHaveBody(method string) bool {
	return method != "GET" && method != "HEAD" && method != ""
}

type MyHandler struct{}

func (_ *MyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	COUNTER, err := cloudflare.NewDurableObjectNamespace("COUNTER")
	if err != nil {
		panic(err)
	}

	id := COUNTER.IdFromName("A")
	obj, err := COUNTER.Get(id)
	if err != nil {
		panic(err)
	}

	if !canHaveBody(req.Method) {
		req.Body = nil
	}

	res, err := obj.Fetch(req)
	if err != nil {
		panic(err)
	}

	count, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	w.Write([]byte("Durable object 'A' count: " + string(count)))
}
