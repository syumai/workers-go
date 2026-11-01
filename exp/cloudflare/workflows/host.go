//go:build js && wasm

package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// This file hosts a Go implementation of a WorkflowEntrypoint (see
// tmp/06-codegen-spec.md 7), mirroring exp/cloudflare/durableobjects/
// host.go's approach for Durable Objects: "run() 1 回 = wasm 1 インスタンス"
// (a fresh wasm instance handles each run() invocation — unlike a Durable
// Object, a Workflow's WorkflowEntrypoint doesn't need cross-trigger state,
// so there is no instance-memoization equivalent to durableobjects.instance()
// here), dispatched through worker.mjs's GoWorkflowEntrypoint#bind and the
// "workflow" runtime context entry it sets up
// (cmd/workers-assets-gen/assets/runtimes/cloudflare/worker.mjs).

// Event is the WorkflowEvent<T> passed to a Runner. Payload is the raw,
// structured-clonable JS value; use PayloadJSON to decode it into a Go
// value.
type Event struct {
	Payload      js.Value
	Timestamp    time.Time
	InstanceID   string
	WorkflowName string
}

// eventFromJS decodes a WorkflowEvent JS value.
func eventFromJS(v js.Value) *Event {
	return &Event{
		Payload:      v.Get("payload"),
		Timestamp:    jsrt.DateToTime(v.Get("timestamp")),
		InstanceID:   v.Get("instanceId").String(),
		WorkflowName: v.Get("workflowName").String(),
	}
}

// PayloadJSON decodes e.Payload into v (which must be a pointer, as for
// [encoding/json.Unmarshal]) by round-tripping it through the JS side's
// JSON.stringify, the same way durableobjects.DurableObjectStorage.GetJSON
// does. If e.Payload is undefined or null, v is left untouched.
func (e *Event) PayloadJSON(v any) error {
	if jsrt.IsNil(e.Payload) {
		return nil
	}
	s, err := jsrt.Call(js.Global().Get("JSON"), "stringify", e.Payload)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(s.String()), v)
}

// StepEvent is the WorkflowStepEvent<T> resolved by Step.WaitForEvent.
type StepEvent struct {
	Payload   js.Value
	Timestamp time.Time
	Type      string
}

func stepEventFromJS(v js.Value) *StepEvent {
	return &StepEvent{
		Payload:   v.Get("payload"),
		Timestamp: jsrt.DateToTime(v.Get("timestamp")),
		Type:      v.Get("type").String(),
	}
}

// Backoff is WorkflowBackoff: the retry backoff strategy for a step.
type Backoff string

const (
	BackoffConstant    Backoff = "constant"
	BackoffLinear      Backoff = "linear"
	BackoffExponential Backoff = "exponential"
)

// RetryConfig is StepConfig.Retries.
type RetryConfig struct {
	Limit   int
	Delay   time.Duration
	Backoff Backoff
}

// StepConfig is the WorkflowStepConfig passed to Step.DoWithConfig /
// DoJSONWithConfig.
type StepConfig struct {
	Retries   *RetryConfig
	Timeout   time.Duration
	Sensitive bool
}

// toJS builds the WorkflowStepConfig JS object. Retries.Delay/Timeout are
// sent as the number-of-milliseconds branch of WorkflowSleepDuration /
// WorkflowTimeoutDuration.
func (c StepConfig) toJS() js.Value {
	obj := jsutil.NewObject()
	if c.Retries != nil {
		retries := jsutil.NewObject()
		retries.Set("limit", c.Retries.Limit)
		retries.Set("delay", float64(c.Retries.Delay.Milliseconds()))
		if c.Retries.Backoff != "" {
			retries.Set("backoff", string(c.Retries.Backoff))
		}
		obj.Set("retries", retries)
	}
	if c.Timeout > 0 {
		obj.Set("timeout", float64(c.Timeout.Milliseconds()))
	}
	if c.Sensitive {
		// WorkflowStepSensitivity currently has a single value, "output".
		obj.Set("sensitive", "output")
	}
	return obj
}

// Step is a hand-written wrapper around a JS WorkflowStep. Unlike the
// generated L1 bindings under zworkflows_gen.go, WorkflowStep.do's
// multiple, config-discriminated overloads don't fit cfgen's single-callback
// rule (tmp/06-codegen-spec.md 7.0), so this type and its methods are
// hand-written instead.
type Step struct{ v js.Value }

// nonRetryableError marks an error, via NonRetryable, as one that should
// fail its Workflow step (or run) permanently instead of being retried.
type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }

// NonRetryable wraps err so that, when returned from a Step.Do/DoWithConfig
// (or DoJSON/DoJSONWithConfig) callback, the step is rejected with the
// runtime's cloudflare:workflows NonRetryableError class instead of a plain
// Error, telling Workflows not to retry that step
// (https://developers.cloudflare.com/workflows/build/rules-of-workflows/#errors-and-exceptions).
// It is a no-op wrapper outside of that context. NonRetryable(nil) returns
// nil.
func NonRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &nonRetryableError{err: err}
}

