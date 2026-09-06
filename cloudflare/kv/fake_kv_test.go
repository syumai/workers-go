//go:build js && wasm

package kv

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeKVEntry is a single stored value in a fakeKV.
type fakeKVEntry struct {
	data       []byte
	expiration int
}

// fakeKVCall records one call made to a fakeKV, for assertions on how the
// Namespace methods reach the underlying JS KVNamespace instance.
type fakeKVCall struct {
	Method string
	Key    string
	Opts   js.Value
}

// fakeKV is an in-memory fake of a Cloudflare Worker's KVNamespace instance
// (get/put/list/delete), built with syscall/js so it can stand in for the
// real binding in tests. See internal/jstest and the queues package's
// validatingProducer for the pattern this follows.
type fakeKV struct {
	mu      sync.Mutex
	entries map[string]*fakeKVEntry
	calls   []fakeKVCall

	value js.Value
}

// newFakeKV creates a fakeKV. Its underlying js.Funcs are released via
// t.Cleanup (see jstest.Func).
func newFakeKV(t testing.TB) *fakeKV {
	t.Helper()

	fk := &fakeKV{entries: map[string]*fakeKVEntry{}}

	getFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		var opts js.Value
		if len(args) > 1 {
			opts = args[1]
		}
		fk.record(fakeKVCall{Method: "get", Key: key, Opts: opts})

		e := fk.lookup(key)
		if e == nil {
			return jstest.Resolved(jsutil.Null)
		}

		type_ := "text"
		if !opts.IsUndefined() {
			if tv := opts.Get("type"); !tv.IsUndefined() {
				type_ = tv.String()
			}
		}
		switch type_ {
		case "stream":
			return jstest.Resolved(jstest.ReadableStream(e.data))
		default:
			return jstest.Resolved(string(e.data))
		}
	})

	putFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		val := args[1]
		var opts js.Value
		if len(args) > 2 {
			opts = args[2]
		}

		data, ok := fakeValueBytes(val)
		if !ok {
			t.Errorf("fakeKV: put value has unsupported JS type %v", val.Type())
		}

		exp := 0
		if !opts.IsUndefined() {
			if ev := opts.Get("expiration"); !ev.IsUndefined() {
				exp = ev.Int()
			}
		}

		fk.mu.Lock()
		fk.entries[key] = &fakeKVEntry{data: data, expiration: exp}
		fk.mu.Unlock()
		fk.record(fakeKVCall{Method: "put", Key: key, Opts: opts})

		return jstest.Resolved(js.Undefined())
	})

	deleteFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		fk.mu.Lock()
		delete(fk.entries, key)
		fk.mu.Unlock()
		fk.record(fakeKVCall{Method: "delete", Key: key})
		return jstest.Resolved(js.Undefined())
	})

	listFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		var opts js.Value
		if len(args) > 0 {
			opts = args[0]
		}
		fk.record(fakeKVCall{Method: "list", Opts: opts})
		return jstest.Resolved(fk.listResult(opts))
	})

	v := jsutil.NewObject()
	v.Set("get", getFn)
	v.Set("put", putFn)
	v.Set("delete", deleteFn)
	v.Set("list", listFn)
	fk.value = v

	return fk
}

// fakeValueBytes converts a value passed to put (a string or an
// ArrayBuffer, matching kv.Namespace's PutString/PutReader) into bytes.
func fakeValueBytes(v js.Value) ([]byte, bool) {
	if v.Type() == js.TypeString {
		return []byte(v.String()), true
	}
	arrayBuffer := js.Global().Get("ArrayBuffer")
	if v.InstanceOf(arrayBuffer) {
		ua := jsutil.Uint8ArrayClass.New(v)
		b := make([]byte, ua.Get("byteLength").Int())
		if len(b) > 0 {
			js.CopyBytesToGo(b, ua)
		}
		return b, true
	}
	return nil, false
}

func (fk *fakeKV) record(c fakeKVCall) {
	fk.mu.Lock()
	defer fk.mu.Unlock()
	fk.calls = append(fk.calls, c)
}

// calls returns a snapshot of the calls recorded so far.
func (fk *fakeKV) callsSnapshot() []fakeKVCall {
	fk.mu.Lock()
	defer fk.mu.Unlock()
	return append([]fakeKVCall(nil), fk.calls...)
}

func (fk *fakeKV) lookup(key string) *fakeKVEntry {
	fk.mu.Lock()
	defer fk.mu.Unlock()
	return fk.entries[key]
}

// has reports whether key is currently stored, bypassing Namespace's get
// (and its "<null>" quirk on a miss, see TestNamespace_GetString_missing).
func (fk *fakeKV) has(key string) bool {
	fk.mu.Lock()
	defer fk.mu.Unlock()
	_, ok := fk.entries[key]
	return ok
}

func (fk *fakeKV) listResult(opts js.Value) js.Value {
	fk.mu.Lock()
	names := make([]string, 0, len(fk.entries))
	for k := range fk.entries {
		names = append(names, k)
	}
	fk.mu.Unlock()
	sort.Strings(names)

	var prefix, cursor string
	var limit int
	if !opts.IsUndefined() {
		if v := opts.Get("prefix"); !v.IsUndefined() {
			prefix = v.String()
		}
		if v := opts.Get("limit"); !v.IsUndefined() {
			limit = v.Int()
		}
		if v := opts.Get("cursor"); !v.IsUndefined() {
			cursor = v.String()
		}
	}

	var filtered []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			filtered = append(filtered, n)
		}
	}

	start := 0
	if cursor != "" {
		if n, err := strconv.Atoi(cursor); err == nil {
			start = n
		}
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := len(filtered)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	page := filtered[start:end]

	fk.mu.Lock()
	keysArr := jsutil.NewArray(len(page))
	for i, n := range page {
		e := fk.entries[n]
		keyObj := jsutil.NewObject()
		keyObj.Set("name", n)
		if e != nil && e.expiration != 0 {
			keyObj.Set("expiration", e.expiration)
		}
		keysArr.SetIndex(i, keyObj)
	}
	fk.mu.Unlock()

	listComplete := end >= len(filtered)
	result := jsutil.NewObject()
	result.Set("keys", keysArr)
	result.Set("list_complete", listComplete)
	if !listComplete {
		result.Set("cursor", strconv.Itoa(end))
	}
	return result
}
