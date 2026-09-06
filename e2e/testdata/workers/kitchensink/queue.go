//go:build js && wasm

package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/syumai/workers-go/cloudflare/kv"
	"github.com/syumai/workers-go/cloudflare/queues"
)

// queueReceivedKeyPrefix namespaces the KV keys the queue consumer writes
// to, so GET /queue/received can find them with a prefix list without
// colliding with any other KV test data.
const queueReceivedKeyPrefix = "queue:"

// handleQueueSend sends the request body as a single text message to the
// QUEUE producer binding.
func handleQueueSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body []byte
	if r.Body != nil { // see the NOTE in handleEcho about nil Body.
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body = b
	}
	producer, err := queues.NewProducer(queueBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := producer.SendText(string(body)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// consumeQueue is the QUEUE consumer: it writes each message's text body to
// KV under queueReceivedKeyPrefix+<message ID>, so GET /queue/received can
// observe delivery without the fixture needing its own in-memory state
// (which wouldn't survive wrangler dev's isolate reloads anyway).
func consumeQueue(batch *queues.MessageBatch) error {
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		return err
	}
	for _, msg := range batch.Messages {
		body, err := msg.StringBody()
		if err != nil {
			return err
		}
		if err := ns.PutString(queueReceivedKeyPrefix+msg.ID, body, nil); err != nil {
			return err
		}
	}
	batch.AckAll()
	return nil
}

type queueReceivedResponse struct {
	Messages []string `json:"messages"`
}

// handleQueueReceived lists every message the consumer has written to KV
// so far and returns their bodies. Tests poll this until the message they
// sent shows up.
func handleQueueReceived(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result, err := ns.List(&kv.ListOptions{Prefix: queueReceivedKeyPrefix})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages := make([]string, 0, len(result.Keys))
	for _, k := range result.Keys {
		v, err := ns.GetString(k.Name, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if v == jsNullString {
			continue
		}
		messages = append(messages, v)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queueReceivedResponse{Messages: messages}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
