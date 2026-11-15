//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go/cloudflare/d1"
)

// d1Request is the shared body shape for POST /d1/exec and POST /d1/query:
// a SQL statement plus its positional arguments.
type d1Request struct {
	SQL  string  `json:"sql"`
	Args []d1Arg `json:"args"`
}

// d1Arg decodes one element of a /d1/exec or /d1/query request's "args"
// array. A JSON object of the shape {"base64": "..."} decodes to []byte,
// so callers can round-trip BLOB values; any other JSON value (string,
// number, bool, null) decodes as-is via encoding/json's default mapping
// (numbers become float64), which database/sql's default parameter
// converter accepts.
type d1Arg struct {
	Value any
}

func (a *d1Arg) UnmarshalJSON(data []byte) error {
	var blob struct {
		Base64 *string `json:"base64"`
	}
	if err := json.Unmarshal(data, &blob); err == nil && blob.Base64 != nil {
		b, err := base64.StdEncoding.DecodeString(*blob.Base64)
		if err != nil {
			return err
		}
		a.Value = b
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	a.Value = v
	return nil
}

func toDriverArgs(args []d1Arg) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

func decodeD1Request(r *http.Request) (d1Request, error) {
	var req d1Request
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

type d1ExecResponse struct {
	LastInsertID int64 `json:"last_insert_id"`
	RowsAffected int64 `json:"rows_affected"`
}

func handleD1Exec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeD1Request(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db, err := getD1DB()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result, err := db.Exec(req.SQL, toDriverArgs(req.Args)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Not every statement produces a usable last_insert_id/rows_affected
	// (e.g. CREATE TABLE); the d1 driver's result.numberFromMeta returns
	// an error in that case rather than 0, so fall back to 0 for the JSON
	// response instead of failing the whole request.
	lastID, err := result.LastInsertId()
	if err != nil {
		lastID = 0
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		rowsAffected = 0
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(d1ExecResponse{
		LastInsertID: lastID,
		RowsAffected: rowsAffected,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type d1QueryResponse struct {
	Columns []string `json:"columns"`
	// Rows holds one []any per result row, in column order. A cell may be
	// nil, float64, string, or []byte (D1's BLOB type); encoding/json
	// renders []byte as a base64 string, so BLOB columns round-trip as
	// base64 the same way BLOB args do.
	Rows [][]any `json:"rows"`
}

func handleD1Query(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeD1Request(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db, err := getD1DB()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows, err := db.Query(req.SQL, toDriverArgs(req.Args)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resultRows := [][]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resultRows = append(resultRows, vals)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(d1QueryResponse{
		Columns: cols,
		Rows:    resultRows,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// d1BatchRequest is the body of POST /d1/batch: an ordered list of
// statements to run through d1.Batch (one D1Database.batch() call).
type d1BatchRequest struct {
	Statements []d1Request `json:"statements"`
}

type d1BatchStatementResponse struct {
	LastInsertID int64    `json:"last_insert_id"`
	RowsAffected int64    `json:"rows_affected"`
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	Meta         d1.Meta  `json:"meta"`
}

type d1BatchResponse struct {
	Results []d1BatchStatementResponse `json:"results"`
}

func handleD1Batch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req d1BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db, err := getD1DB()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stmts := make([]d1.Statement, len(req.Statements))
	for i, s := range req.Statements {
		stmts[i] = d1.Statement{Query: s.SQL, Args: toDriverArgs(s.Args)}
	}
	results, err := d1.Batch(r.Context(), db, stmts...)
	if err != nil {
		// The whole batch was rolled back by D1.
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := d1BatchResponse{Results: make([]d1BatchStatementResponse, len(results))}
	for i, res := range results {
		out := d1BatchStatementResponse{Columns: res.Columns, Rows: [][]any{}, Meta: res.Meta}
		// Not every statement has a usable last_row_id/changes; fall back
		// to 0 like handleD1Exec does.
		out.LastInsertID, _ = res.LastInsertId()
		out.RowsAffected, _ = res.RowsAffected()
		for _, row := range res.Rows {
			vals := make([]any, len(row))
			for j, v := range row {
				vals[j] = v
			}
			out.Rows = append(out.Rows, vals)
		}
		resp.Results[i] = out
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
