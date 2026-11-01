//go:build js && wasm

package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
)

// notebookClassName matches workers-assets-gen's -durable-objects flag and
// wrangler.jsonc's class_name for the NOTEBOOK binding.
const notebookClassName = "Notebook"

// notebook is a SQLite-backed Durable Object. Each HTTP path exercises one
// storage API and returns that call's raw result as JSON.
type notebook struct {
	state *durableobjects.DurableObjectState
}

func newNotebook(state *durableobjects.DurableObjectState, _ js.Value) (durableobjects.Object, error) {
	return &notebook{state: state}, nil
}

func (n *notebook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/sql":
		n.handleSQL(w, r)
	case "/db/exec":
		n.handleDBExec(w, r)
	case "/db/query":
		n.handleDBQuery(w, r)
	case "/txn":
		n.handleTxn(w, r)
	case "/block":
		n.handleBlock(w, r)
	case "/storage":
		n.handleStorage(w, r)
	default:
		http.NotFound(w, r)
	}
}

// sqlRequest is the body of POST /sql, /db/exec, and /db/query.
type sqlRequest struct {
	SQL  string   `json:"sql"`
	Args []sqlArg `json:"args"`
}

// sqlArg decodes one bind value. A one-key object selects a Go type the
// JSON encoding cannot represent on its own: {"base64":"..."} is []byte,
// {"time":"<RFC3339>"} is time.Time, and {"bool":true} is bool. JSON
// numbers that are whole integers become int64 so both SQLStorage.Exec and
// the database/sql driver see an integer bind; every other JSON value is
// left as encoding/json decoded it (string, float64, or nil).
type sqlArg struct {
	Value any
}

func (a *sqlArg) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		if len(obj) == 1 {
			if raw, ok := obj["base64"]; ok {
				var s string
				if err := json.Unmarshal(raw, &s); err != nil {
					return err
				}
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return err
				}
				a.Value = b
				return nil
			}
			if raw, ok := obj["time"]; ok {
				var s string
				if err := json.Unmarshal(raw, &s); err != nil {
					return err
				}
				t, err := time.Parse(time.RFC3339Nano, s)
				if err != nil {
					return err
				}
				a.Value = t
				return nil
			}
			if raw, ok := obj["bool"]; ok {
				var b bool
				if err := json.Unmarshal(raw, &b); err != nil {
					return err
				}
				a.Value = b
				return nil
			}
		}
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if f, ok := v.(float64); ok && f == math.Trunc(f) && !math.IsNaN(f) && !math.IsInf(f, 0) && f >= math.MinInt64 && f <= math.MaxInt64 {
		a.Value = int64(f)
		return nil
	}
	a.Value = v
	return nil
}

func decodeSQLRequest(r *http.Request) (sqlRequest, error) {
	var req sqlRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	return req, err
}

func sqlArgValues(args []sqlArg) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleSQL runs one statement through SQLStorage.Exec. read=one returns
// cursor.One() (exactly one row); otherwise it returns cursor.Rows(), which
// walks the cursor via ToArray.
func (n *notebook) handleSQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeSQLRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	bindings, err := jsSQLBindings(sqlArgValues(req.Args))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sqlStorage := n.state.Storage().SQL()
	cursor, err := sqlStorage.Exec(req.SQL, bindings...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// rowsRead counts rows the cursor has already produced, so it is read
	// after One/Rows consumes the cursor. rowsWritten is final as soon as
	// exec returns, but reading it afterwards matches that order.
	columns := cursor.ColumnNames()
	resp := map[string]any{
		"columns":       columns,
		"database_size": sqlStorage.DatabaseSize(),
	}
	if r.URL.Query().Get("read") == "one" {
		rowVal, err := cursor.One()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		row, err := jsValueToMap(rowVal)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp["row"] = row
	} else {
		rows, err := cursor.Rows()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		resp["rows"] = rows
	}
	resp["rows_read"] = cursor.RowsRead()
	resp["rows_written"] = cursor.RowsWritten()
	writeJSON(w, resp)
}

