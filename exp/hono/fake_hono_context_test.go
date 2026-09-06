package hono

import (
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeHonoContext is a JS object shaped like the subset of Hono's Context
// that this package's Context reads and writes (see context.go):
//   - req.raw: the standard Request object (Context.Request via ToRequest)
//   - header(key, value): Context.SetHeader
//   - status(code): Context.SetStatus
//   - body(bodyObj): Context.SetBody; returns a Response-like object, which
//     real Hono also assigns to its own res, so this fake does the same
//   - res: Context.RawResponse / Context.ResponseBody, and the value
//     Context.SetResponse assigns to directly
//
// Unlike Cloudflare's ExecutionContext (see jstest.ExecutionContext), this
// is read from context.ctx as the Hono Context, not the Workers
// ExecutionContext - see middleware.go's runHonoMiddleware and the package
// doc note on that asymmetry.
type fakeHonoContext struct {
	value js.Value

	headerFn js.Func
	statusFn js.Func
	bodyFn   js.Func

	mu          sync.Mutex
	headerCalls [][2]string
	statusCalls []int
	bodyCalls   []js.Value
}

// newFakeHonoContext creates a fakeHonoContext whose req.raw is reqObj. Its
// underlying js.Funcs are released via t.Cleanup.
func newFakeHonoContext(t testing.TB, reqObj js.Value) *fakeHonoContext {
	t.Helper()
	fc := &fakeHonoContext{}

	fc.headerFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		fc.mu.Lock()
		fc.headerCalls = append(fc.headerCalls, [2]string{args[0].String(), args[1].String()})
		fc.mu.Unlock()
		return js.Undefined()
	})
	fc.statusFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		fc.mu.Lock()
		fc.statusCalls = append(fc.statusCalls, args[0].Int())
		fc.mu.Unlock()
		return js.Undefined()
	})
	fc.bodyFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		body := args[0]
		fc.mu.Lock()
		fc.bodyCalls = append(fc.bodyCalls, body)
		fc.mu.Unlock()
		resp := jsutil.ResponseClass.New(body, jsutil.NewObject())
		fc.value.Set("res", resp)
		return resp
	})
	t.Cleanup(func() {
		fc.headerFn.Release()
		fc.statusFn.Release()
		fc.bodyFn.Release()
	})

	reqWrapper := jsutil.NewObject()
	reqWrapper.Set("raw", reqObj)

	v := jsutil.NewObject()
	v.Set("req", reqWrapper)
	v.Set("header", fc.headerFn)
	v.Set("status", fc.statusFn)
	v.Set("body", fc.bodyFn)
	v.Set("res", js.Undefined())
	fc.value = v

	return fc
}

// Value returns the underlying JS object to plug into
// jstest.RuntimeContext.Ctx.
func (fc *fakeHonoContext) Value() js.Value {
	return fc.value
}

func (fc *fakeHonoContext) HeaderCalls() [][2]string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([][2]string(nil), fc.headerCalls...)
}

func (fc *fakeHonoContext) StatusCalls() []int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([]int(nil), fc.statusCalls...)
}

func (fc *fakeHonoContext) BodyCalls() []js.Value {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([]js.Value(nil), fc.bodyCalls...)
}
