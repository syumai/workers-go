package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/_examples/d1-blog-server/app"
	_ "github.com/syumai/workers-go/cloudflare/d1" // register driver
)

func main() {
	db, err := sql.Open("d1", "DB")
	if err != nil {
		log.Fatalf("error opening DB: %s", err.Error())
	}
	http.Handle("/articles", app.NewArticleHandler(db))
	workers.Serve(nil) // use http.DefaultServeMux
}
