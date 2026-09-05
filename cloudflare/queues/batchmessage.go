package queues

import (
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// MessageSendRequest is a wrapper type used for sending message batches.
// see: https://developers.cloudflare.com/queues/configuration/javascript-apis/#messagesendrequest
type MessageSendRequest struct {
	body    js.Value
	options *sendOptions
}

// NewTextMessageSendRequest creates a single text message to be batched before sending to a queue.
func NewTextMessageSendRequest(content string, opts ...SendOption) *MessageSendRequest {
	return newMessageSendRequest(js.ValueOf(content), contentTypeText, opts...)
}

// NewBytesMessageSendRequest creates a single byte array message to be batched before sending to a queue.
func NewBytesMessageSendRequest(content []byte, opts ...SendOption) *MessageSendRequest {
	return newMessageSendRequest(js.ValueOf(content), contentTypeBytes, opts...)
}

// NewJSONMessageSendRequest creates a single JSON message to be batched before sending to a queue.
func NewJSONMessageSendRequest(content any, opts ...SendOption) *MessageSendRequest {
	return newMessageSendRequest(js.ValueOf(content), contentTypeJSON, opts...)
}

// NewV8MessageSendRequest creates a single raw JS value message to be batched before sending to a queue.
func NewV8MessageSendRequest(content js.Value, opts ...SendOption) *MessageSendRequest {
	return newMessageSendRequest(content, contentTypeV8, opts...)
}

// newMessageSendRequest creates a single message to be batched before sending to a queue.
func newMessageSendRequest(body js.Value, contentType contentType, opts ...SendOption) *MessageSendRequest {
	options := sendOptions{
		ContentType: contentType,
	}
	for _, opt := range opts {
		opt(&options)
	}
	return &MessageSendRequest{body: body, options: &options}
}

func (m *MessageSendRequest) toJS() js.Value {
	obj := jsutil.NewObject()
	obj.Set("body", m.body)
	obj.Set("options", m.options.toJS())
	return obj
}