// jsSQLBindings converts bind values for SQLStorage.Exec. []byte becomes an
// ArrayBuffer (SqlStorage's blob binding type). time.Time is not a raw
// binding type; callers that want the driver's encoding use /db/exec.
func jsSQLBindings(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		switch x := a.(type) {
		case []byte:
			out[i] = arrayBufferFromBytes(x)
		case time.Time:
			return nil, errors.New("time bindings are only supported via /db/exec")
		default:
			out[i] = x
		}
	}
	return out, nil
}

func arrayBufferFromBytes(b []byte) js.Value {
	buf := js.Global().Get("ArrayBuffer").New(len(b))
	js.CopyBytesToJS(js.Global().Get("Uint8Array").New(buf), b)
	return buf
}

func jsValueToMap(v js.Value) (map[string]any, error) {
	if v.IsUndefined() || v.IsNull() {
		return nil, nil
	}
	s := js.Global().Get("JSON").Call("stringify", v)
	var row map[string]any
	if err := json.Unmarshal([]byte(s.String()), &row); err != nil {
		return nil, err
	}
	return row, nil
}

func (n *notebook) openDB() *sql.DB {
	db := n.state.Storage().SQL().OpenDB()
	// SqlStorage is one synchronous database, not a connection pool.
	db.SetMaxOpenConns(1)
	return db
}

type dbExecResponse struct {
	RowsAffected      int64  `json:"rows_affected"`
	LastInsertIDError string `json:"last_insert_id_error"`
}

func (n *notebook) handleDBExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeSQLRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db := n.openDB()
	defer db.Close()
	result, err := db.Exec(req.SQL, sqlArgValues(req.Args)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := dbExecResponse{RowsAffected: rowsAffected}
	if _, err := result.LastInsertId(); err != nil {
		resp.LastInsertIDError = err.Error()
	}
	writeJSON(w, resp)
}

type dbQueryResponse struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func (n *notebook) handleDBQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeSQLRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db := n.openDB()
	defer db.Close()
	rows, err := db.Query(req.SQL, sqlArgValues(req.Args)...)
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
	writeJSON(w, dbQueryResponse{Columns: cols, Rows: resultRows})
}

type txnPut struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type txnRequest struct {
	Mode string   `json:"mode"`
	Puts []txnPut `json:"puts"`
	ID   int64    `json:"id"`
	Name string   `json:"name"`
}

