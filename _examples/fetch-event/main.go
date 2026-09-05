package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/fetch"
)

func handler(w http.ResponseWriter, req *http.Request) {
	cloudflare.PassThroughOnException()

	// logging after responding
	cloudflare.WaitUntil(func() {
		for i := 0; i < 5; i++ {
			time.Sleep(time.Second)
		}
		fmt.Println("5-second task completed")
	})

	// panic if x-error header has provided
	if req.Header.Get("x-error") != "" {
		panic("error")
	}

	// responds with origin server
	fc := fetch.NewClient()
	proxy := httputil.ReverseProxy{
		Transport: fc.HTTPClient(fetch.RedirectModeManual).Transport,
		Director: func(r *http.Request) {
			r.URL = req.URL
		},
	}

	proxy.ServeHTTP(w, req)
}

func main() {
	workers.Serve(http.HandlerFunc(handler))
}
