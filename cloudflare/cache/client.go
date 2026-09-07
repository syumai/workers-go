package cache

import (
	cachejs "github.com/syumai/workers-go/exp/cloudflare/cache"
)

// Cache
type Cache struct {
	// instance - The object that Cache API belongs to.
	instance *cachejs.Cache
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
		v, err := cachejs.Caches().Open(namespace)
		if err != nil {
			panic("failed to open cache")
		}
		c.instance = v
	}
}

func New(opts ...CacheOption) *Cache {
	c := &Cache{
		instance: cachejs.Caches().Default(),
	}
	c.applyOptions(opts)

	return c
}
