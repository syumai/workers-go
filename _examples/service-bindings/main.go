package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/fetch"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		bind := cloudflare.GetBinding("hello")
		fc := fetch.NewClient(fetch.WithBinding(bind))

		hc := fc.HTTPClient(fetch.RedirectModeFollow)
		res, err := hc.Do(req)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		io.Copy(w, res.Body)
	})
	workers.Serve(handler)
}
