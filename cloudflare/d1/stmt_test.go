//go:build js && wasm

package d1

import (
	"database/sql/driver"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
)

// TestConvertArgs fixes the current behavior of convertArgs: []byte is
// converted into a JS Uint8Array, and every other driver.Value (including
// bool and time.Time) is passed through unconverted. Passing a time.Time
// argument all the way to a real bind() call currently panics inside
// syscall/js (js.ValueOf does not know how to convert a time.Time), because
// convertArgs itself never calls js.ValueOf on non-[]byte values. That is
// out of scope here: this test only pins what convertArgs itself returns.
func TestConvertArgs(t *testing.T) {
	someTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := map[string]struct {
		args []driver.NamedValue
		want []any
	}{
		"empty": {
			args: nil,
			want: []any{},
		},
		"nil": {
			args: []driver.NamedValue{{Value: nil}},
			want: []any{nil},
		},
		"string": {
			args: []driver.NamedValue{{Value: "hello"}},
			want: []any{"hello"},
		},
		"int64": {
			args: []driver.NamedValue{{Value: int64(42)}},
			want: []any{int64(42)},
		},
		"float64": {
			args: []driver.NamedValue{{Value: float64(1.5)}},
			want: []any{float64(1.5)},
		},
		"bool": {
			args: []driver.NamedValue{{Value: true}},
			want: []any{true},
		},
		"time": {
			args: []driver.NamedValue{{Value: someTime}},
			want: []any{someTime},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := convertArgs(tc.args)
			if len(got) != len(tc.want) {
				t.Fatalf("convertArgs() len = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("convertArgs()[%d] = %#v, want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}

	t.Run("bytes", func(t *testing.T) {
		src := []byte{1, 2, 3}
		got := convertArgs([]driver.NamedValue{{Value: src}})
		if len(got) != 1 {
			t.Fatalf("convertArgs() len = %d, want 1", len(got))
		}
		jv, ok := got[0].(js.Value)
		if !ok {
			t.Fatalf("convertArgs()[0] = %#v (%T), want js.Value", got[0], got[0])
		}
		if got := jstest.Bytes(t, jv); string(got) != string(src) {
			t.Fatalf("convertArgs()[0] bytes = %v, want %v", got, src)
		}
	})
}
