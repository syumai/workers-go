package queues

import (
	"errors"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// awaitRejected waits for p to settle and fails the test if it does not
// settle within 5 seconds or if it resolves instead of rejecting. It plays
// the same role as jstest.Await, but for the rejection path, which jstest
// does not expose (jstest.Await always calls t.Fatalf on rejection). It
// must only be called from outside of a js.FuncOf callback, for the same
// reasons documented on jstest.Await.
func awaitRejected(t testing.TB, p js.Value) error {
	t.Helper()
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := jsutil.AwaitPromise(p)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		if r.err == nil {
			t.Fatalf("promise resolved with %v, want it to reject", r.v)
		}
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out after 5s waiting for promise to reject")
		return nil
	}
}

// fakeQueueMessage is a JS object shaped like the Message the Cloudflare
// Queues runtime passes to a consumer (see newMessage in message.go: id,
// timestamp, body, attempts, ack, retry), with ack/retry calls recorded so
// tests can assert on them.
type fakeQueueMessage struct {
	value js.Value

	ackFn   js.Func
	retryFn js.Func

	ackCalls   int
	retryCalls []js.Value // the options object passed to each retry() call
}

func newFakeQueueMessage(t testing.TB, id string, body string, ts time.Time, attempts int) *fakeQueueMessage {
	t.Helper()
	fm := &fakeQueueMessage{}

	fm.ackFn = jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		fm.ackCalls++
		return js.Undefined()
	})
	fm.retryFn = jstest.Func(t, func(_ js.Value, args []js.Value) any {
		var opt js.Value
		if len(args) > 0 {
			opt = args[0]
		}
		fm.retryCalls = append(fm.retryCalls, opt)
		return js.Undefined()
	})

	v := jsutil.NewObject()
	v.Set("id", id)
	v.Set("timestamp", jsutil.TimeToDate(ts))
	v.Set("body", body)
	v.Set("attempts", attempts)
	v.Set("ack", fm.ackFn)
	v.Set("retry", fm.retryFn)
	fm.value = v

	return fm
}

func fakeQueueMessageBatch(queue string, messages ...*fakeQueueMessage) js.Value {
	batchObj := jsutil.NewObject()
	batchObj.Set("queue", queue)
	arr := jsutil.NewArray(len(messages))
	for i, m := range messages {
		arr.SetIndex(i, m.value)
	}
	batchObj.Set("messages", arr)
	return batchObj
}

// TestHandleQueueMessageBatch_callsConsumer verifies that
// handleQueueMessageBatch (registered on jsutil.Binding as
// "handleQueueMessageBatch" by this package's init) invokes the Consumer
// set via ConsumeNonBlock with a *MessageBatch whose Queue and Messages
// reflect the JS-side MessageBatch object passed in.
func TestHandleQueueMessageBatch_callsConsumer(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	msg1 := newFakeQueueMessage(t, "id-1", "hello", ts, 0)
	msg2 := newFakeQueueMessage(t, "id-2", "world", ts, 1)
	batchObj := fakeQueueMessageBatch("my-queue", msg1, msg2)

	var gotBatch *MessageBatch
	ConsumeNonBlock(func(b *MessageBatch) error {
		gotBatch = b
		return nil
	})
	t.Cleanup(func() { consumer = nil })

	p := jstest.Binding(t, "handleQueueMessageBatch").Invoke(batchObj)
	jstest.Await(t, p)

	if gotBatch == nil {
		t.Fatalf("consumer was not called")
	}
	if gotBatch.Queue != "my-queue" {
		t.Errorf("Queue = %q, want %q", gotBatch.Queue, "my-queue")
	}
	if len(gotBatch.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(gotBatch.Messages))
	}

	if got := gotBatch.Messages[0].ID; got != "id-1" {
		t.Errorf("Messages[0].ID = %q, want %q", got, "id-1")
	}
	if got, err := gotBatch.Messages[0].StringBody(); err != nil || got != "hello" {
		t.Errorf("Messages[0].StringBody() = (%q, %v), want (%q, nil)", got, err, "hello")
	}
	if got := gotBatch.Messages[1].ID; got != "id-2" {
		t.Errorf("Messages[1].ID = %q, want %q", got, "id-2")
	}
	if got, err := gotBatch.Messages[1].StringBody(); err != nil || got != "world" {
		t.Errorf("Messages[1].StringBody() = (%q, %v), want (%q, nil)", got, err, "world")
	}
	if got := gotBatch.Messages[1].Attempts; got != 1 {
		t.Errorf("Messages[1].Attempts = %d, want 1", got)
	}
}

// TestHandleQueueMessageBatch_consumerError verifies that an error returned
// by the Consumer rejects the Promise returned by handleQueueMessageBatch,
// with the error message carried through.
func TestHandleQueueMessageBatch_consumerError(t *testing.T) {
	batchObj := fakeQueueMessageBatch("q")

	wantErr := errors.New("consumer failed")
	ConsumeNonBlock(func(b *MessageBatch) error {
		return wantErr
	})
	t.Cleanup(func() { consumer = nil })

	p := jstest.Binding(t, "handleQueueMessageBatch").Invoke(batchObj)
	err := awaitRejected(t, p)
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Errorf("error = %q, want it to contain %q", err, wantErr.Error())
	}
}

// TestHandleQueueMessageBatch_ackRetryReachJS verifies that calling Ack /
// Retry on a *Message received by the Consumer reaches the underlying JS
// message object's ack()/retry() methods, including the retry options.
func TestHandleQueueMessageBatch_ackRetryReachJS(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	msg0 := newFakeQueueMessage(t, "id-0", "a", ts, 0)
	msg1 := newFakeQueueMessage(t, "id-1", "b", ts, 0)
	batchObj := fakeQueueMessageBatch("q", msg0, msg1)

	ConsumeNonBlock(func(b *MessageBatch) error {
		b.Messages[0].Ack()
		b.Messages[1].Retry(WithRetryDelay(3 * time.Second))
		return nil
	})
	t.Cleanup(func() { consumer = nil })

	p := jstest.Binding(t, "handleQueueMessageBatch").Invoke(batchObj)
	jstest.Await(t, p)

	if msg0.ackCalls != 1 {
		t.Errorf("ack called %d times, want 1", msg0.ackCalls)
	}
	if len(msg1.retryCalls) != 1 {
		t.Fatalf("retry called %d times, want 1", len(msg1.retryCalls))
	}
	if got := msg1.retryCalls[0].Get("delaySeconds").Int(); got != 3 {
		t.Errorf("retry's delaySeconds = %d, want 3", got)
	}
}

// TestConsume_readyAndBlocks verifies that Consume calls the //go:wasmimport
// workers ready import and then blocks forever (`select {}`). It runs
// Consume in a goroutine that is intentionally never unblocked - that
// goroutine leaks for the rest of the test binary's process, which is fine
// since the process exits once the package's tests finish.
func TestConsume_readyAndBlocks(t *testing.T) {
	before := jstest.ReadyCount(t)
	go func() {
		Consume(func(b *MessageBatch) error { return nil })
	}()

	deadline := time.Now().Add(5 * time.Second)
	for jstest.ReadyCount(t)-before != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("Consume did not call the ready import within 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
