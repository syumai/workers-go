package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/syumai/workers-go/cloudflare/cron"
)

func task(ctx context.Context) error {
	e, err := cron.NewEvent(ctx)
	if err != nil {
		return fmt.Errorf("failed to create cron event: %w", err)
	}
	log.Printf("Cron job triggered at: %s", time.Unix(e.ScheduledTime.Unix(), 0).Format(time.RFC3339))
	log.Println("Executing scheduled task...")
	return nil
}

func main() {
	cron.ScheduleTask(task)
	select {}
}
