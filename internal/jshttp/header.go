package jshttp

import (
	"net/http"
	"strings"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// ToHeader converts JavaScript sides Headers to http.Header.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToHeader(headers js.Value) http.Header {
	entries := jsutil.ArrayFrom(headers.Call("entries"))
	headerLen := entries.Length()
	h := http.Header{}
	for i := 0; i < headerLen; i++ {
		entry := entries.Index(i)
		key := entry.Index(0).String()
		values := entry.Index(1).String()
		for _, value := range strings.Split(values, ",") {
			h.Add(key, value)
		}
	}
	return h
}

// ToJSHeader converts http.Header to JavaScript sides Headers.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToJSHeader(header http.Header) js.Value {
	h := jsutil.HeadersClass.New()
	for key, values := range header {
		for _, value := range values {
			h.Call("append", key, value)
		}
	}
	return h
}
