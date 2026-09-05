// Command oldpath is a smoke test for the forwarding mirror described in
// https://github.com/syumai/workers/issues/173: a project that imports only
// the mirror module (the old github.com/syumai/workers import path, and
// nothing from the new module directly) must keep building unchanged.
//
// mirror-tests/run.sh builds this module against a locally generated mirror
// via a "replace github.com/syumai/workers => <generated mirror>" directive,
// so the import path below is the real mirror module path.
package main

import (
	"context"
	"net/http"

	workers "github.com/syumai/workers"
	"github.com/syumai/workers/cloudflare/cron"
	"github.com/syumai/workers/cloudflare/fetch"
	"github.com/syumai/workers/cloudflare/kv"
	"github.com/syumai/workers/cloudflare/queues"
	"github.com/syumai/workers/cloudflare/r2"
)

func main() {
	var mux http.ServeMux
	workers.ServeNonBlock(&mux)

	ns, err := kv.NewNamespace("MY_KV")
	if err == nil {
		_, _ = ns.GetString("key", nil)
	}

	bucket, err := r2.NewBucket("MY_BUCKET")
	if err == nil {
		_, _ = bucket.Get("key")
	}

	client := fetch.NewClient()
	_ = client

	producer, err := queues.NewProducer("MY_QUEUE")
	if err == nil {
		_ = producer
	}

	cron.ScheduleTaskNonBlock(func(ctx context.Context) error {
		return nil
	})

	workers.Ready()
}
