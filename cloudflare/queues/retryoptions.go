package queues

import (
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

type retryOptions struct {
	delaySeconds int
}

func (o *retryOptions) toJS() js.Value {
	if o == nil {
		return js.Undefined()
	}

	obj := jsutil.NewObject()
	if o.delaySeconds != 0 {
		obj.Set("delaySeconds", o.delaySeconds)
	}

	return obj
}

type RetryOption func(*retryOptions)

// WithRetryDelay sets the delay in seconds before the messages delivery is retried.
// Note that the delay should not be less than a second and is not more precise than a second.
func WithRetryDelay(d time.Duration) RetryOption {
	return func(o *retryOptions) {
		o.delaySeconds = int(d.Seconds())
	}
}
