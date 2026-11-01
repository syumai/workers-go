//go:build js && wasm

package workflows

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// resetRunners clears the package-level Register state around a test, so
// tests don't leak into each other (mirroring
// durableobjects/host_test.go's resetInstance).
func resetRunners(t *testing.T) {
	t.Helper()
	runnersMu.Lock()
	prev := runners
	runners = map[string]Runner{}
	runnersMu.Unlock()
	t.Cleanup(func() {
		runnersMu.Lock()
		runners = prev
		runnersMu.Unlock()
	})
}

// withRuntimeContext installs a fake jsutil.RuntimeContext of the shape
// worker.mjs's GoWorkflowEntrypoint#bind builds for a Workflow run()
// trigger (workflow: {className}, plus an optional NonRetryableError
// class), restoring the previous one via t.Cleanup — mirroring
// durableobjects/host_test.go's withRuntimeContext.
func withRuntimeContext(t *testing.T, className string, nonRetryableErrorClass js.Value) {
	t.Helper()
	prev := jsutil.RuntimeContext
	rc := jsrt.NewObject()
	wf := jsrt.NewObject()
	wf.Set("className", className)
	rc.Set("workflow", wf)
	rc.Set("NonRetryableError", nonRetryableErrorClass)
	jsutil.RuntimeContext = rc
	t.Cleanup(func() { jsutil.RuntimeContext = prev })
}

// fakeStep builds a JS WorkflowStep good enough to exercise Step's methods:
// do(name[, config], cb) invokes cb (ignoring config) and awaits its
// Promise on the first call for a given step name, caching the resolved
// value; every later call for that same name returns the cached value
// instead of invoking cb again, mirroring how Workflows replays a
// completed step. sleep/sleepUntil/waitForEvent just record their
// arguments and resolve.
type fakeStep struct {
	v js.Value

	doCalls          map[string]int
	doConfigs        []js.Value
	sleepCalls       []sleepCall
	sleepUntilCalls  []sleepUntilCall
	waitForEventOpts []js.Value

	results map[string]js.Value
}

type sleepCall struct {
	name string
	arg  js.Value
}

type sleepUntilCall struct {
	name string
	arg  js.Value
}

func newFakeStep() *fakeStep {
	fs := &fakeStep{
		v:       jsrt.NewObject(),
		doCalls: map[string]int{},
		results: map[string]js.Value{},
	}
	fs.v.Set("do", js.FuncOf(func(_ js.Value, args []js.Value) any {
		name := args[0].String()
		cb := args[len(args)-1]
		if len(args) == 3 {
			fs.doConfigs = append(fs.doConfigs, args[1])
		}
		fs.doCalls[name]++
		if v, ok := fs.results[name]; ok {
			return jsutil.PromiseClass.Call("resolve", v)
		}
		p := cb.Invoke()
		return p.Call("then", js.FuncOf(func(_ js.Value, targs []js.Value) any {
			fs.results[name] = targs[0]
			return targs[0]
		}))
	}))
	fs.v.Set("sleep", js.FuncOf(func(_ js.Value, args []js.Value) any {
		fs.sleepCalls = append(fs.sleepCalls, sleepCall{name: args[0].String(), arg: args[1]})
		return jsutil.PromiseClass.Call("resolve", js.Undefined())
	}))
	fs.v.Set("sleepUntil", js.FuncOf(func(_ js.Value, args []js.Value) any {
		fs.sleepUntilCalls = append(fs.sleepUntilCalls, sleepUntilCall{name: args[0].String(), arg: args[1]})
		return jsutil.PromiseClass.Call("resolve", js.Undefined())
	}))
	fs.v.Set("waitForEvent", js.FuncOf(func(_ js.Value, args []js.Value) any {
		fs.waitForEventOpts = append(fs.waitForEventOpts, args[1])
		result := js.ValueOf(map[string]any{
			"payload":   "event-payload",
			"timestamp": jsrt.TimeToDate(time.Unix(0, 0)),
			"type":      args[1].Get("type").String(),
		})
		return jsutil.PromiseClass.Call("resolve", result)
	}))
	return fs
}

