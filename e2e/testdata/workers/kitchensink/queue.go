//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/cloudflare/kv"
	"github.com/syumai/workers-go/cloudflare/queues"
)

// queueReceivedKeyPrefix namespaces the KV keys the queue consumer writes
// to, so GET /queue/received can find them with a prefix list without
// colliding with any other KV test data.
const queueReceivedKeyPrefix = "queue:"

// queueMetaKeyPrefix namespaces the JSON receipts GET /queue/meta lists.
// It must not share a prefix with queueReceivedKeyPrefix: that list is
// the raw text body, and a JSON receipt would look like a delivered
// message to the existing round-trip test.
const queueMetaKeyPrefix = "queue-meta:"

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

// queueSendBatchMessage is one element of POST /queue/send-batch.
// Exactly one of Text, JSON, or BytesBase64 is set, matching ContentType.
type queueSendBatchMessage struct {
	ContentType  string          `json:"contentType"`
	DelaySeconds int             `json:"delaySeconds"`
	Text         string          `json:"text"`
	JSON         json.RawMessage `json:"json"`
	BytesBase64  string          `json:"bytesBase64"`
}

// queueSendBatchRequest is the JSON body of POST /queue/send-batch.
// DelaySeconds is the batch-level QueueSendBatchOptions value;
// per-message DelaySeconds and ContentType ride on each element.
type queueSendBatchRequest struct {
	DelaySeconds int                     `json:"delaySeconds"`
	Messages     []queueSendBatchMessage `json:"messages"`
}

// handleQueueSendBatch sends every message in the body with
// Producer.SendBatch. A positive top-level delaySeconds becomes
// WithBatchDelaySeconds; a positive per-message delaySeconds becomes
// WithDelaySeconds. contentType selects the MessageSendRequest constructor.
func handleQueueSendBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req queueSendBatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, "missing messages", http.StatusBadRequest)
		return
	}
	messages := make([]*queues.MessageSendRequest, len(req.Messages))
	for i, m := range req.Messages {
		msg, err := newBatchMessage(m)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messages[i] = msg
	}
	producer, err := queues.NewProducer(queueBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var opts []queues.BatchSendOption
	if req.DelaySeconds > 0 {
		opts = append(opts, queues.WithBatchDelaySeconds(time.Duration(req.DelaySeconds)*time.Second))
	}
	if err := producer.SendBatch(messages, opts...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newBatchMessage(m queueSendBatchMessage) (*queues.MessageSendRequest, error) {
	var opts []queues.SendOption
	if m.DelaySeconds > 0 {
		opts = append(opts, queues.WithDelaySeconds(time.Duration(m.DelaySeconds)*time.Second))
	}
	switch m.ContentType {
	case "text":
		return queues.NewTextMessageSendRequest(m.Text, opts...), nil
	case "json":
		if len(m.JSON) == 0 {
			return nil, fmt.Errorf("json contentType requires a json field")
		}
		var v any
		if err := json.Unmarshal(m.JSON, &v); err != nil {
			return nil, err
		}
		return queues.NewJSONMessageSendRequest(v, opts...), nil
	case "bytes":
		b, err := base64.StdEncoding.DecodeString(m.BytesBase64)
		if err != nil {
			return nil, err
		}
		return queues.NewBytesMessageSendRequest(b, opts...), nil
	default:
		return nil, fmt.Errorf("unknown contentType %q", m.ContentType)
	}
}

// queueReceipt is the JSON value stored for each delivered message and
// returned by GET /queue/meta. BodyKind is what the consumer actually
// observed on the JS message (string, object, or bytes), which is how a
// test checks that SendBatch's contentType survived the runtime.
type queueReceipt struct {
	ID        string `json:"id"`
	BodyKind  string `json:"bodyKind"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp"`
}

// consumeQueue is the QUEUE consumer. Text bodies are also written under
// queueReceivedKeyPrefix so GET /queue/received keeps its plain-string
// contract. Every message, whatever its content type, gets a queueReceipt
// under queueMetaKeyPrefix. State lives in KV because wrangler dev reloads
// the isolate and would drop an in-memory buffer.
func consumeQueue(batch *queues.MessageBatch) error {
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		return err
	}
	for _, msg := range batch.Messages {
		receipt, err := newQueueReceipt(msg)
		if err != nil {
			return err
		}
		if receipt.BodyKind == "string" {
			if err := ns.PutString(queueReceivedKeyPrefix+msg.ID, receipt.Body, nil); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		if err := ns.PutString(queueMetaKeyPrefix+msg.ID, string(raw), nil); err != nil {
			return err
		}
	}
	batch.AckAll()
	return nil
}

func newQueueReceipt(msg *queues.Message) (queueReceipt, error) {
	rec := queueReceipt{
		ID:        msg.ID,
		Timestamp: msg.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	body := msg.Body
	switch body.Type() {
	case js.TypeString:
		rec.BodyKind = "string"
		rec.Body = body.String()
	case js.TypeObject:
		if isBytesBody(body) {
			rec.BodyKind = "bytes"
			rec.Body = base64.StdEncoding.EncodeToString(bytesFromBody(body))
			break
		}
		rec.BodyKind = "object"
		rec.Body = js.Global().Get("JSON").Call("stringify", body).String()
	default:
		return queueReceipt{}, fmt.Errorf("unsupported queue message body type %s", body.Type())
	}
	return rec, nil
}

// isBytesBody reports whether v is a Uint8Array or an ArrayBuffer. Queues
// delivers contentType "bytes" as one of those two; a JSON object must not
// be mistaken for either.
func isBytesBody(v js.Value) bool {
	if v.InstanceOf(js.Global().Get("Uint8Array")) {
		return true
	}
	return v.InstanceOf(js.Global().Get("ArrayBuffer"))
}

func bytesFromBody(v js.Value) []byte {
	u8 := v
	if !v.InstanceOf(js.Global().Get("Uint8Array")) {
		u8 = js.Global().Get("Uint8Array").New(v)
	}
	b := make([]byte, u8.Get("byteLength").Int())
	js.CopyBytesToGo(b, u8)
	return b
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
		if errors.Is(err, kv.ErrNotFound) {
			continue
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		messages = append(messages, v)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queueReceivedResponse{Messages: messages}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type queueMetaResponse struct {
	Messages []queueReceipt `json:"messages"`
}

// handleQueueMeta lists the receipts consumeQueue wrote, including
// non-string bodies that GET /queue/received does not surface.
func handleQueueMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result, err := ns.List(&kv.ListOptions{Prefix: queueMetaKeyPrefix})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages := make([]queueReceipt, 0, len(result.Keys))
	for _, k := range result.Keys {
		v, err := ns.GetString(k.Name, nil)
		if errors.Is(err, kv.ErrNotFound) {
			continue
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var rec queueReceipt
		if err := json.Unmarshal([]byte(v), &rec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		messages = append(messages, rec)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queueMetaResponse{Messages: messages}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