// errorToJS converts err into the JS value a step/run rejection should
// carry: an instance of the runtime's NonRetryableError class if err was
// wrapped with NonRetryable and that class is available (it is, under
// runtime/cloudflare.mjs; browser.mjs has none, per
// tmp/06-codegen-spec.md 7.2), otherwise a plain Error.
func errorToJS(err error) js.Value {
	var nre *nonRetryableError
	if errors.As(err, &nre) {
		if class, classErr := jsrt.RuntimeContextValue("NonRetryableError"); classErr == nil {
			return class.New(err.Error())
		}
	}
	return jsutil.Error(err.Error())
}

// stepCallback builds the JS callback passed as WorkflowStep.do's
// (ctx) => Promise<T> argument. It mirrors jsrt.AsyncFunc/jsutil.AsyncFunc
// (handler runs in a new goroutine; a panic is recovered into a rejection)
// but, unlike them, rejects through errorToJS instead of always wrapping
// the error in a plain Error, so a NonRetryable-wrapped error surfaces to
// Workflows as a NonRetryableError instance (see NonRetryable's doc
// comment). fn does not receive the JS WorkflowStepContext argument.
func stepCallback(fn func(ctx context.Context) (js.Value, error)) js.Func {
	return js.FuncOf(func(_ js.Value, _ []js.Value) any {
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(jsutil.Errorf("panic in workflow step: %v", r))
					}
				}()
				result, err := fn(context.Background())
				if err != nil {
					reject.Invoke(errorToJS(err))
					return
				}
				resolve.Invoke(result)
			}()
			return js.Undefined()
		})
		return jsutil.NewPromise(cb)
	})
}

// Do runs a step named name: fn runs only if this step hasn't already
// completed for this Workflow instance (a replayed run reuses the saved
// result instead of calling fn again). fn's returned js.Value must be
// structured-clonable; use DoJSON to work with a typed Go value instead.
// A non-nil error from fn fails the step (Workflows retries it, unless the
// error was wrapped with NonRetryable).
func (s *Step) Do(name string, fn func(ctx context.Context) (js.Value, error)) (js.Value, error) {
	cb := stepCallback(fn)
	defer cb.Release()
	p, err := jsrt.Call(s.v, "do", name, cb)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Await(p)
}

// DoWithConfig is Do with an explicit WorkflowStepConfig (retries, timeout,
// sensitive).
func (s *Step) DoWithConfig(name string, cfg StepConfig, fn func(ctx context.Context) (js.Value, error)) (js.Value, error) {
	cb := stepCallback(fn)
	defer cb.Release()
	p, err := jsrt.Call(s.v, "do", name, cfg.toJS(), cb)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Await(p)
}

// valueToJS JSON-encodes v (via encoding/json.Marshal) and parses the
// result back into a structured-clonable JS value (JSON.parse), the same
// round trip durableobjects.DurableObjectStorage.PutJSON uses.
func valueToJS(v any) (js.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Call(js.Global().Get("JSON"), "parse", string(b))
}

// jsToValue decodes v (JSON.stringify'd) into a T via encoding/json.
// An undefined/null v decodes to T's zero value.
func jsToValue[T any](v js.Value) (T, error) {
	var out T
	if jsrt.IsNil(v) {
		return out, nil
	}
	s, err := jsrt.Call(js.Global().Get("JSON"), "stringify", v)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(s.String()), &out); err != nil {
		return out, err
	}
	return out, nil
}

// DoJSON is Do for a typed Go value T instead of a raw js.Value: fn's
// result is JSON-encoded to a structured-clonable JS value before being
// handed to WorkflowStep.do, and the step's result (freshly computed, or
// the saved value on replay) is JSON-decoded back into a T.
func DoJSON[T any](s *Step, name string, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	result, err := s.Do(name, func(ctx context.Context) (js.Value, error) {
		v, err := fn(ctx)
		if err != nil {
			return js.Value{}, err
		}
		return valueToJS(v)
	})
	if err != nil {
		return zero, err
	}
	return jsToValue[T](result)
}

// DoJSONWithConfig is DoJSON with an explicit WorkflowStepConfig.
func DoJSONWithConfig[T any](s *Step, name string, cfg StepConfig, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	result, err := s.DoWithConfig(name, cfg, func(ctx context.Context) (js.Value, error) {
		v, err := fn(ctx)
		if err != nil {
			return js.Value{}, err
		}
		return valueToJS(v)
	})
	if err != nil {
		return zero, err
	}
	return jsToValue[T](result)
}