// TestStep_Do_CallsCallbackOnceThenReplays verifies that Do invokes fn on
// the first call for a step name, but a second Do for the same name
// returns the fake's cached (i.e. "already completed") result without
// calling fn again.
func TestStep_Do_CallsCallbackOnceThenReplays(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	var calls int
	fn := func(ctx context.Context) (js.Value, error) {
		calls++
		return js.ValueOf("result"), nil
	}

	r1, err := step.Do("step-1", fn)
	if err != nil {
		t.Fatalf("Do() (1st call) failed: %v", err)
	}
	r2, err := step.Do("step-1", fn)
	if err != nil {
		t.Fatalf("Do() (2nd call) failed: %v", err)
	}

	if calls != 1 {
		t.Errorf("fn called %d times, want 1 (2nd Do should replay the saved result)", calls)
	}
	if r1.String() != "result" || r2.String() != "result" {
		t.Errorf("Do() results = (%q, %q), want (\"result\", \"result\")", r1.String(), r2.String())
	}
	if fs.doCalls["step-1"] != 2 {
		t.Errorf("fake step.do called %d times for \"step-1\", want 2", fs.doCalls["step-1"])
	}
}

// TestStep_Do_PropagatesError verifies that fn's error rejects the
// Promise WorkflowStep.do receives, which Do surfaces as a Go error.
func TestStep_Do_PropagatesError(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	wantErr := errors.New("step failed")
	_, err := step.Do("step-1", func(ctx context.Context) (js.Value, error) {
		return js.Value{}, wantErr
	})
	if err == nil {
		t.Fatal("Do() succeeded, want an error")
	}
}

// TestStep_DoWithConfig_PassesConfig verifies DoWithConfig sends the
// config object as WorkflowStep.do's 2nd argument, between name and the
// callback.
func TestStep_DoWithConfig_PassesConfig(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	cfg := StepConfig{
		Retries: &RetryConfig{Limit: 3, Delay: 5 * time.Second, Backoff: BackoffExponential},
		Timeout: 10 * time.Second,
	}
	_, err := step.DoWithConfig("step-1", cfg, func(ctx context.Context) (js.Value, error) {
		return js.ValueOf("ok"), nil
	})
	if err != nil {
		t.Fatalf("DoWithConfig() failed: %v", err)
	}
	if len(fs.doConfigs) != 1 {
		t.Fatalf("fake step.do got %d configs, want 1", len(fs.doConfigs))
	}
	got := fs.doConfigs[0]
	retries := got.Get("retries")
	if retries.Get("limit").Int() != 3 {
		t.Errorf("retries.limit = %v, want 3", retries.Get("limit").Int())
	}
	if retries.Get("delay").Float() != 5000 {
		t.Errorf("retries.delay = %v, want 5000", retries.Get("delay").Float())
	}
	if retries.Get("backoff").String() != "exponential" {
		t.Errorf("retries.backoff = %q, want \"exponential\"", retries.Get("backoff").String())
	}
	if got.Get("timeout").Float() != 10000 {
		t.Errorf("timeout = %v, want 10000", got.Get("timeout").Float())
	}
}

// TestDoJSON_RoundTripsTypedValue verifies DoJSON encodes fn's typed
// result to a structured-clonable JS value and decodes WorkflowStep.do's
// result (the same value, echoed back by fakeStep) back into a Go value.
func TestDoJSON_RoundTripsTypedValue(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	got, err := DoJSON(step, "step-1", func(ctx context.Context) (payload, error) {
		return payload{Name: "a", Count: 2}, nil
	})
	if err != nil {
		t.Fatalf("DoJSON() failed: %v", err)
	}
	if got.Name != "a" || got.Count != 2 {
		t.Errorf("DoJSON() = %+v, want {Name:a Count:2}", got)
	}

	// A 2nd call replays the fake's cached (JSON) result via the same
	// decode path.
	got2, err := DoJSON(step, "step-1", func(ctx context.Context) (payload, error) {
		t.Fatal("fn called on replay, want the cached result to be reused")
		return payload{}, nil
	})
	if err != nil {
		t.Fatalf("DoJSON() (2nd call) failed: %v", err)
	}
	if got2 != got {
		t.Errorf("DoJSON() (2nd call) = %+v, want %+v", got2, got)
	}
}

