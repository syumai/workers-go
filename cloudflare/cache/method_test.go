//go:build js && wasm

package cache

import "testing"

func TestMatchOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts       *MatchOptions
		wantUndef  bool
		wantIgnore bool
	}{
		"nil": {
			opts:      nil,
			wantUndef: true,
		},
		"ignore_method_false": {
			opts: &MatchOptions{},
		},
		"ignore_method_true": {
			opts:       &MatchOptions{IgnoreMethod: true},
			wantIgnore: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toJS()
			if tc.wantUndef {
				if !got.IsUndefined() {
					t.Fatalf("toJS() = %v, want undefined", got)
				}
				return
			}
			if got.IsUndefined() {
				t.Fatalf("toJS() = undefined, want an object")
			}
			if v := got.Get("ignoreMethod").Bool(); v != tc.wantIgnore {
				t.Errorf("ignoreMethod = %v, want %v", v, tc.wantIgnore)
			}
		})
	}
}

func TestDeleteOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts       *DeleteOptions
		wantUndef  bool
		wantIgnore bool
	}{
		"nil": {
			opts:      nil,
			wantUndef: true,
		},
		"ignore_method_false": {
			opts: &DeleteOptions{},
		},
		"ignore_method_true": {
			opts:       &DeleteOptions{IgnoreMethod: true},
			wantIgnore: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toJS()
			if tc.wantUndef {
				if !got.IsUndefined() {
					t.Fatalf("toJS() = %v, want undefined", got)
				}
				return
			}
			if got.IsUndefined() {
				t.Fatalf("toJS() = undefined, want an object")
			}
			if v := got.Get("ignoreMethod").Bool(); v != tc.wantIgnore {
				t.Errorf("ignoreMethod = %v, want %v", v, tc.wantIgnore)
			}
		})
	}
}
