package jsmail

import (
	"net/mail"
	"net/textproto"
	"strings"
	"syscall/js"

	"github.com/syumai/workers/internal/jsutil"
)

// ToHeader converts JavaScript sides Headers to mail.Header.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToHeader(headers js.Value) mail.Header {
	entries := jsutil.ArrayFrom(headers.Call("entries"))
	headerLen := entries.Length()
	h := make(map[string][]string, headerLen)
	for i := 0; i < headerLen; i++ {
		entry := entries.Index(i)
		key := textproto.CanonicalMIMEHeaderKey(entry.Index(0).String())
		values := entry.Index(1).String()
		h[key] = strings.Split(values, ",")
	}
	return mail.Header(h)
}

// ToJSHeader converts mail.Header to JavaScript sides Headers.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToJSHeader(header mail.Header) js.Value {
	h := jsutil.HeadersClass.New()
	for key, values := range header {
		for _, value := range values {
			h.Call("append", key, value)
		}
	}
	return h
}
