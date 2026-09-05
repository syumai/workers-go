package cfruntimecontext

import (
	"errors"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

/**
 *  The type definition of RuntimeContext for Cloudflare Worker expects:
 *  ```ts
 *  type RuntimeContext {
 *    env: Env;
 *    ctx: ExecutionContext;
 *    ...
 *  }
 *  ```
 * This type is based on the type definition of ExportedHandlerFetchHandler.
 * - see: https://github.com/cloudflare/workers-types/blob/c8d9533caa4415c2156d2cf1daca75289d01ae70/index.d.ts#LL564
 */

// MustGetRuntimeContextEnv gets object which holds environment variables bound to Cloudflare worker.
// - see: https://github.com/cloudflare/workers-types/blob/c8d9533caa4415c2156d2cf1daca75289d01ae70/index.d.ts#L566
func MustGetRuntimeContextEnv() js.Value {
	return MustGetRuntimeContextValue("env")
}

// MustGetExecutionContext gets ExecutionContext object from context.
// - see: https://github.com/cloudflare/workers-types/blob/c8d9533caa4415c2156d2cf1daca75289d01ae70/index.d.ts#L567
// - see also: https://github.com/cloudflare/workers-types/blob/c8d9533caa4415c2156d2cf1daca75289d01ae70/index.d.ts#L554
func MustGetExecutionContext() js.Value {
	return MustGetRuntimeContextValue("ctx")
}

// MustGetRuntimeContextValue gets value for specified key from RuntimeContext.
// - if the value is undefined, this function panics.
func MustGetRuntimeContextValue(key string) js.Value {
	val, err := GetRuntimeContextValue(key)
	if err != nil {
		panic(err)
	}
	return val
}

var ErrValueNotFound = errors.New("execution context value for specified key not found")

// GetRuntimeContextValue gets value for specified key from RuntimeContext.
// - if the value is undefined, return error.
func GetRuntimeContextValue(key string) (js.Value, error) {
	runtimeObj := jsutil.RuntimeContext
	v := runtimeObj.Get(key)
	if v.IsUndefined() {
		return js.Value{}, ErrValueNotFound
	}
	return v, nil
}
