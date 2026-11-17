package d1

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"syscall/js"

	d1js "github.com/syumai/workers-go/exp/cloudflare/d1"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Statement is a single SQL statement and its positional arguments, for use
// with Batch.
//
// Args accepts the same Go types as database/sql's default parameter
// conversion would for this driver: nil, bool, integers, floats, string and
// []byte (stored as BLOB). Anything with no JS form is rejected with an error
// before any statement is sent to D1.
type Statement struct {
	Query string
	Args  []any
}

// BatchResult is the outcome of one statement in a batch. It implements
// sql.Result and also exposes the rows returned by the statement (if any).
//
// Unlike rows returned by database/sql queries, batch rows come from
// D1Database.batch(), which returns each row as an object keyed by column
// name. Columns that share a name are therefore collapsed into a single
// column, and only one of their values is kept. For example,
//
//	SELECT orders.id, users.id, users.name FROM orders JOIN users ...
//
// yields the columns ["id", "name"], not ["id", "id", "name"]. This is a
// limitation of the D1 batch() API, and this package does not detect it. Give
// such columns distinct aliases in the query
// (SELECT orders.id AS order_id, users.id AS user_id, ...), or run the
// statement outside Batch with db.Query, which preserves duplicate names.
type BatchResult struct {
	// Columns are the column names of Rows, with duplicate names collapsed
	// (see BatchResult). It is nil if the statement returned no rows.
	Columns []string
	// Rows are the rows returned by the statement, each in Columns order.
	// Values use the same Go types as the database/sql driver
	// (nil, int64, float64, string, []byte).
	Rows [][]driver.Value
	// Meta is the metadata D1 reports for the statement: rows read and
	// written, changes, last row ID, duration, database size, which region
	// and colo served the query, and so on. See Meta.
	Meta Meta

	lastInsertID js.Value
	changes      js.Value
}

var _ sql.Result = (*BatchResult)(nil)

// Meta is the per-statement metadata D1 returns in the "meta" field of each
// result. Every field is the zero value if D1 did not report it (for example
// a local wrangler dev database reports fewer fields than a deployed one).
//
// See https://developers.cloudflare.com/d1/worker-api/return-object/
type Meta struct {
	// Duration is the time D1 spent executing the statement, in
	// milliseconds.
	Duration float64
	// SizeAfter is the database size in bytes after the statement ran.
	SizeAfter int64
	// RowsRead is the number of rows read (scanned) by the statement.
	RowsRead int64
	// RowsWritten is the number of rows written by the statement, including
	// index updates.
	RowsWritten int64
	// LastRowID is the ID of the last inserted row (meta.last_row_id).
	LastRowID int64
	// ChangedDB is true if the statement modified the database.
	ChangedDB bool
	// Changes is the number of rows changed by the statement
	// (meta.changes).
	Changes int64
	// ServedByRegion is the region of the database instance that ran the
	// statement.
	ServedByRegion string
	// ServedByColo is the three-letter airport code of the colo that ran
	// the statement.
	ServedByColo string
	// ServedByPrimary is true if the database instance that ran the
	// statement was the primary.
	ServedByPrimary bool
	// SQLDurationMs is meta.timings.sql_duration_ms: the time the database
	// instance spent executing the SQL, excluding network time. It is 0 if
	// D1 did not report timings.
	SQLDurationMs float64
	// TotalAttempts is the number of attempts D1 made to run the statement
	// because of automatic retries. All other fields describe only the last
	// attempt.
	TotalAttempts int64
}

// LastInsertId returns meta.last_row_id of the statement.
// If 'last_row_id' can't be retrieved, this method returns an error.
func (r *BatchResult) LastInsertId() (int64, error) {
	return intFromJS(r.lastInsertID, "last_row_id")
}

// RowsAffected returns meta.changes of the statement.
// If 'changes' can't be retrieved, this method returns an error.
func (r *BatchResult) RowsAffected() (int64, error) {
	return intFromJS(r.changes, "changes")
}

func intFromJS(v js.Value, key string) (int64, error) {
	if v.IsNull() || v.IsUndefined() {
		return 0, fmt.Errorf("d1: '%s' cannot be retrieved", key)
	}
	return int64(v.Int()), nil
}

