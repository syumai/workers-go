//go:build js && wasm

package kv

import (
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func TestGetOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts  *GetOptions
		type_ string
		want  map[string]any
	}{
		"nil_text":   {opts: nil, type_: "text", want: map[string]any{"type": "text"}},
		"nil_stream": {opts: nil, type_: "stream", want: map[string]any{"type": "stream"}},
		"cache_ttl":  {opts: &GetOptions{CacheTTL: 60}, type_: "text", want: map[string]any{"type": "text", "cacheTtl": 60}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toJS(tt.type_)
			jstest.AssertObjectEqual(t, got, tt.want)
		})
	}
}

func TestPutOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts *PutOptions
		want map[string]any // nil means undefined is expected
	}{
		"nil":            {opts: nil, want: nil},
		"expiration":     {opts: &PutOptions{Expiration: 10}, want: map[string]any{"expiration": 10}},
		"expiration_ttl": {opts: &PutOptions{ExpirationTTL: 60}, want: map[string]any{"expirationTtl": 60}},
		"both":           {opts: &PutOptions{Expiration: 10, ExpirationTTL: 60}, want: map[string]any{"expiration": 10, "expirationTtl": 60}},
		"zero_omitted":   {opts: &PutOptions{}, want: map[string]any{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toJS()
			if tt.want == nil {
				if !got.IsUndefined() {
					t.Fatalf("toJS() = %v, want undefined", got)
				}
				return
			}
			jstest.AssertObjectEqual(t, got, tt.want)
		})
	}
}

func TestListOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts *ListOptions
		want map[string]any // nil means undefined is expected
	}{
		"nil":          {opts: nil, want: nil},
		"limit":        {opts: &ListOptions{Limit: 5}, want: map[string]any{"limit": 5}},
		"prefix":       {opts: &ListOptions{Prefix: "foo/"}, want: map[string]any{"prefix": "foo/"}},
		"cursor":       {opts: &ListOptions{Cursor: "abc"}, want: map[string]any{"cursor": "abc"}},
		"all":          {opts: &ListOptions{Limit: 5, Prefix: "foo/", Cursor: "abc"}, want: map[string]any{"limit": 5, "prefix": "foo/", "cursor": "abc"}},
		"zero_omitted": {opts: &ListOptions{}, want: map[string]any{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toJS()
			if tt.want == nil {
				if !got.IsUndefined() {
					t.Fatalf("toJS() = %v, want undefined", got)
				}
				return
			}
			jstest.AssertObjectEqual(t, got, tt.want)
		})
	}
}
