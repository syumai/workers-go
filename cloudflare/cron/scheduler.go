package cron

import (
	"context"
	"syscall/js"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

type Task func(ctx context.Context) error

var (
	scheduledTask Task
	doneCh        = make(chan struct{})
)

func runScheduler(eventObj js.Value) error {
	ctx := runtimecontext.New(context.Background(), eventObj)
	if err := scheduledTask(ctx); err != nil {
		return err
	}
	return nil
}

func init() {
	jsutil.RegisterAsyncHandler("runScheduler", 1, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), runScheduler(args[0])
	})
}

// ScheduleTask sets the Task to be executed
func ScheduleTask(task Task) {
	scheduledTask = task
	workers.Ready()
	<-Done()
}

// ScheduleTaskNonBlock sets the Task to be executed but does not signal readiness or block
// indefinitely. The non-blocking form is meant to be used in conjunction with [workers.Serve].
func ScheduleTaskNonBlock(task Task) {
	scheduledTask = task
}

// Done returns a channel which is closed when the task is done.
// Currently, this channel is never closed to support cloudflare.WaitUntil feature.
func Done() <-chan struct{} {
	return doneCh
}
