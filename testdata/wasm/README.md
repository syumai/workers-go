# testdata/wasm

This directory holds the `GOOS=js GOARCH=wasm` test runner used by `make test`
(and CI) to run `go test ./...` under Node.js. It lives under top-level
`testdata/` so the Go toolchain ignores it when building or testing this
module.

It is **generated** by `scripts/gen-wasm-exec` — run `make gen-wasm-exec` to
regenerate it. Do not hand-edit these files; edit the generator instead.

## Contents

- `wasm_exec.js` — Go's `lib/wasm/wasm_exec.js`, patched so `globalThis` is
  wrapped in a `Proxy` inside `run()`, adding a `context` object accessible as
  `globalThis.context`. This is required because
  `internal/jsutil/jsutil.go` reads `js.Global().Get("context").Get("binding")`
  at init.
- `wasm_exec_node.js` — Go's `lib/wasm/wasm_exec_node.js`, patched so that:
  - it no longer forwards the full `process.env` to Go (`go.env = ...`),
    since doing so can make Go fail with `total length of command line and
    environment variables exceeds limit`.
  - it calls `go.run(result.instance, { binding: {} })`, passing the
    `context` object that the patched `wasm_exec.js` above expects.
- `go_js_wasm_exec` — Go's `lib/wasm/go_js_wasm_exec`, copied verbatim and
  made executable.
