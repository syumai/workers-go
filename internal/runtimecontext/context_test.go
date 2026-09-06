//go:build js && wasm

package runtimecontext

import (
	"context"
	"syscall/js"
	"testing"
)

func TestNew_extract(t *testing.T) {
	trigger := js.ValueOf(map[string]any{"k": 1})

	ctx := New(context.Background(), trigger)

	got := MustExtractTriggerObj(ctx)
	if !got.Equal(trigger) {
		t.Errorf("MustExtractTriggerObj() = %v, want %v", got, trigger)
	}
}

func TestMustExtractTriggerObj_missing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustExtractTriggerObj() did not panic on a context without a trigger object")
		}
	}()

	MustExtractTriggerObj(context.Background())
	t.Errorf("MustExtractTriggerObj() returned normally, want panic")
}

type parentValueKey struct{}

func TestNew_preservesParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), parentValueKey{}, "parent-value"))
	defer cancel()

	ctx := New(parent, js.ValueOf(map[string]any{}))

	if got := ctx.Value(parentValueKey{}); got != "parent-value" {
		t.Errorf("ctx.Value(parentValueKey{}) = %v, want %q", got, "parent-value")
	}

	select {
	case <-ctx.Done():
		t.Errorf("ctx.Done() is already closed before the parent was canceled")
	default:
	}

	cancel()

	select {
	case <-ctx.Done():
	default:
		t.Errorf("ctx.Done() is not closed after the parent was canceled")
	}
	if err := ctx.Err(); err != context.Canceled {
		t.Errorf("ctx.Err() = %v, want %v", err, context.Canceled)
	}
}
