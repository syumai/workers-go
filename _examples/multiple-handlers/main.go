package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare/cron"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, "Hello, world!")
	})

	task := func(ctx context.Context) error {
		e, err := cron.NewEvent(ctx)
		if err != nil {
			return err
		}
		fmt.Println(e.ScheduledTime.Unix())
		return nil
	}

	// set up the worker
	workers.ServeNonBlock(handler)
	cron.ScheduleTaskNonBlock(task)

	// send a ready signal to the runtime
	workers.Ready()

	// block until the handler or task is done
	select {
	case <-workers.Done():
	case <-cron.Done():
	}
}
