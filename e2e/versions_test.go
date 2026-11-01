//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// versionMetadataResponse mirrors
// testdata/workers/kitchensink/versions.go. Local wrangler dev fills these
// from miniflare (a fresh UUID, an empty tag, and an ISO timestamp) rather
// than a deployed version, so the test checks shape, not exact values.
type versionMetadataResponse struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	Timestamp string `json:"timestamp"`
}

// versionIDPattern accepts the UUID miniflare assigns in local dev and any
// other token wrangler might use that is still a plain identifier.
var versionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

func testVersionsMetadata(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		resp, body := w.Get(t, "/versions")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		var got versionMetadataResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal /versions response %q: %v", body, err)
		}
		if !versionIDPattern.MatchString(got.ID) {
			t.Errorf("id = %q, want a non-empty identifier", got.ID)
		}
		if !wellFormedVersionString(got.Tag, true) {
			t.Errorf("tag = %q, want a plain string", got.Tag)
		}
		if !wellFormedVersionString(got.Timestamp, false) {
			t.Errorf("timestamp = %q, want a non-empty plain string", got.Timestamp)
		}
		if _, err := time.Parse(time.RFC3339Nano, got.Timestamp); err != nil {
			t.Errorf("timestamp = %q, want an RFC3339 timestamp: %v", got.Timestamp, err)
		}
	}
}

// wellFormedVersionString reports whether s is a plain decoded string.
// syscall/js turns a missing or non-string JS value into a debug string
// such as "<undefined>" or "<boolean: false>"; those must not pass.
// allowEmpty is for fields local dev may leave blank (the version tag).
func wellFormedVersionString(s string, allowEmpty bool) bool {
	if s == "" {
		return allowEmpty
	}
	if strings.Contains(s, "<") || strings.Contains(s, ">") {
		return false
	}
	return true
}
