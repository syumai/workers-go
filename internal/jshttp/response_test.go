package jshttp

import (
	"net/http"
	"testing"
)

func TestNewResponseInit(t *testing.T) {
	tests := []struct {
		name           string
		headers        http.Header
		wantEncodeBody bool
	}{
		{
			name: "with Content-Encoding",
			headers: http.Header{
				"Content-Encoding": []string{"gzip"},
			},
			wantEncodeBody: true,
		},
		{
			name:           "without Content-Encoding",
			headers:        http.Header{},
			wantEncodeBody: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newResponseInit(http.StatusOK, tt.headers)

			if status := got.Get("status").Int(); status != http.StatusOK {
				t.Fatalf("status = %v, want %v", status, http.StatusOK)
			}

			if statusText := got.Get("statusText").String(); statusText != http.StatusText(http.StatusOK) {
				t.Fatalf("statusText = %v, want %v", statusText, http.StatusText(http.StatusOK))
			}

			encodeBody := got.Get("encodeBody")
			if tt.wantEncodeBody {
				if encodeBody.IsUndefined() {
					t.Fatalf("encodeBody is undefined, want %q", "manual")
				}
				if got := encodeBody.String(); got != "manual" {
					t.Fatalf("encodeBody = %v, want %v", got, "manual")
				}
			} else {
				if !encodeBody.IsUndefined() {
					t.Fatalf("encodeBody = %v, want undefined", encodeBody)
				}
			}
		})
	}
}
