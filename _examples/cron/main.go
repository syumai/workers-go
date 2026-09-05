package main

import (
	"context"
	"fmt"
	"time"

	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/cron"
)

func task(ctx context.Context) error {
	e, err := cron.NewEvent(ctx)
	if err != nil {
		return err
	}

	fmt.Println(e.ScheduledTime.Unix())

	cloudflare.WaitUntil(func() {
		time.Sleep(1 * time.Second)
		fmt.Println("Run sub task after returning from main task")
	})

	return nil
}

func main() {
	cron.ScheduleTask(task)
}
