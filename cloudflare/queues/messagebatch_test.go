package queues

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestNewConsumerMessageBatch(t *testing.T) {
	ts := time.Now()
	jsTs := jsutil.TimeToDate(ts)
	id := "some-message-id"
	m := map[string]any{
		"queue": "some-queue",
		"messages": []any{
			map[string]any{
				"body":      "hello",
				"timestamp": jsTs,
				"id":        id,
				"attempts":  1,
			},
		},
	}

	got, err := newMessageBatch(js.ValueOf(m))
	if err != nil {
		t.Fatalf("newMessageBatch failed: %v", err)
	}

	if got.Queue != "some-queue" {
		t.Fatalf("Queue = %v, want %v", got.Queue, "some-queue")
	}

	if len(got.Messages) != 1 {
		t.Fatalf("Messages = %v, want %v", len(got.Messages), 1)
	}

	msg := got.Messages[0]
	if body := msg.Body.String(); body != "hello" {
		t.Fatalf("Body() = %v, want %v", body, "hello")
	}

	if msg.ID != id {
		t.Fatalf("ID = %v, want %v", msg.ID, id)
	}

	if msg.Attempts != 1 {
		t.Fatalf("Attempts = %v, want %v", msg.Attempts, 1)
	}

	if msg.Timestamp.UnixMilli() != ts.UnixMilli() {
		t.Fatalf("Timestamp = %v, want %v", msg.Timestamp, ts)
	}
}

func TestConsumerMessageBatch_AckAll(t *testing.T) {
	ackAllCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("ackAll", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ackAllCalled = true
		return nil
	}))
	b := &MessageBatch{
		instance: jsObj,
	}

	b.AckAll()

	if !ackAllCalled {
		t.Fatalf("AckAll() did not call ackAll")
	}
}

func TestConsumerMessageBatch_RetryAll(t *testing.T) {
	retryAllCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("retryAll", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		retryAllCalled = true
		return nil
	}))
	b := &MessageBatch{
		instance: jsObj,
	}

	b.RetryAll()

	if !retryAllCalled {
		t.Fatalf("RetryAll() did not call retryAll")
	}
}

func TestConsumerMessageBatch_RetryAllWithRetryOption(t *testing.T) {
	retryAllCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("retryAll", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		retryAllCalled = true
		if len(args) != 1 {
			t.Fatalf("retryAll() called with %d arguments, want 1", len(args))
		}

		opts := args[0]
		if opts.Type() != js.TypeObject {
			t.Fatalf("retryAll() called with argument of type %v, want object", opts.Type())
		}

		if delay := opts.Get("delaySeconds").Int(); delay != 10 {
			t.Fatalf("delaySeconds = %v, want %v", delay, 10)
		}

		return nil
	}))

	b := &MessageBatch{
		instance: jsObj,
	}

	b.RetryAll(WithRetryDelay(10 * time.Second))

	if !retryAllCalled {
		t.Fatalf("RetryAll() did not call retryAll")
	}
}
