//go:build js && wasm

package queues

import "testing"

// TestSendOptions_toJS fixes sendOptions.toJS's current behavior. Unlike
// batchSendOptions.toJS and retryOptions.toJS, sendOptions.toJS has no nil
// guard: it dereferences the receiver unconditionally. That is fine in
// practice because every caller in this package builds a sendOptions value
// (never a nil *sendOptions), so this test only exercises non-nil inputs.
func TestSendOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts        sendOptions
		wantJSON    map[string]any
		wantNoDelay bool
	}{
		"zero_value": {
			opts:        sendOptions{},
			wantNoDelay: true,
		},
		"content_type_only": {
			opts:        sendOptions{ContentType: contentTypeText},
			wantNoDelay: true,
		},
		"with_delay": {
			opts: sendOptions{ContentType: contentTypeJSON, DelaySeconds: 5},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toJS()
			if got.IsUndefined() {
				t.Fatalf("toJS() = undefined, want an object")
			}
			if v := got.Get("contentType").String(); v != string(tc.opts.ContentType) {
				t.Errorf("contentType = %q, want %q", v, tc.opts.ContentType)
			}
			delay := got.Get("delaySeconds")
			if tc.wantNoDelay {
				if !delay.IsUndefined() {
					t.Errorf("delaySeconds = %v, want undefined (0 is omitted)", delay)
				}
				return
			}
			if delay.Int() != tc.opts.DelaySeconds {
				t.Errorf("delaySeconds = %v, want %v", delay.Int(), tc.opts.DelaySeconds)
			}
		})
	}
}

func TestBatchSendOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts      *batchSendOptions
		wantUndef bool
		wantDelay int
		wantOmit  bool
	}{
		"nil": {
			opts:      nil,
			wantUndef: true,
		},
		"zero_omitted": {
			opts:     &batchSendOptions{},
			wantOmit: true,
		},
		"delay": {
			opts:      &batchSendOptions{DelaySeconds: 5},
			wantDelay: 5,
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
			delay := got.Get("delaySeconds")
			if tc.wantOmit {
				if !delay.IsUndefined() {
					t.Errorf("delaySeconds = %v, want undefined", delay)
				}
				return
			}
			if delay.Int() != tc.wantDelay {
				t.Errorf("delaySeconds = %v, want %v", delay.Int(), tc.wantDelay)
			}
		})
	}
}

func TestRetryOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts      *retryOptions
		wantUndef bool
		wantDelay int
		wantOmit  bool
	}{
		"nil": {
			opts:      nil,
			wantUndef: true,
		},
		"zero_omitted": {
			opts:     &retryOptions{},
			wantOmit: true,
		},
		"delay": {
			opts:      &retryOptions{delaySeconds: 10},
			wantDelay: 10,
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
			delay := got.Get("delaySeconds")
			if tc.wantOmit {
				if !delay.IsUndefined() {
					t.Errorf("delaySeconds = %v, want undefined", delay)
				}
				return
			}
			if delay.Int() != tc.wantDelay {
				t.Errorf("delaySeconds = %v, want %v", delay.Int(), tc.wantDelay)
			}
		})
	}
}

func TestContentType(t *testing.T) {
	tests := map[string]struct {
		ct   contentType
		want string
	}{
		"json":  {ct: contentTypeJSON, want: "json"},
		"v8":    {ct: contentTypeV8, want: "v8"},
		"text":  {ct: contentTypeText, want: "text"},
		"bytes": {ct: contentTypeBytes, want: "bytes"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := string(tc.ct); got != tc.want {
				t.Errorf("string(%s) = %q, want %q", name, got, tc.want)
			}
		})
	}
}
