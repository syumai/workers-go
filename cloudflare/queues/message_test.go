package queues

import (
	"bytes"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestNewConsumerMessage(t *testing.T) {
	ts := time.Now()
	jsTs := jsutil.TimeToDate(ts)
	id := "some-message-id"
	m := map[string]any{
		"body":      "hello",
		"timestamp": jsTs,
		"id":        id,
		"attempts":  1,
	}

	got, err := newMessage(js.ValueOf(m))
	if err != nil {
		t.Fatalf("newMessage failed: %v", err)
	}

	if body := got.Body.String(); body != "hello" {
		t.Fatalf("Body() = %v, want %v", body, "hello")
	}

	if got.ID != id {
		t.Fatalf("ID = %v, want %v", got.ID, id)
	}

	if got.Attempts != 1 {
		t.Fatalf("Attempts = %v, want %v", got.Attempts, 1)
	}

	if got.Timestamp.UnixMilli() != ts.UnixMilli() {
		t.Fatalf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
}

func TestConsumerMessage_Ack(t *testing.T) {
	ackCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("ack", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ackCalled = true
		return nil
	}))
	m := &Message{
		instance: jsObj,
	}

	m.Ack()

	if !ackCalled {
		t.Fatalf("Ack() did not call ack")
	}
}

func TestConsumerMessage_Retry(t *testing.T) {
	retryCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("retry", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		retryCalled = true
		return nil
	}))
	m := &Message{
		instance: jsObj,
	}

	m.Retry()

	if !retryCalled {
		t.Fatalf("Retry() did not call retry")
	}
}

func TestConsumerMessage_RetryWithDelay(t *testing.T) {
	retryCalled := false
	jsObj := jsutil.NewObject()
	jsObj.Set("retry", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		retryCalled = true
		if len(args) != 1 {
			t.Fatalf("retry() called with %d arguments, want 1", len(args))
		}

		opts := args[0]
		if opts.Type() != js.TypeObject {
			t.Fatalf("retry() called with argument of type %v, want object", opts.Type())
		}

		if delay := opts.Get("delaySeconds").Int(); delay != 10 {
			t.Fatalf("delaySeconds = %v, want %v", delay, 10)
		}

		return nil
	}))

	m := &Message{
		instance: jsObj,
	}

	m.Retry(WithRetryDelay(10 * time.Second))

	if !retryCalled {
		t.Fatalf("RetryAll() did not call retryAll")
	}
}

func TestNewConsumerMessage_StringBody(t *testing.T) {
	tests := []struct {
		name    string
		body    func() js.Value
		want    string
		wantErr bool
	}{
		{
			name: "string",
			body: func() js.Value {
				return js.ValueOf("hello")
			},
			want: "hello",
		},
		{
			name: "uint8 array",
			body: func() js.Value {
				v := jsutil.Uint8ArrayClass.New(3)
				js.CopyBytesToJS(v, []byte("foo"))
				return v
			},
			wantErr: true,
		},
		{
			name: "int",
			body: func() js.Value {
				return js.ValueOf(42)
			},
			wantErr: true,
		},
		{
			name: "undefined",
			body: func() js.Value {
				return js.Undefined()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Message{
				Body: tt.body(),
			}

			got, err := m.StringBody()
			if (err != nil) != tt.wantErr {
				t.Fatalf("StringBody() error = %v, wantErr %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Fatalf("StringBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConsumerMessage_BytesBody(t *testing.T) {
	tests := []struct {
		name    string
		body    func() js.Value
		want    []byte
		wantErr bool
	}{
		{
			name: "uint8 array",
			body: func() js.Value {
				v := jsutil.Uint8ArrayClass.New(3)
				js.CopyBytesToJS(v, []byte("foo"))
				return v
			},
			want: []byte("foo"),
		},
		{
			name: "uint8 clamped array",
			body: func() js.Value {
				v := jsutil.Uint8ClampedArrayClass.New(3)
				js.CopyBytesToJS(v, []byte("bar"))
				return v
			},
			want: []byte("bar"),
		},
		{
			name: "incorrect type",
			body: func() js.Value {
				return js.ValueOf("hello")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Message{
				Body: tt.body(),
			}

			got, err := m.BytesBody()
			if (err != nil) != tt.wantErr {
				t.Fatalf("BytesBody() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !bytes.Equal(got, tt.want) {
				t.Fatalf("BytesBody() = %v, want %v", got, tt.want)
			}
		})
	}
}
