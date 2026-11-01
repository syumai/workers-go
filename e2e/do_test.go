//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// testDurableObjectRPC calls the JS Counter's add method through
// cloudflare.DurableObjectStub.RPC (exp/cloudflare/rpc.Stub.CallJSON) and
// checks that a later fetch() sees the same persisted counter.
func testDurableObjectRPC(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		const name = "e2e-rpc"

		resp, body := w.Get(t, "/counter/rpc?name="+name+"&n=2")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("first rpc status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "2" {
			t.Errorf("first rpc body = %q, want %q", body, "2")
		}

		resp, body = w.Get(t, "/counter/rpc?name="+name+"&n=3")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("second rpc status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "5" {
			t.Errorf("second rpc body = %q, want %q", body, "5")
		}

		// fetch() on the same id increments the counter add() just wrote.
		resp, body = w.Get(t, "/counter?name="+name)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("fetch status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "6" {
			t.Errorf("fetch body = %q, want %q", body, "6")
		}
	}
}

// TestDurableObject drives Durable Object bindings under wrangler dev.
// The counter subtest calls fetch() on the JS Counter class. rpc calls
// that class's add method through DurableObjectStub.RPC. The other
// subtests host the Go Notebook class and exercise SQL storage,
// transactions, blockConcurrencyWhile, and the KV storage methods. See
// testdata/workers/durableobject.
func TestDurableObject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWrangler(t, "durableobject")

	t.Run("counter", func(t *testing.T) {
		// Use a name unique to this test run so repeated `go test`
		// invocations against the same --persist-to don't see stale
		// counts (each run gets its own t.TempDir(), but this also
		// guards against sharing within this subtest's own retries).
		const name = "e2e-counter"

		resp1, body1 := w.Get(t, "/counter?name="+name)
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("first request status = %d, want %d (body = %q)", resp1.StatusCode, http.StatusOK, body1)
		}
		if body1 != "1" {
			t.Errorf("first request body = %q, want %q", body1, "1")
		}

		resp2, body2 := w.Get(t, "/counter?name="+name)
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("second request status = %d, want %d (body = %q)", resp2.StatusCode, http.StatusOK, body2)
		}
		if body2 != "2" {
			t.Errorf("second request body = %q, want %q", body2, "2")
		}

		// A different name must not share state with "e2e-counter".
		resp3, body3 := w.Get(t, "/counter?name=e2e-counter-other")
		if resp3.StatusCode != http.StatusOK {
			t.Fatalf("other-name request status = %d, want %d (body = %q)", resp3.StatusCode, http.StatusOK, body3)
		}
		if body3 != "1" {
			t.Errorf("other-name request body = %q, want %q", body3, "1")
		}
	})

	t.Run("rpc", testDurableObjectRPC(w))
	t.Run("sql", testDurableObjectSQL(w))
	t.Run("opendb", testDurableObjectOpenDB(w))
	t.Run("transaction", testDurableObjectTransaction(w))
	t.Run("blockConcurrencyWhile", testDurableObjectBlockConcurrencyWhile(w))
	t.Run("storage", testDurableObjectStorage(w))
}
