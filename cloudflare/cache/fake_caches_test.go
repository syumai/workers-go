//go:build js && wasm

package cache

import (
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeCache is an in-memory fake of a single Workers Cache API Cache
// object (https://developers.cloudflare.com/workers/runtime-apis/cache/).
// Entries are keyed by request URL.
type fakeCache struct {
	t testing.TB

	mu      sync.Mutex
	entries map[string]js.Value // url -> Response value

	// lastMatchOpts / lastDeleteOpts record the options object passed to
	// the most recently observed match()/delete() call so tests can
	// assert it reached the fake (js.Undefined() if none was passed).
	lastMatchOpts  js.Value
	lastDeleteOpts js.Value
}

func newFakeCache(t testing.TB) *fakeCache {
	t.Helper()
	return &fakeCache{t: t, entries: map[string]js.Value{}}
}

func (c *fakeCache) entryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// value builds the JS object for this Cache. Call it exactly once per
// fakeCache: it is a plain object with fixed methods, not a live view, so
// building it twice would produce two independent JS objects.
func (c *fakeCache) value() js.Value {
	obj := jsutil.NewObject()
	obj.Set("put", jstest.Func(c.t, func(_ js.Value, args []js.Value) any {
		req, res := args[0], args[1]
		c.mu.Lock()
		c.entries[req.Get("url").String()] = res
		c.mu.Unlock()
		return jstest.Resolved(js.Undefined())
	}))
	obj.Set("match", jstest.Func(c.t, func(_ js.Value, args []js.Value) any {
		req := args[0]
		opts := js.Undefined()
		if len(args) > 1 {
			opts = args[1]
		}
		c.mu.Lock()
		c.lastMatchOpts = opts
		res, ok := c.entries[req.Get("url").String()]
		c.mu.Unlock()
		if !ok {
			return jstest.Resolved(js.Undefined())
		}
		return jstest.Resolved(res)
	}))
	obj.Set("delete", jstest.Func(c.t, func(_ js.Value, args []js.Value) any {
		req := args[0]
		opts := js.Undefined()
		if len(args) > 1 {
			opts = args[1]
		}
		url := req.Get("url").String()
		c.mu.Lock()
		c.lastDeleteOpts = opts
		_, ok := c.entries[url]
		delete(c.entries, url)
		c.mu.Unlock()
		return jstest.Resolved(ok)
	}))
	return obj
}

// fakeCaches is an in-memory fake of the top-level globalThis.caches object:
// a "default" cache plus named caches created on demand via open(name).
type fakeCaches struct {
	t   testing.TB
	def *fakeCache

	mu    sync.Mutex
	named map[string]*fakeCache
}

func newFakeCaches(t testing.TB) *fakeCaches {
	t.Helper()
	return &fakeCaches{t: t, def: newFakeCache(t), named: map[string]*fakeCache{}}
}

// namespace returns the fakeCache for name, creating it on first use. It is
// safe to call before or after open(name) has been invoked from JS: both
// resolve to the same *fakeCache for a given name.
func (fc *fakeCaches) namespace(name string) *fakeCache {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	c, ok := fc.named[name]
	if !ok {
		c = newFakeCache(fc.t)
		fc.named[name] = c
	}
	return c
}

// value builds the JS globalThis.caches replacement. Call it exactly once
// per fakeCaches (see fakeCache.value).
func (fc *fakeCaches) value() js.Value {
	obj := jsutil.NewObject()
	obj.Set("default", fc.def.value())
	obj.Set("open", jstest.Func(fc.t, func(_ js.Value, args []js.Value) any {
		name := args[0].String()
		c := fc.namespace(name)
		return jstest.Resolved(c.value())
	}))
	return obj
}

// installFakeCaches replaces the package-level cache variable with a fresh
// fakeCaches for the duration of the test, restoring it via t.Cleanup. It
// must not be combined with t.Parallel() (cache is shared package state).
func installFakeCaches(t *testing.T) *fakeCaches {
	t.Helper()
	fc := newFakeCaches(t)
	prev := cache
	cache = fc.value()
	t.Cleanup(func() { cache = prev })
	return fc
}
