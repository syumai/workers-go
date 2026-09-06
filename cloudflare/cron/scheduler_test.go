package cron

import (
	"context"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// TestRunScheduler_callsTask verifies that runScheduler (registered on
// jsutil.Binding as "runScheduler" by this package's init) invokes the task
// set via ScheduleTaskNonBlock with a context.Context from which
// NewEvent can extract the same Cron expression and ScheduledTime that were
// on the ScheduledEvent object passed in.
func TestRunScheduler_callsTask(t *testing.T) {
	const cronExpr = "* * * * *"
	const scheduledTimeMs = 1700000000000

	var gotEvent *Event
	ScheduleTaskNonBlock(func(ctx context.Context) error {
		ev, err := NewEvent(ctx)
		if err != nil {
			t.Errorf("NewEvent: %v", err)
			return nil
		}
		gotEvent = ev
		return nil
	})
	t.Cleanup(func() { scheduledTask = nil })

	eventObj := jsutil.NewObject()
	eventObj.Set("cron", cronExpr)
	eventObj.Set("scheduledTime", js.ValueOf(float64(scheduledTimeMs)))

	p := jstest.Binding(t, "runScheduler").Invoke(eventObj)
	jstest.Await(t, p)

	if gotEvent == nil {
		t.Fatalf("task was not called")
	}
	if gotEvent.Cron != cronExpr {
		t.Errorf("Cron = %q, want %q", gotEvent.Cron, cronExpr)
	}
	wantTime := time.Unix(scheduledTimeMs/1000, 0).UTC()
	if !gotEvent.ScheduledTime.Equal(wantTime) {
		t.Errorf("ScheduledTime = %v, want %v", gotEvent.ScheduledTime, wantTime)
	}
}

// TestRunScheduler_taskError documents a known issue found while writing
// this test: unlike handler_js.go's handleRequestCallback and
// queues/consumer.go's handleBatchCallback, runSchedulerCallback's Promise
// executor (in this package's init) only captures `resolve`, not `reject` -
// when the task returns an error, runScheduler's init callback does
// `panic(err)` in a goroutine instead of rejecting the Promise. An
// unrecovered panic in any goroutine crashes the whole wasm process (the
// same failure mode as TestHandleRequest_panicInHandler in the root
// package), rather than surfacing as a normal Promise rejection.
//
// Confirmed empirically with a throwaway probe test: a task returning
// errors.New("boom"), invoked the same way as TestRunScheduler_callsTask,
// aborted the process with a Go panic stack trace instead of rejecting the
// awaited Promise.
func TestRunScheduler_taskError(t *testing.T) {
	t.Skip("known issue: runSchedulerCallback's Promise executor has no reject function - a task error becomes an unrecovered panic(err) in a goroutine (scheduler.go init), which crashes the whole wasm process instead of rejecting the Promise")
}

// TestRunScheduler_beforeSchedule documents a known issue found while
// writing this test: calling runScheduler before
// ScheduleTask/ScheduleTaskNonBlock has set scheduledTask calls a nil Task,
// which is a nil pointer dereference panic. For the same reason as
// TestRunScheduler_taskError (runSchedulerCallback never wires up a reject
// function), this crashes the whole wasm process instead of rejecting the
// Promise.
//
// Confirmed empirically with a throwaway probe test: invoking runScheduler
// with scheduledTask left at its zero value aborted the process with a nil
// pointer dereference panic instead of rejecting the awaited Promise.
func TestRunScheduler_beforeSchedule(t *testing.T) {
	t.Skip("known issue: runScheduler calls scheduledTask(ctx) with no nil check; before ScheduleTask/ScheduleTaskNonBlock is called this is a nil pointer dereference that (like TestRunScheduler_taskError) crashes the whole wasm process instead of rejecting the Promise")
}

// TestScheduleTask_blocks verifies that ScheduleTask calls Ready() and then
// blocks forever: Done() is documented as never closing, to support the
// cloudflare.WaitUntil feature. It runs ScheduleTask in a goroutine that is
// intentionally never unblocked - that goroutine leaks for the rest of the
// test binary's process, which is fine since the process exits once the
// package's tests finish.
func TestScheduleTask_blocks(t *testing.T) {
	before := jstest.ReadyCount(t)
	go func() {
		ScheduleTask(func(context.Context) error { return nil })
	}()

	deadline := time.Now().Add(5 * time.Second)
	for jstest.ReadyCount(t)-before != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("ScheduleTask did not call Ready() within 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
