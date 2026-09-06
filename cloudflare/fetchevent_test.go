//go:build js && wasm

package cloudflare

import (
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestWaitUntil(t *testing.T) {
	ec := jstest.NewExecutionContext(t)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: ec.Value()})

	var ran bool
	WaitUntil(func() { ran = true })
	ec.Wait(t)

	if !ran {
		t.Error("task passed to WaitUntil was not run")
	}
}

func TestPassThroughOnException(t *testing.T) {
	ec := jstest.NewExecutionContext(t)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: ec.Value()})

	PassThroughOnException()

	if got, want := ec.PassThroughCalls(), 1; got != want {
		t.Errorf("PassThroughCalls() = %d, want %d", got, want)
	}
}
