//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/syumai/workers-go/cloudflare/cron"
	"github.com/syumai/workers-go/cloudflare/kv"
)

// cronLastKey is the KV key the scheduled task writes its Event to, as
// JSON, on every invocation.
const cronLastKey = "cron:last"

// cronEventResponse mirrors cron.Event's fields in JSON so a test can read
// them back via GET /kv/cron:last.
type cronEventResponse struct {
	Cron          string `json:"cron"`
	ScheduledTime string `json:"scheduledTime"`
}

// cronTask is the scheduled Task registered via cron.ScheduleTaskNonBlock.
// It records the triggering Event to KV so GET /kv/cron:last can confirm
// wrangler's --test-scheduled simulation actually fired it and passed
// along a cron string and scheduledTime.
func cronTask(ctx context.Context) error {
	e, err := cron.NewEvent(ctx)
	if err != nil {
		return err
	}
	ns, err := kv.NewNamespace(kvBindingName)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cronEventResponse{
		Cron:          e.Cron,
		ScheduledTime: e.ScheduledTime.Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return ns.PutString(cronLastKey, string(data), nil)
}
