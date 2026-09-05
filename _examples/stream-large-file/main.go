package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/fetch"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		cli := fetch.NewClient().HTTPClient(fetch.RedirectModeFollow)
		resp, err := cli.Get("http://tyo.download.datapacket.com/1000mb.bin")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "err: %v", err)
			return
		}
		defer resp.Body.Close()

		io.Copy(w, resp.Body)
	})
	workers.Serve(nil)
}
