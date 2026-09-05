package queues

import (
	"fmt"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// Message represents a message of the batch received by the consumer.
//   - https://developers.cloudflare.com/queues/configuration/javascript-apis/#message
type Message struct {
	// instance - The underlying instance of the JS message object passed by the cloudflare
	instance js.Value

	// ID - The unique Cloudflare-generated identifier of the message
	ID string
	// Timestamp - The time when the message was enqueued
	Timestamp time.Time
	// Body - The message body. Could be accessed directly or using converting helpers as StringBody, BytesBody, IntBody, FloatBody.
	Body js.Value
	// Attempts - The number of times the message delivery has been retried.
	Attempts int
}

func newMessage(obj js.Value) (*Message, error) {
	timestamp, err := jsutil.DateToTime(obj.Get("timestamp"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse message timestamp: %v", err)
	}

	return &Message{
		instance:  obj,
		ID:        obj.Get("id").String(),
		Body:      obj.Get("body"),
		Attempts:  obj.Get("attempts").Int(),
		Timestamp: timestamp,
	}, nil
}

// Ack acknowledges the message as successfully delivered despite the result returned from the consuming function.
//   - https://developers.cloudflare.com/queues/configuration/javascript-apis/#message
func (m *Message) Ack() {
	m.instance.Call("ack")
}

// Retry marks the message to be re-delivered.
// The message will be retried after the optional delay configured with RetryOption.
func (m *Message) Retry(opts ...RetryOption) {
	var o *retryOptions
	if len(opts) > 0 {
		o = &retryOptions{}
		for _, opt := range opts {
			opt(o)
		}
	}

	m.instance.Call("retry", o.toJS())
}

func (m *Message) StringBody() (string, error) {
	if m.Body.Type() != js.TypeString {
		return "", fmt.Errorf("message body is not a string: %v", m.Body)
	}
	return m.Body.String(), nil
}

func (m *Message) BytesBody() ([]byte, error) {
	if m.Body.Type() != js.TypeObject ||
		!(m.Body.InstanceOf(jsutil.Uint8ArrayClass) || m.Body.InstanceOf(jsutil.Uint8ClampedArrayClass)) {
		return nil, fmt.Errorf("message body is not a byte array: %v", m.Body)
	}
	b := make([]byte, m.Body.Get("byteLength").Int())
	js.CopyBytesToGo(b, m.Body)
	return b, nil
}
