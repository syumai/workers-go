package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/queues"
)

const queueName = "QUEUE"

func handleErr(w http.ResponseWriter, msg string, err error) {
	log.Println(err)
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(msg))
}

func main() {
	// start Qeueue consumer.
	// If we would not have an HTTP handler in this worker, we would use queues.Consume instead
	queues.ConsumeNonBlock(consumeBatch)

	// start HTTP server
	http.HandleFunc("/", handleProduce)
	workers.Serve(nil)
}

func handleProduce(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defer req.Body.Close()

	q, err := queues.NewProducer(queueName)
	if err != nil {
		handleErr(w, "failed to init queue", err)
	}

	contentType := req.Header.Get("Content-Type")
	switch contentType {
	case "text/plain":
		log.Println("Handling text content type")
		err = produceText(q, req)
	case "application/json":
		log.Println("Handling json content type")
		err = produceJSON(q, req)
	default:
		log.Println("Handling bytes content type")
		err = produceBytes(q, req)
	}

	if err != nil {
		handleErr(w, "failed to handle request", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("message sent\n"))
}

func produceText(q *queues.Producer, req *http.Request) error {
	content, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	// text content type supports string
	if err := q.SendText(string(content)); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

func produceJSON(q *queues.Producer, req *http.Request) error {
	var data any
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	// json content type supports messages of types that can be serialized to json
	if err := q.SendJSON(data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

func produceBytes(q *queues.Producer, req *http.Request) error {
	content, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	// bytes content type support messages of type []byte
	if err := q.SendBytes(content); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

func consumeBatch(batch *queues.MessageBatch) error {
	for _, msg := range batch.Messages {
		log.Printf("Received message: %v\n", msg.Body.Get("name").String())
	}

	batch.AckAll()
	return nil
}
