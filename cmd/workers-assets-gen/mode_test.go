package main

import "testing"

func TestMode_IsValid(t *testing.T) {
	tests := map[string]struct {
		mode Mode
		want bool
	}{
		"go":      {mode: ModeGo, want: true},
		"tinygo":  {mode: ModeTinygo, want: true},
		"empty":   {mode: Mode(""), want: false},
		"invalid": {mode: Mode("invalid"), want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.mode.IsValid()
			if got != tt.want {
				t.Errorf("Mode(%q).IsValid() = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}
