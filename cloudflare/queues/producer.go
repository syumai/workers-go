package queues

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

type Producer struct {
	// queue - Objects that Queue API belongs to. Default is Global
	queue js.Value
}

// NewProducer creates a new Producer object to send messages to a queue.
// queueName is the name of the queue environment var to send messages to.
// In Cloudflare API documentation, this object represents the Queue.
//   - https://developers.cloudflare.com/queues/configuration/javascript-apis/#producer
func NewProducer(queueName string) (*Producer, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(queueName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", queueName)
	}
	return &Producer{queue: inst}, nil
}

// SendText sends a single text message to a queue.
func (p *Producer) SendText(body string, opts ...SendOption) error {
	return p.send(js.ValueOf(body), contentTypeText, opts...)
}

// SendBytes sends a single byte array message to a queue.
func (p *Producer) SendBytes(body []byte, opts ...SendOption) error {
	ua := jsutil.BytesToJS(body)
	// accortind to docs, "bytes" type requires an ArrayBuffer to be sent, however practical experience shows that ArrayBufferView should
	// be used instead and with Uint8Array.buffer as a value, the send simply fails
	return p.send(ua, contentTypeBytes, opts...)
}

// SendJSON sends a single JSON message to a queue.
func (p *Producer) SendJSON(body any, opts ...SendOption) error {
	return p.send(js.ValueOf(body), contentTypeJSON, opts...)
}

// SendV8 sends a single raw JS value message to a queue.
func (p *Producer) SendV8(body js.Value, opts ...SendOption) error {
	return p.send(body, contentTypeV8, opts...)
}

// send sends a single message to a queue.
// This function allows setting send options for the message.
//   - https://developers.cloudflare.com/queues/configuration/javascript-apis/#producer
//   - https://developers.cloudflare.com/queues/configuration/javascript-apis/#queuesendoptions
func (p *Producer) send(body js.Value, contentType contentType, opts ...SendOption) error {
	options := sendOptions{
		ContentType: contentType,
	}
	for _, opt := range opts {
		opt(&options)
	}

	prom := p.queue.Call("send", body, options.toJS())
	_, err := jsutil.AwaitPromise(prom)
	return err
}

// SendBatch sends multiple messages to a queue. This function allows setting options for each message.
func (p *Producer) SendBatch(messages []*MessageSendRequest, opts ...BatchSendOption) error {
	var options batchSendOptions
	for _, opt := range opts {
		opt(&options)
	}

	jsArray := jsutil.NewArray(len(messages))
	for i, message := range messages {
		jsArray.SetIndex(i, message.toJS())
	}

	prom := p.queue.Call("sendBatch", jsArray, options.toJS())
	_, err := jsutil.AwaitPromise(prom)
	return err
}
