package cache

import (
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

var cache = js.Global().Get("caches")

// Cache
type Cache struct {
	// instance - The object that Cache API belongs to.
	instance js.Value
}

// applyOptions applies client options.
func (c *Cache) applyOptions(opts []CacheOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// CacheOption
type CacheOption func(*Cache)

// WithNamespace
func WithNamespace(namespace string) CacheOption {
	return func(c *Cache) {
		v, err := jsutil.AwaitPromise(cache.Call("open", namespace))
		if err != nil {
			panic("failed to open cache")
		}
		c.instance = v
	}
}

func New(opts ...CacheOption) *Cache {
	c := &Cache{
		instance: cache.Get("default"),
	}
	c.applyOptions(opts)

	return c
}
