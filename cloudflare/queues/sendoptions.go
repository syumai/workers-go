package queues

import (
	"time"

	queuesjs "github.com/syumai/workers-go/exp/cloudflare/queues"
)

type sendOptions struct {
	// ContentType - Content type of the message
	// Default is "json"
	ContentType contentType

	// DelaySeconds - The number of seconds to delay the message.
	// Default is 0
	DelaySeconds int
}

func (o *sendOptions) toQueuesJS() queuesjs.QueueSendOptions {
	return queuesjs.QueueSendOptions{
		ContentType:  queuesjs.QueueContentType(o.ContentType),
		DelaySeconds: o.DelaySeconds,
	}
}

type SendOption func(*sendOptions)

// WithDelaySeconds changes the number of seconds to delay the message.
func WithDelaySeconds(d time.Duration) SendOption {
	return func(o *sendOptions) {
		o.DelaySeconds = int(d.Seconds())
	}
}