// TestStep_Sleep_SendsMilliseconds verifies Sleep converts d to the number
// (ms) branch of WorkflowSleepDuration.
func TestStep_Sleep_SendsMilliseconds(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	if err := step.Sleep("wait", 1500*time.Millisecond); err != nil {
		t.Fatalf("Sleep() failed: %v", err)
	}
	if len(fs.sleepCalls) != 1 {
		t.Fatalf("fake step.sleep called %d times, want 1", len(fs.sleepCalls))
	}
	call := fs.sleepCalls[0]
	if call.name != "wait" {
		t.Errorf("sleep name = %q, want \"wait\"", call.name)
	}
	if call.arg.Float() != 1500 {
		t.Errorf("sleep duration = %v, want 1500", call.arg.Float())
	}
}

// TestStep_WaitForEvent_DecodesResult verifies WaitForEvent sends
// {type, timeout} and decodes the resolved WorkflowStepEvent.
func TestStep_WaitForEvent_DecodesResult(t *testing.T) {
	fs := newFakeStep()
	step := &Step{v: fs.v}

	got, err := step.WaitForEvent("wait-for-approval", "approved", 30*time.Second)
	if err != nil {
		t.Fatalf("WaitForEvent() failed: %v", err)
	}
	if got.Type != "approved" {
		t.Errorf("StepEvent.Type = %q, want \"approved\"", got.Type)
	}
	if got.Payload.String() != "event-payload" {
		t.Errorf("StepEvent.Payload = %q, want \"event-payload\"", got.Payload.String())
	}
	if len(fs.waitForEventOpts) != 1 {
		t.Fatalf("fake step.waitForEvent called %d times, want 1", len(fs.waitForEventOpts))
	}
	opts := fs.waitForEventOpts[0]
	if opts.Get("type").String() != "approved" {
		t.Errorf("options.type = %q, want \"approved\"", opts.Get("type").String())
	}
	if opts.Get("timeout").Float() != 30000 {
		t.Errorf("options.timeout = %v, want 30000", opts.Get("timeout").Float())
	}
}

// fakeEvent builds a JS WorkflowEvent value.
func fakeEvent(payload js.Value, instanceID, workflowName string) js.Value {
	return js.ValueOf(map[string]any{
		"payload":      payload,
		"timestamp":    jsrt.TimeToDate(time.Unix(1700000000, 0)),
		"instanceId":   instanceID,
		"workflowName": workflowName,
	})
}

