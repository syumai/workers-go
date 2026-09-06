//go:build js && wasm

package cloudflare

import (
	"io"
	"net/http"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestNewDurableObjectNamespace_undefined(t *testing.T) {
	jstest.SetEnv(t, map[string]any{})

	if _, err := NewDurableObjectNamespace("MISSING"); err == nil {
		t.Fatal("NewDurableObjectNamespace() error = nil, want error")
	}
}

func TestDurableObjectNamespace_IdFromName_Get_Fetch(t *testing.T) {
	fake := newFakeDurableObjectNamespace(t, http.StatusOK, []byte("hello"))
	jstest.SetEnv(t, map[string]any{"DO": fake.val})

	ns, err := NewDurableObjectNamespace("DO")
	if err != nil {
		t.Fatalf("NewDurableObjectNamespace() error = %v", err)
	}

	id := ns.IdFromName("room-1")
	if fake.lastIdFromName != "room-1" {
		t.Errorf("idFromName called with %q, want %q", fake.lastIdFromName, "room-1")
	}

	stub, err := ns.Get(id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://do.internal/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	res, err := stub.Fetch(req)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", res.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	if _, err := ns.Get(nil); err == nil {
		t.Error("Get(nil) error = nil, want error")
	}
}
