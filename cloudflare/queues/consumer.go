package queues

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// Consumer is a function that received a batch of messages from Cloudflare Queues.
// The function should be set using Consume or ConsumeNonBlock.
// A returned error will cause the batch to be retried (unless the batch or individual messages are acked).
// NOTE: to do long-running message processing task within the Consumer, use cloudflare.WaitUntil, this will postpone the message
// acknowledgment until the task is completed witout blocking the queue consumption.
type Consumer func(batch *MessageBatch) error

var consumer Consumer

func init() {
	jsutil.RegisterAsyncHandler("handleQueueMessageBatch", 1, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), consumeBatch(args[0])
	})
}

func consumeBatch(batch js.Value) error {
	b, err := newMessageBatch(batch)
	if err != nil {
		return fmt.Errorf("failed to parse message batch: %v", err)
	}

	if err := consumer(b); err != nil {
		return err
	}
	return nil
}

//go:wasmimport workers ready
func ready()

// Consume sets the Consumer function to receive batches of messages from Cloudflare Queues
// NOTE: This function will block the current goroutine and is intented to be used as long as the
// only worker's purpose is to be the consumer of a Cloudflare Queue.
// In case the worker has other purposes (e.g. handling HTTP requests), use ConsumeNonBlock instead.
func Consume(f Consumer) {
	consumer = f
	ready()
	select {}
}

// ConsumeNonBlock sets the Consumer function to receive batches of messages from Cloudflare Queues.
// This function is intented to be used when the worker has other purposes (e.g. handling HTTP requests).
// The worker will not block receiving messages and will continue to execute other tasks.
// ConsumeNonBlock should be called before setting other blocking handlers (e.g. workers.Serve).
func ConsumeNonBlock(f Consumer) {
	consumer = f
}
