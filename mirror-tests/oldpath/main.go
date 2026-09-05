// Command oldpath is a smoke test for the forwarding mirror described in
// https://github.com/syumai/workers/issues/173: a project that imports only
// the mirror module (the old github.com/syumai/workers import path, and
// nothing from the new module directly) must keep building unchanged.
//
// The import path below is a placeholder rewritten by mirror-tests/run.sh to
// whatever module path the mirror under test actually declares.
package main

import (
	"context"
	"net/http"

	workers "OLDPATH_MIRROR_MODULE_PLACEHOLDER"
	"OLDPATH_MIRROR_MODULE_PLACEHOLDER/cloudflare/cron"
	"OLDPATH_MIRROR_MODULE_PLACEHOLDER/cloudflare/fetch"
	"OLDPATH_MIRROR_MODULE_PLACEHOLDER/cloudflare/kv"
	"OLDPATH_MIRROR_MODULE_PLACEHOLDER/cloudflare/queues"
	"OLDPATH_MIRROR_MODULE_PLACEHOLDER/cloudflare/r2"
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
