package d1

import (
	"context"
	"database/sql/driver"
	"errors"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

type stmt struct {
	stmtObj js.Value
}

var (
	_ driver.Stmt             = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
	_ driver.StmtQueryContext = (*stmt)(nil)
)

func (s *stmt) Close() error {
	// do nothing
	return nil
}

// NumInput is not supported and always returns -1.
func (s *stmt) NumInput() int {
	return -1
}

func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("d1: Exec is deprecated and not implemented")
}

// ExecContext executes prepared statement.
// Given []driver.NamedValue's `Name` field will be ignored because Cloudflare D1 client doesn't support it.
func (s *stmt) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	argValues := make([]any, len(args))
	for i, arg := range args {
		if src, ok := arg.Value.([]byte); ok {
			dst := jsutil.Uint8ArrayClass.New(len(src))
			if n := js.CopyBytesToJS(dst, src); n != len(src) {
				return nil, errors.New("incomplete copy into Uint8Array")
			}
			argValues[i] = dst
		} else {
			argValues[i] = arg.Value
		}
	}
	resultPromise := s.stmtObj.Call("bind", argValues...).Call("run")
	resultObj, err := jsutil.AwaitPromise(resultPromise)
	if err != nil {
		return nil, err
	}
	return &result{
		resultObj: resultObj,
	}, nil
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("d1: Query is deprecated and not implemented")
}

func (s *stmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	argValues := make([]any, len(args))
	for i, arg := range args {
		if src, ok := arg.Value.([]byte); ok {
			dst := jsutil.Uint8ArrayClass.New(len(src))
			if n := js.CopyBytesToJS(dst, src); n != len(src) {
				return nil, errors.New("incomplete copy into Uint8Array")
			}
			argValues[i] = dst
		} else {
			argValues[i] = arg.Value
		}
	}
	resultPromise := s.stmtObj.Call("bind", argValues...).Call("raw", map[string]any{"columnNames": true})
	rowsArray, err := jsutil.AwaitPromise(resultPromise)
	if err != nil {
		return nil, err
	}
	// If there are no rows to retrieve, length is 0.
	if rowsArray.Length() == 0 {
		return &rows{
			_columns:  nil,
			rowsArray: rowsArray,
		}, nil
	}

	// First item of rowsArray is column names
	colsArray := rowsArray.Call("shift")
	colsLen := colsArray.Length()
	cols := make([]string, colsLen)
	for i := 0; i < colsLen; i++ {
		cols[i] = colsArray.Index(i).String()
	}
	return &rows{
		_columns:  cols,
		rowsArray: rowsArray,
	}, nil
}
