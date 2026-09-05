package queues

import (
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

type batchSendOptions struct {
	// DelaySeconds - The number of seconds to delay the message.
	// Default is 0
	DelaySeconds int
}

func (o *batchSendOptions) toJS() js.Value {
	if o == nil {
		return js.Undefined()
	}

	obj := jsutil.NewObject()
	if o.DelaySeconds != 0 {
		obj.Set("delaySeconds", o.DelaySeconds)
	}

	return obj
}

type BatchSendOption func(*batchSendOptions)

// WithBatchDelaySeconds changes the number of seconds to delay the message.
func WithBatchDelaySeconds(d time.Duration) BatchSendOption {
	return func(o *batchSendOptions) {
		o.DelaySeconds = int(d.Seconds())
	}
}
