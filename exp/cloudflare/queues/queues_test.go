//go:build js && wasm

package queues

import (
	"syscall/js"
	"testing"
)

func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestQueue_SendBatch_PassesArray verifies SendBatch's messages parameter
// (an Iterable<MessageSendRequest<Body>> in the .d.ts, treated as []T per
// item 7) is sent to JS as a real Array, with each element's fields
// encoded.
func TestQueue_SendBatch_PassesArray(t *testing.T) {
	var gotLen int
	var gotBody0, gotContentType0 string
	fake := js.ValueOf(map[string]any{})
	fake.Set("sendBatch", js.FuncOf(func(this js.Value, args []js.Value) any {
		arr := args[0]
		gotLen = arr.Length()
		gotBody0 = arr.Index(0).Get("body").String()
		gotContentType0 = arr.Index(0).Get("contentType").String()
		return resolve(map[string]any{
			"metadata": map[string]any{
				"metrics": map[string]any{"backlogCount": 1, "backlogBytes": 2},
			},
		})
	}))

	q := QueueFromJS(fake)
	messages := []MessageSendRequest{
		{Body: js.ValueOf("hello"), ContentType: QueueContentTypeText},
		{Body: js.ValueOf("world"), ContentType: QueueContentTypeText},
	}
	resp, err := q.SendBatch(messages, QueueSendBatchOptions{})
	if err != nil {
		t.Fatalf("SendBatch() failed: %v", err)
	}
	if gotLen != 2 {
		t.Fatalf("array length sent to JS = %d, want 2", gotLen)
	}
	if gotBody0 != "hello" {
		t.Errorf("messages[0].body sent to JS = %q, want %q", gotBody0, "hello")
	}
	if gotContentType0 != "text" {
		t.Errorf("messages[0].contentType sent to JS = %q, want %q", gotContentType0, "text")
	}
	if resp.Metadata.Metrics.BacklogCount != 1 {
		t.Errorf("resp.Metadata.Metrics.BacklogCount = %v, want 1", resp.Metadata.Metrics.BacklogCount)
	}
}
