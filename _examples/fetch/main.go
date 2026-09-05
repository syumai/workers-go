package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/fetch"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cli := fetch.NewClient()

		r, err := fetch.NewRequest(req.Context(), http.MethodGet, "https://api.github.com/repos/syumai/workers-go/releases/latest", nil)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/111.0")

		res, err := cli.Do(r, nil)
		if err != nil {
			fmt.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("content-type", "application/json")
		io.Copy(w, res.Body)
	})
	workers.Serve(handler)
}
