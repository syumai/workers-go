//go:build js && wasm

package workflows

import (
	"syscall/js"
	"testing"
)

// TestWorkflow_Create exercises a handle-typed binding whose method takes a
// data-type argument (WorkflowInstanceCreateOptions) and resolves to
// another handle type (WorkflowInstance): a fake JS Workflow object whose
// create() returns a resolved Promise wrapping a fake WorkflowInstance,
// wrapped with WorkflowFromJS, called through Create.
func TestWorkflow_Create(t *testing.T) {
	var gotID string
	fakeInstance := js.ValueOf(map[string]any{"id": "instance-1"})
	fake := js.ValueOf(map[string]any{})
	fake.Set("create", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotID = args[0].Get("id").String()
		return js.Global().Get("Promise").Call("resolve", fakeInstance)
	}))

	wf := WorkflowFromJS(fake)
	instance, err := wf.Create(WorkflowInstanceCreateOptions{ID: "my-instance"})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if instance.ID() != "instance-1" {
		t.Errorf("instance.ID() = %q, want %q", instance.ID(), "instance-1")
	}
	if gotID != "my-instance" {
		t.Errorf("options.id sent to JS = %q, want %q", gotID, "my-instance")
	}
}

// TestWorkflowInstance_Status exercises decoding a data type (InstanceStatus)
// off a Promise-returning method.
func TestWorkflowInstance_Status(t *testing.T) {
	fake := js.ValueOf(map[string]any{"id": "instance-1"})
	fake.Set("status", js.FuncOf(func(this js.Value, args []js.Value) any {
		result := js.ValueOf(map[string]any{"status": "running"})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	instance := WorkflowInstanceFromJS(fake)
	status, err := instance.Status()
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("status.Status = %q, want %q", status.Status, "running")
	}
}

// TestWorkflow_DeleteBatch exercises an array-typed method parameter
// (instanceIds []string), which requires cfgen's multi-statement JS
// conversion support.
func TestWorkflow_DeleteBatch(t *testing.T) {
	var gotLen int
	fake := js.ValueOf(map[string]any{})
	fake.Set("deleteBatch", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotLen = args[0].Length()
		result := js.ValueOf(map[string]any{
			"deleted": []any{map[string]any{"id": "a"}},
			"errors":  []any{},
		})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	wf := WorkflowFromJS(fake)
	result, err := wf.DeleteBatch([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("DeleteBatch() failed: %v", err)
	}
	if gotLen != 3 {
		t.Errorf("instanceIds sent to JS had length %d, want 3", gotLen)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("len(result.Deleted) = %d, want 1", len(result.Deleted))
	}
}
