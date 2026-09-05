package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// counterNamespace is a bounded KV namespace for storing counter.
const counterNamespace = "COUNTER"

// countKey is a key to store current count value to the KV namespace.
const countKey = "count"

func handleErr(w http.ResponseWriter, msg string, err error) {
	log.Println(err)
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(msg))
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// initialize KV namespace instance
		counterKV, err := kv.NewNamespace(counterNamespace)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init KV: %v", err)
			os.Exit(1)
		}

		countStr, err := counterKV.GetString(countKey, nil)
		if err != nil {
			handleErr(w, "failed to get current count\n", err)
			return
		}

		// ignore err and treat count value as 0
		count, _ := strconv.Atoi(countStr)

		nextCountStr := strconv.Itoa(count + 1)

		err = counterKV.PutString(countKey, nextCountStr, nil)
		if err != nil {
			handleErr(w, "failed to put next count\n", err)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(nextCountStr))
	})

	workers.Serve(nil)
}
