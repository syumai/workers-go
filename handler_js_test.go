//go:build js && wasm

package workers

import (
	"syscall/js"
	"testing"
)

// TestReady_callsImport verifies that Ready() reaches the
// //go:wasmimport workers ready import: the Node test runner
// (testdata/wasm/wasm_exec_node.js) increments context.readyCount each time
// that import is called.
func TestReady_callsImport(t *testing.T) {
	Ready()

	got := js.Global().Get("context").Get("readyCount").Int()
	if got != 1 {
		t.Errorf("context.readyCount = %d, want 1", got)
	}
}
