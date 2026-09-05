package d1

import (
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

type rows struct {
	rowsArray  js.Value
	currentRow int
	// _columns is cached value of Columns method.
	// do not use this directly.
	_columns []string
	// _rowsLen is cached value of rowsLen method.
	// do not use this directly.
	_rowsLen    int
	onceRowsLen sync.Once
	mu          sync.Mutex
}

var _ driver.Rows = (*rows)(nil)

// Columns returns column names retrieved from query result.
// If rows are empty, this returns nil.
func (r *rows) Columns() []string {
	return r._columns
}

func (r *rows) Close() error {
	// do nothing
	return nil
}

// isIntegralNumber returns if given float64 value is integral value or not.
func isIntegralNumber(f float64) bool {
	// If the value is NaN or Inf, returns the value to avoid being mistakenly treated as an integral value.
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return false
	}
	return f == math.Trunc(f)
}

// convertRowColumnValueToDriverValue converts row column's value in JS to Go's driver.Value.
// row column value is `null | Number | String | ArrayBuffer`.
// see: https://developers.cloudflare.com/d1/platform/client-api/#type-conversion
func convertRowColumnValueToAny(v js.Value) (driver.Value, error) {
	switch v.Type() {
	case js.TypeNull:
		return nil, nil
	case js.TypeNumber:
		fv := v.Float()
		// if the value can be treated as integral value, return as int64.
		if isIntegralNumber(fv) {
			return int64(fv), nil
		}
		return fv, nil
	case js.TypeString:
		return v.String(), nil
	case js.TypeObject:
		// handle BLOB type (ArrayBuffer).
		src := jsutil.Uint8ArrayClass.New(v)
		dst := make([]byte, src.Length())
		n := js.CopyBytesToGo(dst, src)
		if n != len(dst) {
			return nil, errors.New("incomplete copy from Uint8Array")
		}
		return dst[:n], nil
	}
	return nil, errors.New("d1: unexpected row column value type")
}

func (r *rows) Next(dest []driver.Value) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentRow == r.rowsLen() {
		return io.EOF
	}
	// rowArray is Array of string.
	rowArray := r.rowsArray.Index(r.currentRow)
	rowArrayLen := rowArray.Length()
	for i := 0; i < rowArrayLen; i++ {
		v, err := convertRowColumnValueToAny(rowArray.Index(i))
		if err != nil {
			return err
		}
		dest[i] = v
	}
	r.currentRow++
	return nil
}

func (r *rows) rowsLen() int {
	r.onceRowsLen.Do(func() {
		r._rowsLen = r.rowsArray.Length()
	})
	return r._rowsLen
}
