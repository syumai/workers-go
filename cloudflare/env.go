package cloudflare

import (
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Getenv gets a value of an environment variable.
//   - https://developers.cloudflare.com/workers/platform/environment-variables/
//   - This function panics when a runtime context is not found.
func Getenv(name string) string {
	return jsutil.MaybeString(cfruntimecontext.MustGetRuntimeContextEnv().Get(name))
}

// GetBinding gets a value of an environment binding.
//   - https://developers.cloudflare.com/workers/platform/bindings/about-service-bindings/
//   - This function panics when a runtime context is not found.
func GetBinding(name string) js.Value {
	return cfruntimecontext.MustGetRuntimeContextEnv().Get(name)
}