// TestRegister_HandleWorkflowRun_Dispatches verifies that
// handleWorkflowRun (registered on jsutil.Binding by init()) looks up
// workflow.className in the runtime context, dispatches to the matching
// Register-ed Runner with a decoded *Event and *Step, and resolves with
// the Runner's returned js.Value.
func TestRegister_HandleWorkflowRun_Dispatches(t *testing.T) {
	resetRunners(t)
	withRuntimeContext(t, "MyWorkflow", js.Undefined())

	var gotEvent *Event
	var gotStepV js.Value
	Register("MyWorkflow", func(ctx context.Context, event *Event, step *Step) (js.Value, error) {
		gotEvent = event
		gotStepV = step.v
		return js.ValueOf("done"), nil
	})

	handle := jsutil.Binding.Get("handleWorkflowRun")
	if handle.IsUndefined() {
		t.Fatal("init() did not register \"handleWorkflowRun\" on jsutil.Binding")
	}

	fs := newFakeStep()
	eventVal := fakeEvent(js.ValueOf("payload-value"), "instance-1", "MyWorkflow")
	result, err := jsrt.Await(handle.Invoke(eventVal, fs.v))
	if err != nil {
		t.Fatalf("handleWorkflowRun rejected: %v", err)
	}
	if result.String() != "done" {
		t.Errorf("result = %q, want \"done\"", result.String())
	}
	if gotEvent == nil {
		t.Fatal("Runner was not called")
	}
	if gotEvent.InstanceID != "instance-1" || gotEvent.WorkflowName != "MyWorkflow" {
		t.Errorf("event = {InstanceID:%q WorkflowName:%q}, want {InstanceID:\"instance-1\" WorkflowName:\"MyWorkflow\"}", gotEvent.InstanceID, gotEvent.WorkflowName)
	}
	if gotEvent.Payload.String() != "payload-value" {
		t.Errorf("event.Payload = %q, want \"payload-value\"", gotEvent.Payload.String())
	}
	if !gotStepV.Equal(fs.v) {
		t.Error("Runner's *Step did not wrap the JS step value passed to handleWorkflowRun")
	}
}

// TestRegister_HandleWorkflowRun_UnregisteredClassName verifies that a
// className with no Register-ed Runner rejects the Promise.
func TestRegister_HandleWorkflowRun_UnregisteredClassName(t *testing.T) {
	resetRunners(t)
	withRuntimeContext(t, "NotRegistered", js.Undefined())

	handle := jsutil.Binding.Get("handleWorkflowRun")
	fs := newFakeStep()
	eventVal := fakeEvent(js.Undefined(), "instance-1", "NotRegistered")
	if _, err := jsrt.Await(handle.Invoke(eventVal, fs.v)); err == nil {
		t.Fatal("handleWorkflowRun resolved for an unregistered class name, want a rejection")
	}
}