// handleTxn runs DurableObjectStorage.Transaction.
//
//	commit      txn.Put, then commit
//	error       txn.Put, then return an error (rollback)
//	rollback    txn.Put, then txn.Rollback
//	sql-commit  SQL INSERT inside the callback, then commit
//	sql-error   SQL INSERT inside the callback, then return an error
//
// On the SQLite backend, SQL run on storage during the callback joins the
// transaction; the txn handle itself is only required for the KV methods.
func (n *notebook) handleTxn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req txnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	storage := n.state.Storage()
	sqlMode := req.Mode == "sql-commit" || req.Mode == "sql-error"
	if sqlMode {
		if _, err := storage.SQL().Exec(`CREATE TABLE IF NOT EXISTS txn_items (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)`); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	result, err := storage.Transaction(func(txn *durableobjects.DurableObjectTransaction) (js.Value, error) {
		switch req.Mode {
		case "commit", "error", "rollback":
			for _, p := range req.Puts {
				if err := txn.Put(p.Key, js.ValueOf(p.Value), durableobjects.DurableObjectPutOptions{}); err != nil {
					return js.Value{}, err
				}
			}
			switch req.Mode {
			case "error":
				return js.Value{}, errors.New("intentional rollback")
			case "rollback":
				if err := txn.Rollback(); err != nil {
					return js.Value{}, err
				}
				return js.Value{}, errors.New("rollback returned without error")
			default:
				return js.ValueOf("committed"), nil
			}
		case "sql-commit", "sql-error":
			if _, err := storage.SQL().Exec(
				`INSERT INTO txn_items (id, name) VALUES (?, ?)`,
				req.ID, req.Name,
			); err != nil {
				return js.Value{}, err
			}
			if req.Mode == "sql-error" {
				return js.Value{}, errors.New("intentional rollback")
			}
			return js.ValueOf("committed"), nil
		default:
			return js.Value{}, errors.New("unknown mode " + req.Mode)
		}
	})

	resp := map[string]any{
		"result": "",
		"error":  "",
	}
	if err != nil {
		resp["error"] = err.Error()
	} else if result.Type() == js.TypeString {
		resp["result"] = result.String()
	}

	values := map[string]*string{}
	for _, p := range req.Puts {
		s, ok, err := storage.GetString(p.Key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			values[p.Key] = nil
			continue
		}
		copied := s
		values[p.Key] = &copied
	}
	resp["values"] = values

	if sqlMode {
		cursor, err := storage.SQL().Exec(`SELECT COUNT(*) AS n FROM txn_items`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows, err := cursor.Rows()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(rows) != 1 {
			http.Error(w, "COUNT(*) did not return one row", http.StatusInternalServerError)
			return
		}
		count, ok := rows[0]["n"].(float64)
		if !ok {
			http.Error(w, "COUNT(*) value was not a number", http.StatusInternalServerError)
			return
		}
		resp["sql_count"] = int(count)
	}
	writeJSON(w, resp)
}

type blockRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// handleBlock runs BlockConcurrencyWhile. The callback writes Key and
// returns Value; the response includes the callback's return value, the
// value read inside the callback, and the value read after it returns.
func (n *notebook) handleBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req blockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	storage := n.state.Storage()
	var inside string
	result, err := n.state.BlockConcurrencyWhile(func() (js.Value, error) {
		if err := storage.PutString(req.Key, req.Value); err != nil {
			return js.Value{}, err
		}
		s, ok, err := storage.GetString(req.Key)
		if err != nil {
			return js.Value{}, err
		}
		if !ok {
			return js.Value{}, errors.New("key missing inside blockConcurrencyWhile")
		}
		inside = s
		return js.ValueOf(req.Value), nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stored, ok, err := storage.GetString(req.Key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		stored = ""
	}
	writeJSON(w, map[string]any{
		"result": result.String(),
		"inside": inside,
		"stored": stored,
	})
}

type storageRequest struct {
	Op      string            `json:"op"`
	Key     string            `json:"key"`
	Value   string            `json:"value"`
	Keys    []string          `json:"keys"`
	Entries map[string]string `json:"entries"`
	Prefix  string            `json:"prefix"`
	NoCache bool              `json:"noCache"`
}

// handleStorage runs one generated DurableObjectStorage method (other than
// Transaction, which has its own path).
func (n *notebook) handleStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req storageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	storage := n.state.Storage()
	opts := durableobjects.DurableObjectPutOptions{}
	switch req.Op {
	case "get":
		s, ok, err := storage.GetString(req.Key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": ok, "value": s})
	case "put":
		if err := storage.PutString(req.Key, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "putMultiple":
		entries := make(map[string]js.Value, len(req.Entries))
		for k, v := range req.Entries {
			entries[k] = js.ValueOf(v)
		}
		if err := storage.PutMultiple(entries, opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "getMultiple":
		got, err := storage.GetMultiple(req.Keys, durableobjects.DurableObjectGetOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		values := make(map[string]string, len(got))
		for k, v := range got {
			values[k] = v.String()
		}
		writeJSON(w, map[string]any{"values": values})
	case "list":
		listed, err := storage.List(durableobjects.DurableObjectListOptions{
			Prefix:  req.Prefix,
			NoCache: req.NoCache,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		values := make(map[string]string, len(listed))
		for k, v := range listed {
			values[k] = v.String()
		}
		writeJSON(w, map[string]any{"values": values})
	case "delete":
		deleted, err := storage.Delete(req.Key, opts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"deleted": deleted})
	case "deleteMultiple":
		deleted, err := storage.DeleteMultiple(req.Keys, opts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"deleted": deleted})
	case "deleteAll":
		if err := storage.DeleteAll(opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "sync":
		if err := storage.Sync(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "setAlarm":
		when := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
		if err := storage.SetAlarm(when, durableobjects.DurableObjectSetAlarmOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"scheduled": when.UnixMilli()})
	case "getAlarm":
		got, ok, err := storage.Alarm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := map[string]any{"ok": ok}
		if ok {
			resp["scheduled"] = got.UTC().UnixMilli()
		}
		writeJSON(w, resp)
	case "deleteAlarm":
		if err := storage.DeleteAlarm(durableobjects.DurableObjectSetAlarmOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "unknown op "+req.Op, http.StatusBadRequest)
	}
}
