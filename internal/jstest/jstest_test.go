//go:build js && wasm

package jstest

import (
	"bytes"
	"net/http"
	"sort"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// runtimeContextEnv reads globalThis.context.env the same way
// cloudflare/internal/cfruntimecontext does, without importing that
// cloudflare-internal package (internal/jstest is not rooted under
// cloudflare/, so it is not allowed to import it).
func runtimeContextEnv() js.Value {
	return jsutil.RuntimeContext.Get("env")
}

func TestSetRuntimeContext_restores(t *testing.T) {
	if !runtimeContextEnv().IsUndefined() {
		t.Fatalf("env should not be available before SetRuntimeContext")
	}

	t.Run("child", func(t *testing.T) {
		SetEnv(t, map[string]any{"FOO": "bar"})

		env := runtimeContextEnv()
		if env.IsUndefined() {
			t.Fatalf("env should be available after SetEnv")
		}
		if got := env.Get("FOO").String(); got != "bar" {
			t.Errorf("env.FOO = %q, want %q", got, "bar")
		}
	})

	// The child subtest's t.Cleanup must have restored the previous state
	// by the time it returns.
	if !runtimeContextEnv().IsUndefined() {
		t.Fatalf("env should have been restored after the child subtest")
	}
}

func TestExecutionContext_waitUntil(t *testing.T) {
	ec := NewExecutionContext(t)

	ran := false
	task := Func(t, func(_ js.Value, _ []js.Value) any {
		ran = true
		return js.Undefined()
	})

	// Simulate what cloudflare.WaitUntil does: register a Promise with
	// ctx.waitUntil whose executor runs the task (in a goroutine, exactly
	// like cloudflare.WaitUntil) and resolves once it's done. The Node
	// test runner drains all ready goroutines synchronously, so the task
	// may already have run by the time waitUntil returns; what matters is
	// that Wait reliably observes it having run, by actually awaiting the
	// recorded promise rather than assuming it settled already.
	promiseFn := Func(t, func(_ js.Value, args []js.Value) any {
		resolve := args[0]
		go func() {
			task.Invoke()
			resolve.Invoke(js.Undefined())
		}()
		return js.Undefined()
	})
	promise := jsutil.NewPromise(promiseFn)
	ec.Value().Call("waitUntil", promise)

	ec.Wait(t)
	if !ran {
		t.Errorf("task did not run by the time Wait returned")
	}
}

func TestExecutionContext_passThroughOnException(t *testing.T) {
	ec := NewExecutionContext(t)
	ec.Value().Call("passThroughOnException")
	ec.Value().Call("passThroughOnException")

	if got := ec.PassThroughCalls(); got != 2 {
		t.Errorf("PassThroughCalls() = %d, want 2", got)
	}
}

func TestAwait_rejected(t *testing.T) {
	_, err := await(Rejected("boom"))
	if err == nil {
		t.Fatalf("await(Rejected(...)) returned nil error, want non-nil")
	}
}

func TestAwait_resolved(t *testing.T) {
	got := Await(t, Resolved("ok"))
	if got.String() != "ok" {
		t.Errorf("Await(Resolved(ok)) = %v, want %q", got, "ok")
	}
}

func TestReadableStream_roundtrip(t *testing.T) {
	tests := map[string][][]byte{
		"empty":       {},
		"one_chunk":   {[]byte("hello")},
		"three_chunk": {[]byte("foo"), []byte("bar"), []byte("baz")},
	}
	for name, chunks := range tests {
		t.Run(name, func(t *testing.T) {
			var want []byte
			for _, c := range chunks {
				want = append(want, c...)
			}

			stream := ReadableStream(chunks...)
			got := ReadAll(t, stream)
			if !bytes.Equal(got, want) {
				t.Errorf("ReadAll() = %q, want %q", got, want)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	want := []byte("hello world")

	t.Run("uint8array", func(t *testing.T) {
		got := Bytes(t, Uint8Array(want))
		if !bytes.Equal(got, want) {
			t.Errorf("Bytes() = %q, want %q", got, want)
		}
	})

	t.Run("arraybuffer", func(t *testing.T) {
		ua := Uint8Array(want)
		ab := ua.Get("buffer")
		got := Bytes(t, ab)
		if !bytes.Equal(got, want) {
			t.Errorf("Bytes() = %q, want %q", got, want)
		}
	})
}

func TestRequest_Response(t *testing.T) {
	header := http.Header{}
	header.Set("X-Test", "1")
	body := []byte("request body")

	req := Request(t, http.MethodPost, "https://example.com/path", body, header)
	if got := req.Get("method").String(); got != http.MethodPost {
		t.Errorf("req.method = %q, want %q", got, http.MethodPost)
	}
	if got := req.Get("url").String(); got != "https://example.com/path" {
		t.Errorf("req.url = %q, want %q", got, "https://example.com/path")
	}
	if got := req.Get("headers").Call("get", "X-Test").String(); got != "1" {
		t.Errorf("req.headers.get(X-Test) = %q, want %q", got, "1")
	}
	gotBody := ReadAll(t, req.Get("body"))
	if !bytes.Equal(gotBody, body) {
		t.Errorf("request body = %q, want %q", gotBody, body)
	}

	respHeader := http.Header{}
	respHeader.Set("Content-Type", "text/plain")
	respBody := []byte("response body")
	resp := Response(t, 201, respBody, respHeader)
	if got := resp.Get("status").Int(); got != 201 {
		t.Errorf("resp.status = %d, want 201", got)
	}
	if got := resp.Get("headers").Call("get", "Content-Type").String(); got != "text/plain" {
		t.Errorf("resp.headers.get(Content-Type) = %q, want %q", got, "text/plain")
	}
	gotRespBody := ReadAll(t, resp.Get("body"))
	if !bytes.Equal(gotRespBody, respBody) {
		t.Errorf("response body = %q, want %q", gotRespBody, respBody)
	}
}

func TestAssertObjectEqual(t *testing.T) {
	obj := jsutil.NewObject()
	obj.Set("name", "foo")
	obj.Set("count", 3)
	obj.Set("ok", true)
	obj.Set("note", jsutil.Null)

	AssertObjectEqual(t, obj, map[string]any{
		"name":  "foo",
		"count": 3,
		"ok":    true,
		"note":  nil,
	})
}

func TestObjectDiff(t *testing.T) {
	obj := jsutil.NewObject()
	obj.Set("name", "foo")
	obj.Set("count", 3)
	obj.Set("extra", "unexpected")

	got := objectDiff(obj, map[string]any{
		"name":    "bar",
		"count":   3,
		"missing": "x",
	})
	sort.Strings(got)

	want := []string{
		`key "name": got foo, want "bar"`,
		`missing key "missing"`,
		`unexpected key "extra"`,
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("objectDiff() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("objectDiff()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestObjectDiff_undefined(t *testing.T) {
	got := objectDiff(js.Undefined(), map[string]any{"a": 1})
	if len(got) != 1 {
		t.Fatalf("objectDiff(undefined, ...) = %v, want exactly 1 diff", got)
	}
}

func TestReadyCount(t *testing.T) {
	// This package never calls the //go:wasmimport workers ready import
	// itself (that is exercised by the root package's own test,
	// TestReady_callsImport in handler_js_test.go, to avoid importing the
	// root package from an internal test-support package). Here we only
	// check the documented default.
	if got := ReadyCount(t); got != 0 {
		t.Errorf("ReadyCount() = %d, want 0", got)
	}
}

func TestBinding_missing(t *testing.T) {
	// Binding calls t.Fatalf on a missing binding; verify indirectly via
	// jsutil.Binding directly instead of calling Binding(t, ...), since we
	// cannot observe a t.Fatalf from within this test.
	if !jsutil.Binding.Get("does-not-exist").IsUndefined() {
		t.Fatalf("expected binding to be undefined")
	}
}