// TestHandleWorkflowRun_RethrowsEngineAbort verifies that a Runner error
// produced when the runtime aborts the in-flight step (pause rejects
// step.sleep with "Aborting engine: ...", and AwaitPromise prefixes that)
// is rethrown with the original abort message. workerd only keeps the
// control-plane status when run() rejects with that message; the prefixed
// form is recorded as a user failure.
func TestHandleWorkflowRun_RethrowsEngineAbort(t *testing.T) {
	resetRunners(t)
	withRuntimeContext(t, "MyWorkflow", js.Undefined())

	Register("MyWorkflow", func(ctx context.Context, event *Event, step *Step) (js.Value, error) {
		return js.Value{}, errors.New("failed on promise: Error: Aborting engine: User called pause")
	})

	handle := jsutil.Binding.Get("handleWorkflowRun")
	fs := newFakeStep()
	eventVal := fakeEvent(js.Undefined(), "instance-1", "MyWorkflow")
	_, err := jsrt.Await(handle.Invoke(eventVal, fs.v))
	if err == nil {
		t.Fatal("handleWorkflowRun resolved on an engine abort, want a rejection")
	}
	// Await wraps the rejection once more on the way back into this test.
	// The message handleWorkflowRun itself rejected with must be the bare
	// abort text, so the prefix appears exactly once here.
	if strings.Count(err.Error(), "failed on promise") != 1 {
		t.Fatalf("engine abort rejection = %q, want the abort message without the AwaitPromise prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "Aborting engine: User called pause") {
		t.Fatalf("engine abort rejection = %q, want it to contain the abort message", err.Error())
	}
}

// TestRegister_HandleWorkflowRun_PropagatesRunnerError verifies a Runner
// error rejects the Promise.
func TestRegister_HandleWorkflowRun_PropagatesRunnerError(t *testing.T) {
	resetRunners(t)
	withRuntimeContext(t, "MyWorkflow", js.Undefined())

	Register("MyWorkflow", func(ctx context.Context, event *Event, step *Step) (js.Value, error) {
		return js.Value{}, errors.New("run failed")
	})

	handle := jsutil.Binding.Get("handleWorkflowRun")
	fs := newFakeStep()
	eventVal := fakeEvent(js.Undefined(), "instance-1", "MyWorkflow")
	if _, err := jsrt.Await(handle.Invoke(eventVal, fs.v)); err == nil {
		t.Fatal("handleWorkflowRun resolved despite a Runner error, want a rejection")
	}
}

// TestEvent_PayloadJSON decodes a JSON-shaped JS payload into a typed Go
// value via Event.PayloadJSON.
func TestEvent_PayloadJSON(t *testing.T) {
	type params struct {
		Greeting string `json:"greeting"`
	}
	parsed, err := jsrt.Call(js.Global().Get("JSON"), "parse", `{"greeting":"hi"}`)
	if err != nil {
		t.Fatalf("JSON.parse failed: %v", err)
	}
	event := eventFromJS(fakeEvent(parsed, "instance-1", "MyWorkflow"))

	var p params
	if err := event.PayloadJSON(&p); err != nil {
		t.Fatalf("PayloadJSON() failed: %v", err)
	}
	if p.Greeting != "hi" {
		t.Errorf("PayloadJSON() = %+v, want {Greeting:hi}", p)
	}
}

// TestResultJSON verifies ResultJSON JSON-encodes a Go value into a
// structured-clonable JS value.
func TestResultJSON(t *testing.T) {
	v, err := ResultJSON(map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("ResultJSON() failed: %v", err)
	}
	if !v.Get("ok").Bool() {
		t.Errorf("ResultJSON() value's \"ok\" = %v, want true", v.Get("ok").Bool())
	}
}

// TestNonRetryable_RejectsWithRuntimeClass verifies that a step error
// wrapped with NonRetryable is rejected using the runtime context's
// NonRetryableError class (fetched via jsrt.RuntimeContextValue), not a
// plain Error, when that class is available.
func TestNonRetryable_RejectsWithRuntimeClass(t *testing.T) {
	withRuntimeContext(t, "MyWorkflow", js.Undefined()) // placeholder; overwritten below

	var gotMessage string
	class := js.FuncOf(func(this js.Value, args []js.Value) any {
		gotMessage = args[0].String()
		this.Set("message", args[0])
		this.Set("name", "NonRetryableError")
		return js.Undefined()
	})
	defer class.Release()

	// Re-install the runtime context with the fake NonRetryableError class
	// (withRuntimeContext above already scheduled the restore).
	rc := jsrt.NewObject()
	wf := jsrt.NewObject()
	wf.Set("className", "MyWorkflow")
	rc.Set("workflow", wf)
	rc.Set("NonRetryableError", class.Value)
	jsutil.RuntimeContext = rc

	fs := newFakeStep()
	step := &Step{v: fs.v}

	_, err := step.Do("step-1", func(ctx context.Context) (js.Value, error) {
		return js.Value{}, NonRetryable(errors.New("fatal"))
	})
	if err == nil {
		t.Fatal("Do() succeeded, want an error")
	}
	if gotMessage != "fatal" {
		t.Errorf("NonRetryableError constructor got message %q, want \"fatal\"", gotMessage)
	}
}

// TestNonRetryable_FallsBackWithoutRuntimeClass verifies that, when the
// runtime context has no NonRetryableError class (as under
// runtime/browser.mjs), a NonRetryable-wrapped error still rejects (as a
// plain Error) instead of panicking.
func TestNonRetryable_FallsBackWithoutRuntimeClass(t *testing.T) {
	withRuntimeContext(t, "MyWorkflow", js.Undefined())

	fs := newFakeStep()
	step := &Step{v: fs.v}

	_, err := step.Do("step-1", func(ctx context.Context) (js.Value, error) {
		return js.Value{}, NonRetryable(errors.New("fatal"))
	})
	if err == nil {
		t.Fatal("Do() succeeded, want an error")
	}
}
