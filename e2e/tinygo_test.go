//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestTinyGo checks that a Worker built with the tinygo toolchain
// (workers-assets-gen -mode=tinygo + `tinygo build`) works end-to-end
// under a real workerd. It only runs when E2E_TINYGO=1, since it requires
// a local tinygo install that most contributors won't have; TestMain never
// builds this fixture (see buildFixtureTinygo in e2e_test.go).
func TestTinyGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	if os.Getenv("E2E_TINYGO") != "1" {
		t.Skip("set E2E_TINYGO=1 to run the tinygo e2e test")
	}

	e2eDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	repoRoot, err := repoRootDir()
	if err != nil {
		t.Fatalf("failed to determine repo root: %v", err)
	}
	if err := buildFixtureTinygo(repoRoot, e2eDir, "tinygo"); err != nil {
		t.Fatalf("failed to build tinygo fixture: %v", err)
	}

	w := startWrangler(t, "tinygo")

	t.Run("hello", func(t *testing.T) {
		resp, body := w.Get(t, "/hello")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != "hello" {
			t.Errorf("body = %q, want %q", body, "hello")
		}
	})

	t.Run("echo", func(t *testing.T) {
		const wantBody = "hello from the tinygo e2e test"
		resp, body := w.Do(t, http.MethodPost, "/echo", nil, strings.NewReader(wantBody))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		if body != wantBody {
			t.Errorf("body = %q, want %q", body, wantBody)
		}
	})

	t.Run("kv", func(t *testing.T) {
		const key = "e2e-tinygo-kv"
		const value = "tinygo-kv-value"

		putResp, putBody := w.Do(t, http.MethodPut, "/kv/"+key, nil, strings.NewReader(value))
		if putResp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT status = %d, want %d (body = %q)", putResp.StatusCode, http.StatusNoContent, putBody)
		}

		getResp, body := w.Get(t, "/kv/"+key)
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("GET status = %d, want %d (body = %q)", getResp.StatusCode, http.StatusOK, body)
		}
		if body != value {
			t.Errorf("GET body = %q, want %q", body, value)
		}
	})
}
