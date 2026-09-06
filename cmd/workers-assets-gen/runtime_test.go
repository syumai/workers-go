package main

import "testing"

func TestRuntime_IsValid(t *testing.T) {
	tests := map[string]struct {
		runtime Runtime
		want    bool
	}{
		"cloudflare": {runtime: RuntimeCloudflare, want: true},
		"browser":    {runtime: RuntimeBrowser, want: true},
		"empty":      {runtime: Runtime(""), want: false},
		"invalid":    {runtime: Runtime("invalid"), want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.runtime.IsValid()
			if got != tt.want {
				t.Errorf("Runtime(%q).IsValid() = %v, want %v", tt.runtime, got, tt.want)
			}
		})
	}
}

func TestRuntime_AssetFileName(t *testing.T) {
	tests := map[string]struct {
		runtime Runtime
		want    string
	}{
		"cloudflare": {runtime: RuntimeCloudflare, want: "cloudflare.mjs"},
		"browser":    {runtime: RuntimeBrowser, want: "browser.mjs"},
		"empty":      {runtime: Runtime(""), want: ".mjs"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.runtime.AssetFileName()
			if got != tt.want {
				t.Errorf("Runtime(%q).AssetFileName() = %q, want %q", tt.runtime, got, tt.want)
			}
		})
	}
}