// Sleep pauses the Workflow (durably — it survives a restart/eviction of
// the underlying wasm instance) for d, under step name.
func (s *Step) Sleep(name string, d time.Duration) error {
	p, err := jsrt.Call(s.v, "sleep", name, float64(d.Milliseconds()))
	if err != nil {
		return err
	}
	_, err = jsrt.Await(p)
	return err
}

// SleepUntil pauses the Workflow until t, under step name.
func (s *Step) SleepUntil(name string, t time.Time) error {
	p, err := jsrt.Call(s.v, "sleepUntil", name, jsrt.TimeToDate(t))
	if err != nil {
		return err
	}
	_, err = jsrt.Await(p)
	return err
}

// WaitForEvent waits, under step name, for an event of type eventType sent
// to this Workflow instance (e.g. via the client-side L1's
// WorkflowInstance.SendEvent). timeout of 0 omits the timeout option (the
// runtime's default applies).
func (s *Step) WaitForEvent(name, eventType string, timeout time.Duration) (*StepEvent, error) {
	opts := jsutil.NewObject()
	opts.Set("type", eventType)
	if timeout > 0 {
		opts.Set("timeout", float64(timeout.Milliseconds()))
	}
	p, err := jsrt.Call(s.v, "waitForEvent", name, opts)
	if err != nil {
		return nil, err
	}
	r, err := jsrt.Await(p)
	if err != nil {
		return nil, err
	}
	return stepEventFromJS(r), nil
}

// Runner implements a Workflow's run(event, step): the Go-side body of a
// WorkflowEntrypoint, registered per Workflow class name via Register. Its
// returned js.Value becomes run()'s resolved value (structured-clonable;
// see ResultJSON to build one from a typed Go value) and a non-nil error
// rejects run() (wrap it with NonRetryable to fail the Workflow instance
// permanently instead of retrying it).
type Runner func(ctx context.Context, event *Event, step *Step) (js.Value, error)

var (
	runnersMu sync.Mutex
	runners   = map[string]Runner{}
)

// Register associates className (matching both the -workflows flag passed
// to workers-assets-gen and wrangler.toml's [[workflows]] class_name) with
// r. Register must be called for every Workflow class this Worker hosts,
// before workers.Serve (or any other blocking entry point) is called in
// main.
//
// Calling Register more than once for the same className overwrites the
// previously registered Runner.
func Register(className string, r Runner) {
	runnersMu.Lock()
	defer runnersMu.Unlock()
	runners[className] = r
}

// ResultJSON JSON-encodes v into the js.Value a Runner should return, the
// same way DoJSON encodes a step's result.
func ResultJSON(v any) (js.Value, error) {
	return valueToJS(v)
}

func init() {
	jsutil.RegisterAsyncHandler("handleWorkflowRun", 2, func(args []js.Value) (js.Value, error) {
		if len(args) < 2 {
			return js.Value{}, fmt.Errorf("workflows: handleWorkflowRun requires 2 arguments (event, step), got %d", len(args))
		}
		className, err := currentClassName()
		if err != nil {
			return js.Value{}, err
		}
		runnersMu.Lock()
		r, ok := runners[className]
		runnersMu.Unlock()
		if !ok {
			return js.Value{}, fmt.Errorf("workflows: no Runner registered for class %q; call workflows.Register before workers.Serve", className)
		}
		event := eventFromJS(args[0])
		step := &Step{v: args[1]}
		result, err := r(context.Background(), event, step)
		if abort, ok := engineAbortMessage(err); ok {
			// Pause (and the other control-plane aborts) reject the
			// in-flight step promise with "Aborting engine: ...".
			// AwaitPromise prefixes that, and rejecting run() with the
			// prefixed text makes workerd record a user failure (status
			// "errored") instead of the control-plane status ("paused").
			// The engine keeps the control-plane status only when run()
			// rejects with the original abort message.
			return js.Value{}, errors.New(abort)
		}
		return result, err
	})
}

// engineAbortMessage returns the Workers runtime abort text embedded in
// err. step.Sleep / step.waitForEvent reject with that text when the
// isolate is aborted because the instance was paused or otherwise stopped
// by the engine; it is not a step failure.
func engineAbortMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	const marker = "Aborting engine:"
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	return msg[i:], true
}

// currentClassName reads workflow.className off the runtime context — set
// by worker.mjs's GoWorkflowEntrypoint#bind for every run() trigger
// dispatched to a Workflow instance (see
// cmd/workers-assets-gen/assets/runtimes/cloudflare/worker.mjs).
func currentClassName() (string, error) {
	wf, err := jsrt.RuntimeContextValue("workflow")
	if err != nil {
		return "", fmt.Errorf("workflows: no \"workflow\" runtime context value (was this triggered as a Workflow? see cmd/workers-assets-gen's -workflows flag): %w", err)
	}
	name := wf.Get("className")
	if jsrt.IsNil(name) {
		return "", errors.New("workflows: workflow.className is not set")
	}
	return name.String(), nil
}
