//go:build js && wasm

package r2

import (
	"fmt"
	"sort"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeR2Entry is a single stored object in a fakeBucket.
type fakeR2Entry struct {
	key            string
	data           []byte
	etag           string
	version        string
	uploaded       time.Time
	httpMetadata   js.Value // as passed in PutOptions.httpMetadata; may be the zero js.Value (undefined)
	customMetadata js.Value // as passed in PutOptions.customMetadata; may be the zero js.Value (undefined)
}

// fakeR2Call records one call made to a fakeBucket.
type fakeR2Call struct {
	Method string
	Key    string
	Opts   js.Value
}

// fakeBucket is an in-memory fake of a Cloudflare Worker's R2Bucket
// instance (head/get/put/delete/list), built with syscall/js. See
// internal/jstest and the queues package's validatingProducer for the
// pattern this follows.
type fakeBucket struct {
	mu       sync.Mutex
	entries  map[string]*fakeR2Entry
	calls    []fakeR2Call
	nextID   int
	baseTime time.Time

	value js.Value
}

// newFakeBucket creates a fakeBucket. Its underlying js.Funcs are released
// via t.Cleanup (see jstest.Func).
func newFakeBucket(t testing.TB) *fakeBucket {
	t.Helper()

	fb := &fakeBucket{
		entries:  map[string]*fakeR2Entry{},
		baseTime: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	headFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		fb.record(fakeR2Call{Method: "head", Key: key})
		e := fb.lookup(key)
		if e == nil {
			return jstest.Resolved(jsutil.Null)
		}
		return jstest.Resolved(fb.entryToJS(e, false))
	})

	getFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		fb.record(fakeR2Call{Method: "get", Key: key})
		e := fb.lookup(key)
		if e == nil {
			return jstest.Resolved(jsutil.Null)
		}
		return jstest.Resolved(fb.entryToJS(e, true))
	})

	putFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		val := args[1]
		var opts js.Value
		if len(args) > 2 {
			opts = args[2]
		}

		data, ok := fakeR2ValueBytes(val)
		if !ok {
			t.Errorf("fakeBucket: put value has unsupported JS type %v", val.Type())
		}

		e := fb.put(key, data, opts)
		fb.record(fakeR2Call{Method: "put", Key: key, Opts: opts})
		return jstest.Resolved(fb.entryToJS(e, false))
	})

	deleteFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		key := args[0].String()
		fb.mu.Lock()
		delete(fb.entries, key)
		fb.mu.Unlock()
		fb.record(fakeR2Call{Method: "delete", Key: key})
		return jstest.Resolved(js.Undefined())
	})

	listFn := jstest.Func(t, func(_ js.Value, args []js.Value) any {
		var opts js.Value
		if len(args) > 0 {
			opts = args[0]
		}
		fb.record(fakeR2Call{Method: "list", Opts: opts})
		return jstest.Resolved(fb.listResult())
	})

	v := jsutil.NewObject()
	v.Set("head", headFn)
	v.Set("get", getFn)
	v.Set("put", putFn)
	v.Set("delete", deleteFn)
	v.Set("list", listFn)
	fb.value = v

	return fb
}

// fakeR2ValueBytes converts a value passed to put (a string or an
// ArrayBuffer, matching r2.Bucket.Put) into bytes.
func fakeR2ValueBytes(v js.Value) ([]byte, bool) {
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

func (fb *fakeBucket) record(c fakeR2Call) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.calls = append(fb.calls, c)
}

func (fb *fakeBucket) lookup(key string) *fakeR2Entry {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.entries[key]
}

// has reports whether key is currently stored, bypassing Bucket.Get/Head.
func (fb *fakeBucket) has(key string) bool {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	_, ok := fb.entries[key]
	return ok
}

func (fb *fakeBucket) put(key string, data []byte, opts js.Value) *fakeR2Entry {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	fb.nextID++
	e := &fakeR2Entry{
		key:      key,
		data:     data,
		etag:     fmt.Sprintf("etag-%d", fb.nextID),
		version:  fmt.Sprintf("v%d", fb.nextID),
		uploaded: fb.baseTime.Add(time.Duration(fb.nextID) * time.Second),
	}
	if !opts.IsUndefined() {
		e.httpMetadata = opts.Get("httpMetadata")
		e.customMetadata = opts.Get("customMetadata")
	}
	fb.entries[key] = e
	return e
}

// entryToJS builds the JS representation of e, matching the shape toObject
// reads: key, version, size, etag, httpEtag, uploaded (Date), httpMetadata,
// customMetadata, and (when includeBody) body (ReadableStream) and
// bodyUsed.
func (fb *fakeBucket) entryToJS(e *fakeR2Entry, includeBody bool) js.Value {
	obj := jsutil.NewObject()
	obj.Set("key", e.key)
	obj.Set("version", e.version)
	obj.Set("size", len(e.data))
	obj.Set("etag", e.etag)
	obj.Set("httpEtag", `"`+e.etag+`"`)
	obj.Set("uploaded", jsutil.TimeToDate(e.uploaded))

	if !e.httpMetadata.IsUndefined() {
		obj.Set("httpMetadata", e.httpMetadata)
	} else {
		obj.Set("httpMetadata", jsutil.NewObject())
	}
	if !e.customMetadata.IsUndefined() {
		obj.Set("customMetadata", e.customMetadata)
	} else {
		obj.Set("customMetadata", jsutil.NewObject())
	}

	if includeBody {
		obj.Set("body", jstest.ReadableStream(e.data))
		obj.Set("bodyUsed", false)
	}
	return obj
}

func (fb *fakeBucket) listResult() js.Value {
	fb.mu.Lock()
	keys := make([]string, 0, len(fb.entries))
	for k := range fb.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	objs := jsutil.NewArray(len(keys))
	for i, k := range keys {
		e := fb.entries[k]
		objs.SetIndex(i, fb.entryToJS(e, false))
	}
	fb.mu.Unlock()

	result := jsutil.NewObject()
	result.Set("objects", objs)
	result.Set("truncated", false)
	result.Set("delimitedPrefixes", jsutil.NewArray(0))
	return result
}
