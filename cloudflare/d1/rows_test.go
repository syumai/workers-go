//go:build js && wasm

package d1

import (
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"reflect"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
)

func Test_isIntegralNumber(t *testing.T) {
	tests := map[string]struct {
		f    float64
		want bool
	}{
		"valid positive integral value": {
			f:    1,
			want: true,
		},
		"valid negative integral value": {
			f:    -1,
			want: true,
		},
		"invalid positive float value": {
			f:    1.1,
			want: false,
		},
		"invalid negative float value": {
			f:    -1.1,
			want: false,
		},
		"invalid NaN": {
			f:    math.NaN(),
			want: false,
		},
		"invalid +Inf": {
			f:    math.Inf(+1),
			want: false,
		},
		"invalid -Inf": {
			f:    math.Inf(-1),
			want: false,
		},
	}
	for name, tc := range tests {
		name := name
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isIntegralNumber(tc.f); got != tc.want {
				t.Errorf("isIntegralNumber() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConvertRowColumnValueToAny(t *testing.T) {
	tests := map[string]struct {
		v       js.Value
		want    driver.Value
		wantErr bool
	}{
		"null": {
			v:    js.Null(),
			want: nil,
		},
		"integral number": {
			v:    js.ValueOf(5),
			want: int64(5),
		},
		"decimal number": {
			v:    js.ValueOf(1.5),
			want: float64(1.5),
		},
		"string": {
			v:    js.ValueOf("hello"),
			want: "hello",
		},
		"array buffer": {
			v:    jstest.Uint8Array([]byte{1, 2, 3}).Get("buffer"),
			want: []byte{1, 2, 3},
		},
		"unsupported type": {
			v:       js.ValueOf(true),
			wantErr: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := convertRowColumnValueToAny(tc.v)
			if (err != nil) != tc.wantErr {
				t.Fatalf("convertRowColumnValueToAny() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("convertRowColumnValueToAny() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRows_Next_Columns(t *testing.T) {
	r := &rows{
		_columns: []string{"a", "b"},
		rowsArray: js.ValueOf([]any{
			[]any{"x", 1},
			[]any{"y", 2},
		}),
	}

	if got := r.Columns(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Columns() = %v, want %v", got, []string{"a", "b"})
	}

	dest := make([]driver.Value, 2)

	if err := r.Next(dest); err != nil {
		t.Fatalf("Next() (row 1) error = %v", err)
	}
	if !reflect.DeepEqual(dest, []driver.Value{"x", int64(1)}) {
		t.Fatalf("Next() (row 1) dest = %v, want %v", dest, []driver.Value{"x", int64(1)})
	}

	if err := r.Next(dest); err != nil {
		t.Fatalf("Next() (row 2) error = %v", err)
	}
	if !reflect.DeepEqual(dest, []driver.Value{"y", int64(2)}) {
		t.Fatalf("Next() (row 2) dest = %v, want %v", dest, []driver.Value{"y", int64(2)})
	}

	if err := r.Next(dest); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() (row 3) error = %v, want %v", err, io.EOF)
	}
}

func TestRows_Close(t *testing.T) {
	r := &rows{}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() (1st) error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() (2nd) error = %v", err)
	}
}
