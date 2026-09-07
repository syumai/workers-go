package cache

import (
	"errors"
	"net/http"

	cachejs "github.com/syumai/workers-go/exp/cloudflare/cache"

	"github.com/syumai/workers-go/internal/jshttp"
)

// Put attempts to add a response to the cache, using the given request as the key.
// Returns an error for the following conditions
// - the request passed is a method other than GET.
// - the response passed has a status of 206 Partial Content.
// - Cache-Control instructs not to cache or if the response is too large.
// docs: https://developers.cloudflare.com/workers/runtime-apis/cache/#put
func (c *Cache) Put(req *http.Request, res *http.Response) error {
	return c.instance.Put(jshttp.ToJSRequest(req), jshttp.ToJSResponse(res))
}

// ErrCacheNotFound is returned when there is no matching cache.
var ErrCacheNotFound = errors.New("cache not found")

// MatchOptions represents the options of the Match method.
type MatchOptions struct {
	// IgnoreMethod - Consider the request method a GET regardless of its actual value.
	IgnoreMethod bool
}

func (opts *MatchOptions) toCacheJS() cachejs.CacheQueryOptions {
	if opts == nil {
		return cachejs.CacheQueryOptions{}
	}
	return cachejs.CacheQueryOptions{IgnoreMethod: opts.IgnoreMethod}
}

// Match returns the response object keyed to that request.
// docs: https://developers.cloudflare.com/workers/runtime-apis/cache/#match
func (c *Cache) Match(req *http.Request, opts *MatchOptions) (*http.Response, error) {
	res, err := c.instance.Match(jshttp.ToJSRequest(req), opts.toCacheJS())
	if err != nil {
		return nil, err
	}
	if res.IsUndefined() {
		return nil, ErrCacheNotFound
	}
	return jshttp.ToResponse(res)
}

// DeleteOptions represents the options of the Delete method.
type DeleteOptions struct {
	// IgnoreMethod - Consider the request method a GET regardless of its actual value.
	IgnoreMethod bool
}

func (opts *DeleteOptions) toCacheJS() cachejs.CacheQueryOptions {
	if opts == nil {
		return cachejs.CacheQueryOptions{}
	}
	return cachejs.CacheQueryOptions{IgnoreMethod: opts.IgnoreMethod}
}

// Delete removes the Response object from the cache.
// This method only purges content of the cache in the data center that the Worker was invoked.
// Returns ErrCacheNotFount if the response was not cached.
func (c *Cache) Delete(req *http.Request, opts *DeleteOptions) error {
	ok, err := c.instance.Delete(jshttp.ToJSRequest(req), opts.toCacheJS())
	if err != nil {
		return err
	}
	if !ok {
		return ErrCacheNotFound
	}
	return nil
}
