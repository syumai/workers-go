package main

import (
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/_examples/mysql-blog-server/app"
)

func main() {
	http.Handle("/articles", app.NewArticleHandler())
	workers.Serve(nil) // use http.DefaultServeMux
}
