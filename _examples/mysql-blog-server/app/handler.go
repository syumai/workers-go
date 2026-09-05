package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/syumai/workers-go/_examples/mysql-blog-server/app/model"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/sockets"
)

type articleHandler struct{}

var _ http.Handler = (*articleHandler)(nil)

func NewArticleHandler() http.Handler {
	return &articleHandler{}
}

func (h *articleHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// initialize DB.
	mysql.RegisterDialContext("tcp", func(ctx context.Context, addr string) (net.Conn, error) {
		return sockets.Connect(ctx, addr, &sockets.SocketOptions{
			SecureTransport: sockets.SecureTransportOff,
		})
	})
	db, err := sql.Open("mysql",
		cloudflare.Getenv("MYSQL_DSN"))
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	switch req.Method {
	case http.MethodGet:
		h.listArticles(w, req, db)
		return
	case http.MethodPost:
		h.createArticle(w, req, db)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("not found"))
}

func (h *articleHandler) handleErr(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(msg))
}

func (h *articleHandler) createArticle(w http.ResponseWriter, req *http.Request, db *sql.DB) {
	var createArticleReq model.CreateArticleRequest
	if err := json.NewDecoder(req.Body).Decode(&createArticleReq); err != nil {
		h.handleErr(w, http.StatusBadRequest,
			"request format is invalid")
		return
	}

	now := time.Now().Unix()
	article := model.Article{
		Title:     createArticleReq.Title,
		Body:      createArticleReq.Body,
		CreatedAt: uint64(now),
	}

	result, err := db.Exec(`
INSERT INTO articles (title, body, created_at)
VALUES (?, ?, ?)
   `, article.Title, article.Body, article.CreatedAt)
	if err != nil {
		log.Println(err)
		h.handleErr(w, http.StatusInternalServerError,
			"failed to save article")
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		log.Println(err)
		h.handleErr(w, http.StatusInternalServerError,
			"failed to get ID of inserted article")
		return
	}
	article.ID = uint64(id)

	res := model.CreateArticleResponse{
		Article: article,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode response: %w\n", err)
	}
}

func (h *articleHandler) listArticles(w http.ResponseWriter, req *http.Request, db *sql.DB) {
	rows, err := db.Query(`
SELECT id, title, body, created_at FROM articles
ORDER BY created_at DESC;
   `)
	if err != nil {
		log.Println(err)
		h.handleErr(w, http.StatusInternalServerError,
			"failed to load article")
		return
	}

	articles := []model.Article{}
	for rows.Next() {
		var a model.Article
		err = rows.Scan(&a.ID, &a.Title, &a.Body, &a.CreatedAt)
		if err != nil {
			log.Println(err)
			h.handleErr(w, http.StatusInternalServerError,
				"failed to scan article")
			return
		}
		articles = append(articles, a)
	}
	res := model.ListArticlesResponse{
		Articles: articles,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode response: %w\n", err)
	}
}
