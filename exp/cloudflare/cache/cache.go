//go:build js && wasm

package cache

import "syscall/js"

// Caches returns the global CacheStorage, wrapping the runtime's caches
// object. Most callers want Caches().Default() (the same cache shared by
// fetch()) or Caches().Open(name) for a namespaced cache.
func Caches() *CacheStorage {
	return CacheStorageFromJS(js.Global().Get("caches"))
}
