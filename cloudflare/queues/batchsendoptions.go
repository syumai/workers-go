package queues

import (
	"time"

	queuesjs "github.com/syumai/workers-go/exp/cloudflare/queues"
)

type batchSendOptions struct {
	// DelaySeconds - The number of seconds to delay the message.
	// Default is 0
	DelaySeconds int
}

func (o *batchSendOptions) toQueuesJS() queuesjs.QueueSendBatchOptions {
	if o == nil {
		return queuesjs.QueueSendBatchOptions{}
	}
	return queuesjs.QueueSendBatchOptions{DelaySeconds: o.DelaySeconds}
}

type BatchSendOption func(*batchSendOptions)

// WithBatchDelaySeconds changes the number of seconds to delay the message.
func WithBatchDelaySeconds(d time.Duration) BatchSendOption {
	return func(o *batchSendOptions) {
		o.DelaySeconds = int(d.Seconds())
	}
}