// Batch sends all statements to D1 in a single D1Database.batch() call. D1
// runs the statements sequentially and non-concurrently, in the order given.
// If any statement fails, D1 aborts and rolls back the entire sequence (none
// of the statements take effect) and an error is returned along with no
// results.
//
// Sending several statements in one call can have a large performance impact,
// because it replaces one network round trip to D1 per statement with a
// single round trip for the whole batch. Prefer Batch over separate Exec or
// Query calls when running multiple statements together.
//
// The returned slice has one BatchResult per statement, in order. Batch with
// no statements returns (nil, nil) without calling D1.
//
// Rows returned by a batched SELECT collapse columns that share a name; see
// BatchResult for details and how to avoid it.
//
// See https://developers.cloudflare.com/d1/worker-api/d1-database/#batch
func Batch(ctx context.Context, db *sql.DB, stmts ...Statement) ([]BatchResult, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var results []BatchResult
	err = conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return errors.New("d1: Batch requires a *sql.DB opened with the d1 driver")
		}
		var err error
		results, err = c.Batch(ctx, stmts...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// Batch is like the package-level Batch but operates on a Conn directly.
func (c *Conn) Batch(ctx context.Context, stmts ...Statement) ([]BatchResult, error) {
	if len(stmts) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	db := d1js.D1DatabaseFromJS(c.dbObj)
	prepared := make([]*d1js.D1PreparedStatement, len(stmts))
	for i, s := range stmts {
		p, err := db.Prepare(s.Query)
		if err != nil {
			return nil, fmt.Errorf("d1: batch statement %d: %w", i, err)
		}
		if prepared[i], err = p.Bind(s.Args...); err != nil {
			return nil, fmt.Errorf("d1: batch statement %d: %w", i, err)
		}
	}

	rs, err := db.Batch(prepared)
	if err != nil {
		return nil, err
	}

	results := make([]BatchResult, len(rs))
	for i, r := range rs {
		res, err := batchResultFromJS(r)
		if err != nil {
			return nil, fmt.Errorf("d1: batch result %d: %w", i, err)
		}
		results[i] = res
	}
	return results, nil
}

func batchResultFromJS(r d1js.D1Result) (BatchResult, error) {
	res := BatchResult{}
	if r.Meta.Type() == js.TypeObject {
		res.lastInsertID = r.Meta.Get("last_row_id")
		res.changes = r.Meta.Get("changes")
		res.Meta = metaFromJS(r.Meta)
	}
	if len(r.Results) == 0 {
		return res, nil
	}
	cols, rows, err := decodeObjectRows(r.Results)
	if err != nil {
		return BatchResult{}, err
	}
	res.Columns, res.Rows = cols, rows
	return res, nil
}

// metaFromJS decodes D1's meta object. It is deliberately lenient: fields
// that are missing or of an unexpected type stay at their zero value, so a
// database that reports only some of them still yields a usable Meta.
func metaFromJS(v js.Value) Meta {
	num := func(m js.Value, key string) float64 {
		f := m.Get(key)
		if f.Type() != js.TypeNumber {
			return 0
		}
		return f.Float()
	}
	str := func(key string) string {
		f := v.Get(key)
		if f.Type() != js.TypeString {
			return ""
		}
		return f.String()
	}
	boolean := func(key string) bool {
		f := v.Get(key)
		return f.Type() == js.TypeBoolean && f.Bool()
	}
	m := Meta{
		Duration:        num(v, "duration"),
		SizeAfter:       int64(num(v, "size_after")),
		RowsRead:        int64(num(v, "rows_read")),
		RowsWritten:     int64(num(v, "rows_written")),
		LastRowID:       int64(num(v, "last_row_id")),
		ChangedDB:       boolean("changed_db"),
		Changes:         int64(num(v, "changes")),
		ServedByRegion:  str("served_by_region"),
		ServedByColo:    str("served_by_colo"),
		ServedByPrimary: boolean("served_by_primary"),
		TotalAttempts:   int64(num(v, "total_attempts")),
	}
	if t := v.Get("timings"); t.Type() == js.TypeObject {
		m.SQLDurationMs = num(t, "sql_duration_ms")
	}
	return m
}

// decodeObjectRows converts D1's `results` (an array of {column: value}
// objects) into column names and positional rows. Column order follows the
// key order of the first row object, which JS preserves from the SELECT list.
func decodeObjectRows(objs []js.Value) ([]string, [][]driver.Value, error) {
	keysArr := jsutil.ObjectClass.Call("keys", objs[0])
	cols := make([]string, keysArr.Length())
	for i := range cols {
		cols[i] = keysArr.Index(i).String()
	}
	rows := make([][]driver.Value, len(objs))
	for i, obj := range objs {
		row := make([]driver.Value, len(cols))
		for j, col := range cols {
			v, err := convertRowColumnValueToAny(obj.Get(col))
			if err != nil {
				return nil, nil, fmt.Errorf("row %d column %q: %w", i, col, err)
			}
			row[j] = v
		}
		rows[i] = row
	}
	return cols, rows, nil
}
