# exp/cloudflare

`exp/cloudflare/<pkg>` holds Go bindings for Cloudflare Workers runtime APIs
that are mechanically generated from the `@cloudflare/workers-types` TypeScript
definitions. Each subpackage wraps one JS interface/class family: one JS
interface or class becomes one Go type, one method becomes one Go method,
one property becomes one Go getter (or struct field, for data types).

## Experimental contract

Everything under `exp/`, including this tree, is **experimental**:

* Its shape follows `@cloudflare/workers-types` directly, so it can gain,
  lose, or change fields and methods whenever `scripts/gen-bindings/ir/index.json`
  is regenerated from a newer `workers-types` release — including in a minor
  version release of this module.
* There is no attempt to hide Cloudflare's TypeScript API surface behind a
  more Go-idiomatic one here; the goal is coverage, not idiom. Prefer the
  hand-written packages under `cloudflare/` (`kv`, `r2`, `d1`, `queues`,
  `sockets`, ...) where one already exists for the API you need. Some of
  these (`kv`, `r2`, `cache`, and the producer side of `queues`) are
  themselves thin wrappers around a package here — see "A hand-written
  package wrapping a generated one" below — and can round out the
  generated API's rough edges; `cloudflare/kv`, for instance, returns a
  proper `kv.ErrNotFound` where the generated `KVNamespace.GetText` would
  otherwise hand back a nil `*string` for a missing key.
* Use `exp/cloudflare/<pkg>` directly for APIs that don't have a hand-written
  package yet, or reach into `JSValue()` on any generated handle type when
  the wrapper doesn't expose something you need.

`exp/cloudflare/doc.go` defines `WorkersTypesVersion`, the exact
`@cloudflare/workers-types` version the checked-in IR (and therefore every
generated package) was derived from.

## How it fits together

```
node_modules/@cloudflare/workers-types/<date>/index.d.ts   (not committed)
        │  scripts/gen-bindings/src/extract.ts (Node + TypeScript Compiler API)
        ▼
scripts/gen-bindings/ir/index.json                          (committed IR)
scripts/gen-bindings/ir/SOURCE                              (package@version + extraction date)
        │  scripts/gen-bindings/cfgen (Go) + exp/internal/gen/overrides/<pkg>.yaml
        ▼
exp/cloudflare/<pkg>/z<pkg>_gen.go                           (generated, DO NOT EDIT)
exp/cloudflare/<pkg>/<pkg>.go                                (hand-written, only where needed)
```

* **`scripts/gen-bindings/ir/index.json`** is a JSON dump of every top-level
  declaration (interface, class, type alias) in the `workers-types` `.d.ts`
  source, extracted once and committed. It is package/version-tagged by the
  sibling `SOURCE` file. Regenerating it requires Node.js 24+ and pnpm, but
  it's already committed — most contributors never need to touch it.
* **`scripts/gen-bindings/cfgen`** (a separate Go module, so its `yaml`
  dependency doesn't leak into this module's `go.sum`) reads that IR plus one
  overrides YAML file per output package, and writes
  `exp/cloudflare/<pkg>/z<pkg>_gen.go`. It only needs Go — no Node — so this
  step, and the `-check` mode used in CI, run without any JS toolchain.
* **`exp/internal/jsrt`** is the small runtime helper vocabulary the
  generated code calls into (`jsrt.Await`, `jsrt.Call`, `jsrt.Binding`,
  byte/typed-array/Date/Headers conversions, ...). It delegates to
  `internal/jsutil` and friends; it intentionally does not import anything
  under `exp/cloudflare` or `cloudflare/internal/...` so a future extraction
  of `exp/cloudflare` into its own module doesn't run into Go's `internal/`
  visibility rule.
* A package may also contain a hand-written `<pkg>.go` file, for anything
  cfgen can't express mechanically (an API that needs multiple statements to
  build, a bridge to another package's types, ...). See `hyperdrive.go`
  (`Hyperdrive.connect` is `handwritten:` in the overrides file; `Connect`
  bridges to `cloudflare/sockets.Connect` using `Host()`/`Port()` — see
  "Hyperdrive" below) and `cf` (`FromRequest`/`FromJS`, which read
  `request.cf` off a `*http.Request` — there's no IR declaration to
  generate those from at all) for examples.
* `durableobjects` generates the Durable Objects bindings
  (`DurableObjectState`, `DurableObjectStorage`, `DurableObjectID`,
  `DurableObjectNamespace`, `SQLStorage`/`SQLStorageCursor`); `storage.go`
  adds a few hand-written convenience helpers on top
  (`GetString`/`PutString`, `GetJSON`/`PutJSON`, `Alarm`, and
  `SQLStorageCursor.Rows`), `sql_driver.go` adds a `database/sql` driver
  (`SQLStorage.OpenDB`), and `websocket.go` adds
  `DurableObjectState.AcceptHibernatingConn`. `host.go` adds the separate,
  hand-written concern of *hosting* a Go type as a Durable Object class —
  see "Hosting a Go type as a Durable Object" below.
* `email` mixes both: `ForwardableEmailMessage` (the argument to a Worker's
  `email(message, env, ctx)` handler), `SendEmail` (the binding), and
  `EmailSendResult` are generated (`z email_gen.go`), but the outbound
  `EmailMessage` type (`NewEmailMessage`/`NewEmailMessageString` in
  `email.go`) is entirely hand-written: the real `cloudflare:email` module
  class isn't visible to extract.ts at all (only an ambient global
  `EmailMessage` *interface* stub with `from`/`to` getters is), so it's
  fetched from the runtime context the same way `cloudflare/sockets.Connect`
  fetches `connect()` — see `email.yaml`'s doc comment and
  `cmd/workers-assets-gen/assets/runtimes/cloudflare/runtime.mjs`'s
  `EmailMessage` entry. `email.Handle` registers the Worker's own
  `email(message, env, ctx)` export (added to
  `cmd/workers-assets-gen/assets/runtimes/cloudflare/worker.mjs`) the same way
  `cloudflare/queues.Consume` registers `queue`.
* `websocket` (`Upgrade`/`Conn`, `UpgradeHibernating`/`HibernatingConn`) has
  no `z<pkg>_gen.go` at all — it is entirely hand-written.
  `WebSocketPair`/`WebSocket` are `EventTarget`-based
  (`addEventListener("message"/"close"/"error")`), not the
  method-returns-Promise shape cfgen's IR→Go mapping expects, so there is no
  overrides file for it either. It lives under `exp/cloudflare` (rather than
  `cloudflare/`) purely because it wraps a runtime API with no other Go
  binding yet, not because any part of it is generated.
* `tail` generates `TraceItem` and its nested types, delivered to a Worker's
  `tail(events, env, ctx)` handler — i.e. a [Tail Worker](https://developers.cloudflare.com/workers/observability/logs/tail-workers/)
  consuming another Worker's execution traces via that Worker's
  `tail_consumers` config. `tail.Handle` registers the handler the same way
  `email.Handle` does. See "Tail Workers" below for `TraceItem.event`'s
  hand-written `EventKind()`/typed-accessor treatment.
* `secretsstore` generates `SecretsStoreSecret`, the [Secrets Store](https://developers.cloudflare.com/secrets-store/integrations/workers/)
  binding — a single async `Get() (string, error)`. Nothing hand-written.
* `dispatch` generates `DispatchNamespace`/`DynamicDispatchOptions`/
  `DynamicDispatchLimits`, the [Workers for Platforms dynamic dispatch](https://developers.cloudflare.com/cloudflare-for-platforms/workers-for-platforms/get-started/dynamic-dispatch/)
  binding; `dispatch.go` adds a hand-written `GetClient` — see "Dynamic
  dispatch" below.
* `workflows` now also generates `Workflow.CreateBatch` and
  `WorkflowInstance.SendEvent`. `sendEvent`'s parameter is a destructured
  object literal in the `.d.ts` (`{ type, payload }`), so `workflows.yaml`
  renames it with a `rename: "WorkflowInstance.sendEvent.<raw name>": event`
  entry before cfgen synthesizes the `WorkflowInstanceSendEventEvent` struct.
  This is the client-side handle for *calling into* a Workflow; `host.go`
  adds the separate, hand-written concern of *hosting* a Go type as the
  Workflow's own `run()` implementation — see "Hosting a Workflow" below.
  `WorkflowStep` (`step.do`/`sleep`/`sleepUntil`/`waitForEvent`) is entirely
  hand-written too, since `do`'s config-discriminated overloads don't fit
  cfgen's single-callback rule.
* `email` generates the builder overloads too: `SendEmail.SendBuilder` and
  `ForwardableEmailMessage.ReplyBuilder` take the flattened
  `EmailMessageBuilder`/`EmailReplyMessageBuilder` structs; their address and
  attachment fields stay `js.Value` (documented in `email.yaml`).
* `cloudflare/kv` (the hand-written package) exposes the bulk overload as
  `GetStrings(keys []string, opts *GetOptions) (map[string]string, error)`,
  wrapping the generated `GetTextMultiple`; keys with no value are omitted.

### A hand-written package wrapping a generated one

`cloudflare/kv` is the reference example of a *third* kind of package: a
hand-written L2 that wraps a generated L1 (`exp/cloudflare/kv`, imported as
`kvjs`) instead of extending it in place. `kv.Namespace` holds a
`*kvjs.KVNamespace`; each L2 method (`GetString`, `PutString`, `List`, ...)
builds the right `kvjs.KVNamespaceGetOptions`/`...PutOptions`/... value —
including the "text"/"json"/"arrayBuffer"/"stream" `Type` discriminant
`KVNamespace.get` needs — calls the matching generated method, and adapts
the result to L2's own, narrower, pre-existing API (e.g. turning a nil
`*string` from `GetText` into `kv.ErrNotFound`). `cloudflare/r2`, `cache`,
and `queues`' producer side (`Producer`, `MessageSendRequest`, ...) follow
the same pattern against `exp/cloudflare/{r2,cache,queues}`.

`cloudflare/dostub.go` (`DurableObjectNamespace`/`DurableObjectId`/
`DurableObjectStub`) is a variant of this: it predates
`exp/cloudflare/durableobjects` and keeps its own, pre-existing exported API
(`IdFromName` returning `*DurableObjectId` directly rather than
`(..., error)`, etc.), but internally delegates to
`durableobjects.DurableObjectNamespace`/`DurableObjectID` instead of calling
the JS methods itself.

## Hyperdrive

`exp/cloudflare/hyperdrive/hyperdrive.go` adds `(*Hyperdrive).Connect(ctx)
(net.Conn, error)`, bridging `Hyperdrive.connect()` — synchronous on the JS
side, and left `handwritten:` in `hyperdrive.yaml` for that reason — to
`cloudflare/sockets.Connect(ctx, net.JoinHostPort(h.Host(), ...), nil)`.
Use it as a `database/sql` driver's custom dialer, the same way
`_examples/mysql-blog-server`'s `app/handler.go` uses
`cloudflare/sockets.Connect` directly:

```go
mysql.RegisterDialContext("tcp", func(ctx context.Context, addr string) (net.Conn, error) {
	return h.Connect(ctx)
})
db, err := sql.Open("mysql", h.ConnectionString())
```

## Tail Workers

`exp/cloudflare/tail/tail.go` adds `tail.Handle(func(items []tail.TraceItem)
error)`, registering the Worker's `tail(events, env, ctx)` export the same
way `exp/cloudflare/email.Handle` registers `email`. See
`_examples/tail-worker` for a complete, runnable example (including the
`tail_consumers` config, which lives on the *producer* Worker's
`wrangler.toml`, not this one's).

`TraceItem.event` is a 10-way discriminated union (one branch per trigger
kind the producer Worker might have handled — `fetch`, `scheduled`,
`alarm`, `queue`, `email`, ...), which cfgen can't synthesize a single Go
type for without losing the discriminant — see `tail.yaml`'s doc comment.
It's generated as `js.Value`, and `TraceItem.EventKind() string` (hand-written
in `tail.go`) infers which branch it is from which of that branch's
distinguishing properties are present on the raw object, e.g. `"fetch"` for
one with a `request` property. A typed accessor is then available per
identifiable kind — `FetchEvent()`, `ScheduledEvent()`, `AlarmEvent()`,
`QueueEvent()`, `EmailEvent()`, `TailEvent()`, `JsRpcEvent()`,
`HibernatableWebSocketEvent()` — each returning `(*T, bool)`, `ok` true only
when `EventKind()` matches. `TraceItemConnectEventInfo` and
`TraceItemCustomEventInfo` carry no properties of their own, so `connect`
and `custom` events can't be told apart this way; both (and any
unrecognized shape) report `EventKind() == ""`.

`TraceItem.EventTimestamp` (a Unix millisecond timestamp) is `number |
null` in the source types, but cfgen's `types:` overrides only support a
`*int` pointer spelling, not `*float64` — see `tail.yaml`'s doc comment —
so it's generated as a plain `float64`, and `TraceItem.EventTime() (time.Time,
bool)` treats an `EventTimestamp` of exactly `0` as absent (indistinguishable
from a real 0 without an underlying js.Value to re-check, which `TraceItem`,
a plain data struct, doesn't retain).

## Dynamic dispatch

`exp/cloudflare/dispatch/dispatch.go` adds `(*DispatchNamespace).GetClient(name
string, opts *DynamicDispatchOptions) (*fetch.Client, error)`, bridging
`DispatchNamespace.get()` — which returns a `Fetcher`, left as `js.Value`
since `Fetcher` has no generated Go type of its own, the same way
`Request`/`Response` don't — to `cloudflare/fetch.NewClient(fetch.WithBinding(v))`,
the same pattern `hyperdrive.Hyperdrive.Connect` uses for its own
synchronous JS return value:

```go
ns, err := dispatch.NewDispatchNamespace("DISPATCHER")
client, err := ns.GetClient("customer-worker-123", nil)
resp, err := client.HTTPClient(fetch.RedirectModeFollow).Get("https://example.com/")
```

The plain generated `Get(name string, args map[string]any, options
DynamicDispatchOptions) (js.Value, error)` is still available directly for
the raw `Fetcher` value. `args` and `DynamicDispatchOptions.Outbound` are
both TypeScript index-signature object types (`{ [key: string]: any }`),
overridden to `map[string]any` in `dispatch.yaml` since cfgen's inline-object
synthesis doesn't recognize that shape as a `Record`-style map on its own.

## Hosting a Go type as a Durable Object

`exp/cloudflare/durableobjects/host.go` lets a Worker host a Go type as a
[Durable Object](https://developers.cloudflare.com/durable-objects/) class,
in addition to (or instead of) the regular `workers.Serve(handler)` fetch
path. See `_examples/durable-object-go` for a complete, runnable
example.

### The 1-instance-per-object model

Every other trigger this module supports (`fetch`, `scheduled`, `queue`,
`email`) follows the same shape: `worker.mjs`'s `run()` boots a *new* wasm
instance for every single trigger invocation, and that instance exits once
the trigger's response/promise settles (`workers.Serve`'s `Done()` channel
closes when the response body is fully read). A Durable Object breaks that
assumption on purpose: the whole point of a Durable Object is that its
in-memory state (and here, that includes whatever a Go program keeps in
memory, not just `ctx.storage`) survives across many `fetch`/`alarm`/
`webSocket*` triggers delivered to the same object over its lifetime.

So Durable Object hosting keeps **one wasm instance alive for one Durable
Object instance's whole lifetime**, and dispatches every trigger it
receives to the same Go value:

* `worker.mjs`'s `GoDurableObject` base class (which a generated subclass
  extends — see below) calls `run()` at most once per JS-side Durable
  Object instance, memoizing the resulting binding object; every
  `fetch`/`alarm`/`webSocket*` call on that instance reuses it instead of
  booting a new wasm instance.
* On the Go side, `durableobjects.Register(className, ctor)` records a
  `Constructor` for a class name (call it before `workers.Serve`, at the top
  of `main`). The first trigger any wasm instance receives looks up its
  `durableObject.className` (set by `GoDurableObject`) in that registry and
  calls the matching `Constructor` exactly once (guarded by a
  `sync.Once`-like mechanism); every later trigger on that same instance
  reuses the same `durableobjects.Object` value.
* Because of this, a Durable Object's `fetch` trigger does **not** close
  `workers.Done()` the way `workers.Serve`'s own fetch handler does — the Go
  program has to stay alive for the next trigger. (`durableobjects.Object`'s
  `ServeHTTP` is dispatched through the same `internal/jshttp.ServeRequest`
  helper `handler_js.go`'s `handleRequest` uses, just with `onBodyClosed`
  passed as `nil` instead of a callback that closes `Done()`.)
* A Worker can both host Durable Objects and serve regular HTTP traffic in
  the same binary: call `durableobjects.Register(...)` for each class, then
  call `workers.Serve(handler)` as usual. `workers.Serve` only blocks the
  *fetch-triggered* wasm instances; a Durable Object trigger dispatches
  through `durableobjects`' own registered handlers (`handleDurableObjectFetch`
  etc.) instead of ever calling into `workers.Serve`'s `handleRequest`, so
  the two don't interfere with each other — each trigger still gets its own
  wasm instance except for the "reuse across a Durable Object's lifetime"
  case described above.

### Wiring it up

1. Implement `durableobjects.Object` (just `http.Handler`) for your type,
   and optionally `durableobjects.AlarmHandler` /
   `WebSocketMessageHandler` / `WebSocketCloseHandler` /
   `WebSocketErrorHandler` for whichever other triggers you need — each is
   dispatched only if your type implements it; a trigger for one you didn't
   implement rejects with an error instead of silently doing nothing.
2. In `main`, before `workers.Serve` (or any other blocking call):
   ```go
   durableobjects.Register("Counter", func(state *durableobjects.DurableObjectState, env js.Value) (durableobjects.Object, error) {
       return &Counter{state: state}, nil
   })
   ```
   `durableobjects.State()` also returns the current instance's
   `*DurableObjectState` from anywhere in your handler's call graph.
3. Build with `-durable-objects=Counter` (comma-separate multiple classes):
   ```sh
   go run github.com/syumai/workers-go/cmd/workers-assets-gen -mode=go -durable-objects=Counter
   ```
   This appends one subclass definition per name to the generated
   `worker.mjs`:
   ```js
   export class Counter extends GoDurableObject { static goClassName = "Counter"; }
   ```
   The name passed here, the `class_name` in `wrangler.toml`'s
   `[[durable_objects.bindings]]`, and the `className` given to
   `durableobjects.Register` must all match exactly.
4. Add the binding and a migration to `wrangler.toml`:
   ```toml
   [[durable_objects.bindings]]
   name = "COUNTER"
   class_name = "Counter"

   [[migrations]]
   tag = "v1"
   new_sqlite_classes = ["Counter"]
   ```

### WebSocket hibernation

`WebSocketMessageHandler`/`WebSocketCloseHandler`/`WebSocketErrorHandler`
hand you a `*websocket.HibernatingConn` (`exp/cloudflare/websocket`), not
the plain `WebSocketPair`-based `Conn` that package's `Upgrade` builds —
these triggers fire for a connection accepted via the
[hibernatable WebSocket API](https://developers.cloudflare.com/durable-objects/best-practices/websockets/),
which is a different JS object shape and lifecycle (no in-process
`ReadMessage` loop; messages are delivered as separate triggers, possibly
to a different wasm instance once this one hibernates). To use it:

1. In `ServeHTTP`, call `websocket.UpgradeHibernating(w, r)` instead of
   `Upgrade` — it builds a `*HibernatingConn` without calling `accept()` on
   it — then `state.AcceptHibernatingConn(conn, tags...)` to register it and
   return immediately (no goroutine to start, unlike `Upgrade`'s read/write
   loop).
2. Implement `WebSocketMessageHandler` (and optionally
   `WebSocketCloseHandler`/`WebSocketErrorHandler`): `WebSocketMessage`
   receives the already-decoded `websocket.MessageType`/`[]byte`, the same
   way `Conn.ReadMessage` decodes its own `message` events.
   `DurableObjectState.GetWebSockets(tag)` + `websocket.HibernatingConnFromJS`
   list every connection sharing a tag, for broadcasting.

See `_examples/durable-object-websocket` (a broadcast chat room) for a
complete, runnable example, including
`cloudflare.DurableObjectStub.FetchWebSocket` — needed because the plain
`Fetch` reconstructs a `*http.Response` with nowhere to carry a 101
response's special `webSocket` pairing, so a Worker that doesn't host the
Durable Object itself needs that instead to forward an upgrade request to
one that does.

### Durable Object SQL driver

`exp/cloudflare/durableobjects/sql_driver.go` adds
`(*SQLStorage).OpenDB() *sql.DB`, a `database/sql` driver over
[`SqlStorage`](https://developers.cloudflare.com/durable-objects/api/sql-storage/)
(the embedded SQLite database `DurableObjectStorage.SQL()` returns),
mirroring `cloudflare/d1`: placeholders are `?`, bind values must be
`string`/`int64`/`float64`/`bool`/`[]byte`/`nil`/`time.Time` (encoded as an
RFC 3339 string), and transactions (`Begin`/`BeginTx`) are not currently
supported. Unlike `d1`, `LastInsertId` is also not supported (`SqlStorage`
has no equivalent of D1's `meta.last_row_id`), and every call runs
synchronously against `SqlStorage.exec` — there's no separate connection to
open.

```go
db := state.Storage().SQL().OpenDB()
defer db.Close()
_, err := db.Exec(`INSERT INTO todos (title) VALUES (?)`, title)
```

## Hosting a Workflow

`exp/cloudflare/workflows/host.go` lets a Worker host a Go type's `run()` as
a [Workflow](https://developers.cloudflare.com/workflows/)'s
`WorkflowEntrypoint`. See `_examples/workflow-go` for a complete, runnable
example.

Unlike a Durable Object, a Cloudflare Workflow's `WorkflowEntrypoint`
doesn't need to keep any state alive between triggers by itself — Workflows
persists step results and durably replays `run()` on its own — so hosting a
Workflow follows the regular "one wasm instance per trigger" shape every
other trigger in this module uses, not the Durable Object
one-instance-per-object model:

* `worker.mjs`'s `GoWorkflowEntrypoint` base class (which a generated
  subclass extends — a subclass is required here, since `WorkflowEntrypoint`
  is abstract) calls `run()` on a fresh wasm instance for the Workflow's
  `run(event, step)` trigger, passing a `workflow: { className }` runtime
  context entry.
* On the Go side, `workflows.Register(className, runner)` records a
  `Runner` for a class name (call it before `workers.Serve`, at the top of
  `main`). Every `run()` trigger looks up `workflow.className` in that
  registry and calls the matching `Runner`.
* `*workflows.Step` (`Do`/`DoWithConfig`/`DoJSON`/`DoJSONWithConfig`/
  `Sleep`/`SleepUntil`/`WaitForEvent`) wraps the JS `WorkflowStep` passed
  into `run()`: `Do`'s callback only runs if that step hasn't already
  completed for this Workflow instance — on a retry/replay, Workflows
  returns the saved result instead of calling it again. `DoJSON`/
  `DoJSONWithConfig` are generic helpers that JSON-encode/decode a step's
  result instead of working with a raw `js.Value`, the same way
  `durableobjects.DurableObjectStorage.GetJSON`/`PutJSON` do.
* Wrap a step (or `Runner`) error with `workflows.NonRetryable(err)` to fail
  the Workflow instance permanently instead of letting Workflows retry it —
  this rejects with the runtime's `cloudflare:workflows` `NonRetryableError`
  class instead of a plain `Error` (fetched from the runtime context the
  same way `email`'s `EmailMessage` is; unavailable under
  `runtime/browser.mjs`, where it falls back to a plain `Error`).

### Wiring it up

1. In `main`, before `workers.Serve` (or any other blocking call):
   ```go
   workflows.Register("MyWorkflow", func(ctx context.Context, event *workflows.Event, step *workflows.Step) (js.Value, error) {
       result, err := workflows.DoJSON(step, "step-1", func(ctx context.Context) (MyResult, error) {
           return MyResult{...}, nil
       })
       if err != nil {
           return js.Value{}, err
       }
       if err := step.Sleep("wait", time.Second); err != nil {
           return js.Value{}, err
       }
       return workflows.ResultJSON(result)
   })
   ```
2. Build with `-workflows=MyWorkflow` (comma-separate multiple classes):
   ```sh
   go run github.com/syumai/workers-go/cmd/workers-assets-gen -mode=go -workflows=MyWorkflow
   ```
   This appends one subclass definition per name to the generated
   `worker.mjs`:
   ```js
   export class MyWorkflow extends GoWorkflowEntrypoint { static goClassName = "MyWorkflow"; }
   ```
   The name passed here, the `class_name` in `wrangler.toml`'s
   `[[workflows]]`, and the `className` given to `workflows.Register` must
   all match exactly.
3. Add the binding to `wrangler.toml`:
   ```toml
   [[workflows]]
   name = "my-workflow"
   binding = "MY_WORKFLOW"
   class_name = "MyWorkflow"
   ```
   A Worker can create/inspect instances of the Workflow it hosts (or any
   other Workflow binding) through the client-side `workflows.Workflow`/
   `WorkflowInstance` L1 (`NewWorkflow("MY_WORKFLOW")` — see
   `zworkflows_gen.go`) — hosting a Workflow and calling into one are
   independent; a Worker can do either, both, or neither.

## RPC and named entrypoints

`exp/cloudflare/rpc` lets a Worker both *call* [Workers
RPC](https://developers.cloudflare.com/workers/runtime-apis/rpc/) methods on
a remote `WorkerEntrypoint` (a named entrypoint reached over a Service
binding, or a Durable Object's own methods beyond `fetch()`) and *host* its
own RPC methods as a `WorkerEntrypoint`. See `_examples/rpc-go` for a
complete, runnable example.

Workers RPC's typed stubs (TypeScript's `Rpc.Provider<T>`) can't be
generated for statically-typed Go — there's no way to derive a Go method set
from an arbitrary remote class's shape — so this package is entirely
hand-written (no generated L1) and dynamic on both sides:

* **Calling** a remote entrypoint: `rpc.NewStub(bindingName)` (or
  `rpc.StubFromJS` for an existing JS value, e.g. `cloudflare.
  DurableObjectStub.RPC()`) wraps a Service binding as a `*rpc.Stub`.
  `Stub.Call(method, args...)` invokes `method` (args converted the way
  `syscall/js.ValueOf` converts a Go value — primitives, `map[string]any`,
  `[]any`, `js.Value`, ... — to JS) and awaits its result (an RPC method's
  return is always a Promise). `Stub.CallJSON(method, &out, args...)`
  additionally JSON round-trips the result into a typed Go value, the same
  way `workflows.DoJSON` does for a step's result.
* **Hosting** RPC methods: `worker.mjs`'s `GoWorkerEntrypoint` base class
  (extended by a generated subclass — see below) forwards each RPC method
  call to `handleRPC(name, args)` and its own `fetch()` trigger to
  `handleEntrypointFetch(req)`, passing an `entrypoint: {className}` runtime
  context entry, mirroring `GoDurableObject`/`GoWorkflowEntrypoint`'s
  `#bind` pattern (exposed here as the non-private `_bind()`, since a
  generated subclass's own methods need to call it, and a JS private method
  is only reachable from methods defined in the class that declares it).
  On the Go side, `rpc.Register(className, map[string]rpc.Method)` and
  `rpc.RegisterFetch(className, http.Handler)` record the class's RPC
  methods and `fetch()` handler; every call looks up `entrypoint.className`
  in the registry. `rpc.Method` takes/returns raw `js.Value`s;
  `rpc.MethodJSON` adapts a typed function instead — since RPC arguments
  arrive positionally with no fixed arity known to the package, it hands the
  function each argument individually JSON-encoded (`[]json.RawMessage`) to
  decode however many/whatever shape the method expects, and JSON-encodes
  its typed result back into the resolved value.

### Wiring it up

1. In `main`, before `workers.Serve`:
   ```go
   rpc.Register("MyService", map[string]rpc.Method{
       "add": rpc.MethodJSON(func(ctx context.Context, args []json.RawMessage) (int, error) {
           var a, b int
           json.Unmarshal(args[0], &a)
           json.Unmarshal(args[1], &b)
           return a + b, nil
       }),
   })
   rpc.RegisterFetch("MyService", myFetchHandler) // optional; serves MyService's own fetch()
   ```
2. Build with `-entrypoints=MyService:add,greet` (semicolon-separate
   multiple classes; `Name` alone, with no `:methods`, is also valid — only
   the always-generated `fetch()` is defined then):
   ```sh
   go run github.com/syumai/workers-go/cmd/workers-assets-gen -mode=go -entrypoints=MyService:add,greet
   ```
   This appends one subclass definition per class to the generated
   `worker.mjs`:
   ```js
   export class MyService extends GoWorkerEntrypoint {
     static goClassName = "MyService";
     async add(...args) { return (await this._bind()).handleRPC("add", args); }
     async greet(...args) { return (await this._bind()).handleRPC("greet", args); }
     async fetch(req) { return (await this._bind()).handleEntrypointFetch(req); }
   }
   ```
   The name passed here, the `className` given to `rpc.Register`/
   `rpc.RegisterFetch`, and (for a Service binding) `wrangler.toml`'s
   `[[services]] entrypoint` must all match exactly.
3. Point a Service binding at it in `wrangler.toml` (including from the same
   Worker, to call its own entrypoint):
   ```toml
   [[services]]
   binding = "SELF"
   service = "rpc-go"
   entrypoint = "MyService"
   ```
   Then call it: `stub, _ := rpc.NewStub("SELF"); stub.Call("add", 1, 2)`.
   A Durable Object's own RPC methods work the same way, without a Service
   binding: `cloudflare.DurableObjectStub.RPC().Call(...)`.

## Regenerating the bindings

Most contributors never need to do this — the IR and all generated files are
committed. You only need to regenerate when:

* a newer `@cloudflare/workers-types` release should be picked up, or
* you're adding a new generated package or changing an existing one's
  overrides.

Requirements: **Node.js 24+** and **pnpm** (only for the `extract` step,
which runs the TypeScript Compiler API over `workers-types`' `.d.ts` file
using Node's built-in type stripping — no `ts-node`/`tsx` needed). Go 1.21+
for `cfgen` itself.

```sh
make gen-bindings        # pnpm install + extract -> IR, then cfgen -> Go
```

This is also wired up as the sole `//go:generate` directive in
`exp/cloudflare/doc.go`, so `go generate ./exp/cloudflare/...` (run from the
repo root) works too.

To regenerate only one package (faster, and useful while iterating on an
overrides file), pass `-pkg`:

```sh
go run -C scripts/gen-bindings ./cfgen -root "$(pwd)" -pkg workflows
```

### Checking generated output is up to date

```sh
make gen-bindings-check
```

This runs `cfgen -check`, which re-derives every package from the committed
IR and overrides and fails (non-zero exit, listing which files differ) if
the checked-in `z<pkg>_gen.go` files don't match. Unlike `make gen-bindings`,
this does **not** re-run the `extract` step (no Node/pnpm needed), which is
what lets it run in CI — the extract step re-derives the IR from
`node_modules`, and `-check` only needs the IR that's already committed.

`cfgen` prints a warning to stderr for every field/parameter/return value it
couldn't map to a precise Go type (see "Type mapping" below); these aren't
failures, just a reminder of what fell back to `js.Value`. It also prints
`info` lines (not counted as warnings) for a choice it *did* resolve
automatically but is worth being aware of — currently just a `union` with
exactly one member declared in the package's own `include:` list, which
picks that member (see the "union" row in "Type mapping" below). Both kinds
are also folded into the affected field/method's own generated doc comment.

## Writing an overrides file

Each generated package is driven by one `exp/internal/gen/overrides/<pkg>.yaml`
file. `<pkg>` becomes both the YAML's `package:` field and the output
directory name. cfgen validates every name referenced in the file against the
IR (and, for member/param names, against the include list) — an override that
refers to a declaration or member that doesn't exist is a hard generation
error, so the file can't silently drift from the IR.

```yaml
package: ratelimit                 # Go package name = output directory name
doc: "Package ratelimit ..."       # package doc comment (optional)
include:                           # declarations (IR names) to generate. Not
                                    # auto-transitive: list every type you
                                    # want a Go type for, even ones only
                                    # reached via another included type
  - RateLimit
  - RateLimitOptions
  - RateLimitOutcome
bindings:                          # types resolved from env; generates
                                    # New<Name>(bindingName string) (*Name, error)
                                    # (or (Name, error) for a data type)
  - RateLimit
rename:                            # override a generated name.
                                    # keys: "Decl", "Decl.member", or
                                    # "Decl.method.param"
  RateLimitOptions.key: Key
types:                             # override the Go type for one field,
                                    # method return, or method param.
                                    # keys: "Decl" (alias types only),
                                    # "Decl.member", "Decl.method.returns",
                                    # or "Decl.method.params.<name>".
                                    # supported values: js.Value, io.Reader,
                                    # http.Header, time.Time, string,
                                    # float32, float64, bool, int, *int,
                                    # []string/[]float32/... (any of the
                                    # above with a "[]" prefix),
                                    # map[string]any, or the name of another
                                    # declaration in this package's include:
                                    # (picks one branch of an otherwise-
                                    # unresolvable union)
  AnalyticsEngineDataPoint.indexes: "[]string"
  Vectorize.query.params.vector: "[]float32"
  Hyperdrive.connect.returns: "js.Value"
  R2GetOptions.onlyIf: "R2Conditional"
typeParams:                        # override the Go type chosen for a
                                    # declaration's (or one of its methods')
                                    # type parameter that has no resolvable
                                    # TS default and would otherwise fall
                                    # back to js.Value. keys: "Decl.Param".
                                    # Mostly documentation — an unoverridden
                                    # defaultless type parameter already
                                    # falls back to js.Value silently (no
                                    # warning either way): generics erasure
                                    # here is expected, not a fixable gap.
  KVNamespaceListKey.Metadata: js.Value
overloads:                         # split an overloaded method: each entry
                                    # names one overload, by its 0-based
                                    # position among same-named members, to
                                    # generate under a distinct Go name.
                                    # literal (optional) names a string-
                                    # literal-typed parameter to drop from
                                    # the Go signature and pass as a
                                    # constant instead. Overloads with no
                                    # entry are skipped, with a warning.
  KVNamespace.get:
    - { index: 5, name: GetText }
    - { index: 6, name: GetJSON }
handwritten:                       # skip generating this declaration/member;
                                    # you provide it in a hand-written file in
                                    # the same package instead
  - Hyperdrive.connect
exclude:                           # drop a specific member of an included
                                    # declaration (e.g. one cfgen can't
                                    # generate, or one you don't want yet)
  - ImagesBinding.hosted
optional:                          # "Decl.member" properties the .d.ts marks
                                    # required but the runtime omits. fromJS
                                    # skips undefined/null instead of panicking
                                    # (e.g. Value.Float). The Go field type is
                                    # unchanged; an absent number stays 0.
  - IncomingRequestCfProperties.clientTrustScore
```

A method whose only parameter is a callback shaped `(a: A) => Promise<U>` or
`() => Promise<U>` doesn't need any of the above: cfgen generates it as
`func(a *A) (U, error)` (or `func() (U, error)`; `U` void drops it to just
`error`) automatically, wrapping the Go closure as a JS function via
`jsrt.AsyncFunc` — see `DurableObjectStorage.Transaction` and
`DurableObjectState.BlockConcurrencyWhile` in `exp/cloudflare/durableobjects`.

### Adding a new generated package

1. Find the declaration name(s) you need in the committed IR:
   `jq '.decls[] | select(.name | test("MyThing")) | .name' scripts/gen-bindings/ir/index.json`.
   Inspect the full declaration with
   `jq '.decls[] | select(.name=="MyThing")' scripts/gen-bindings/ir/index.json`
   to see its members' types before writing overrides — this is much faster
   than iterating against generation errors.
2. Add `exp/internal/gen/overrides/mypkg.yaml` (package name = directory
   name) with at least `package:` and `include:`.
3. Run `go run -C scripts/gen-bindings ./cfgen -root "$(pwd)" -pkg mypkg` and
   read any warnings it prints — each one names the field/param that fell
   back to `js.Value` and why.
4. Add `types:`/`rename:`/`handwritten:`/`exclude:` entries to tighten up
   anything you want a more precise mapping for, and re-run.
5. `GOOS=js GOARCH=wasm go build ./exp/...` and
   `GOOS=js GOARCH=wasm go vet ./exp/...` to confirm the generated package
   compiles.
6. Add at least one `//go:build js && wasm` test in the new package,
   constructing a fake `js.Value` with `js.ValueOf(map[string]any{...})` the
   way `exp/cloudflare/ratelimit/ratelimit_test.go` does, and run it with
   `make test`.
7. `make gen-bindings-check` should now pass.

## Type mapping

| IR type | Go type | Conversion |
|---|---|---|
| `prim string` / `boolean` | `string` / `bool` | direct |
| `prim number` | `float64` (or `int` via `types:`) | direct (`.Int()` for the `int` override) |
| `prim bigint` | `int64` | via `js.Value.Int()` |
| `prim any` / `unknown` / `object` | `js.Value` | direct |
| `ref Promise<T>` | method return becomes `(T, error)` | `jsrt.Await` |
| `ref Array<T>` / `array` | `[]T` | loop |
| `ref Record<string, T>` / `index` | `map[string]T` | `Object.keys` loop |
| `ref Map<string, T>` | `map[string]T` (or `map[string]*T`/`map[string]js.Value` for `Map<string, T \| null>`, per the nullable-value row below) | `Array.from(v.keys())`/`v.get(k)` loop (a JS `Map`, unlike a `Record`, isn't a plain object) |
| a rest parameter (`...args: T[]`) | a Go variadic parameter (`args ...T`) | `T` is `any` -> `...any`, spread straight into `jsrt.Call`; otherwise each element is converted individually into a `[]any` ahead of the call |
| `ref ArrayBuffer` / `Uint8Array` | `[]byte` | `jsrt.BytesFromJS`/`BytesToJS` |
| `ref Float32Array` / `Float64Array` | `[]float32` / `[]float64` | `jsrt.Float32ArrayFromJS`/`ToJS` (and `Float64...`) |
| `ref ReadableStream<...>` | `io.ReadCloser` | `jsrt.ReadCloser` |
| `ref Date` | `time.Time` | `jsrt.DateToTime`/`TimeToDate` |
| `ref Headers` | `http.Header` | `jsrt.HeadersFromJS`/`HeadersToJS` (also available as a `types:` override spelling, to pick the `Headers` branch of an otherwise-unresolvable union, e.g. `images`' `HeadersInit`) |
| `ref Request` / `Response` | `js.Value` | direct (left as a raw escape hatch for hand-written L2 packages) |
| `ref` to a type in this package's `include:` | that type's Go type | `fromJS`/`toJS` for a data type, `FromJS`/`JSValue()` for a handle type |
| `ref` to a type *not* in `include:` | `js.Value` | direct, with a warning |
| an optional (`field?:`) or nullable (`field: X \| null`/`undefined`) data-type struct field whose type is itself a nested data-type struct — via a plain `ref`, a `types:` override naming one, or an inline object/union literal synthesized into one (see below) | `*X`, omitted from `toJS()` when nil and only allocated/decoded in `fromJS` when present | pointer-wrap |
| a single `literal` | its base type (`string`, ...) | direct |
| `union` of string literals and/or plain `string` | `string` (a named enum type + `const`s, if it's a top-level alias of only literals) | direct |
| `union` of `T \| null \| undefined` | `T` (treated as optional) | direct |
| `union` of numeric literals and/or plain `number` | `float64` | direct |
| `union` of `boolean`/boolean literals | `bool` | direct |
| `union` where every member is an object literal and/or a `ref` to a data-shaped declaration (in or out of `include:`) | one synthesized Go struct merging every member's fields (a field present in every branch with the same type stays required; present in only some, or boolean-ish in every branch, becomes optional/`bool`; genuinely conflicting types for the same field fail the whole union) | see "Inline object/union types" below |
| `union` of plain `ref`s where exactly one names a declaration in this package's `include:` | that declaration's Go type, with an `info` note (not a warning) on the field/method's doc comment | direct |
| an inline object type literal (`{ foo: string, ... }`), in field, getter, method-param, or method-return position | one synthesized Go struct (see below) | `fromJS`/`toJS` |
| any other `union` / `intersection` (non-data) / `function` / `typeParam` (with no default and no `typeParams:` override) / `unsupported` | `js.Value` | direct, with a warning (silent for `typeParam`; see `typeParams:` above) |

### Data-type flattening and inline object/union types

Data-type (property-only) declarations composed via TypeScript `extends` or
an `intersection` of other data shapes are flattened into one Go struct with
every field reachable directly on it (own fields win over inherited ones with
the same name) — see `exp/cloudflare/cf` for the most involved example
(`IncomingRequestCfProperties` merges five constituent interfaces, one of
which itself uses `extends`). The flattener also understands the TypeScript
utility types `Pick<T, K>`, `Omit<T, K>`, and `Partial<T>` as intersection/
extends operands (`K` a string literal or union of them) — see
`exp/cloudflare/vectorize`'s `VectorizeMatch`
(`Pick<Partial<VectorizeVector>, "values"> & Omit<VectorizeVector, "values">
& {score}`). An intersection with even one unresolvable operand (some other
TypeScript utility/mapped/conditional type, or a ref to something outside
the IR entirely) falls back to `js.Value` for the whole declaration rather
than silently keeping only the fields it could resolve — see `cf`'s
`IncomingRequestCfPropertiesTLSClientAuth`/`...Placeholder` union (two data
shapes whose fields are typed too differently, field by field, to unify) for
an example that still falls back this way.

An inline object type literal, or a union that resolves per the "union"
rows above, appearing at a field/getter/method-param/method-return position
is synthesized into its own named Go struct (with `fromJS`/`toJS`) rather
than falling back to `js.Value`. The name is `<enclosing Go type
name><field/param/method name, PascalCase>` — e.g.
`WorkflowInstanceCreateOptions.retention`'s `{ successRetention?, errorRetention?
}` becomes `WorkflowInstanceCreateOptionsRetention`. This applies
recursively (a field of a synthesized type can itself synthesize a further
nested one) and reuses one synthesized type across every position that
resolves to the exact same field (a data type's struct/`fromJS`/`toJS` all
resolve the same field independently). Two different fields that happen to
produce the same shape still each get their own type (no deduplication by
shape); a name collision with an unrelated, already-generated type (real or
synthesized) is disambiguated with a numeric suffix (`Foo`, `Foo2`, ...).

A method argument whose JS conversion needs more than one statement (e.g. a
Go slice becoming a JS `Array`) is still supported: cfgen spills it into a
local variable in its own scoped block ahead of the call, rather than
requiring a single inline expression. Return values are unaffected either
way, since decoding always happens into a pre-declared local.

## Naming

camelCase becomes PascalCase, with common initialisms upper-cased using a
fixed table (`id`→`ID`, `url`→`URL`, `ttl`→`TTL`, `http`→`HTTP`, `json`→`JSON`,
`ip`→`IP`, `tls`→`TLS`, `api`→`API`, `sql`→`SQL`, `db`→`DB`, `ai`→`AI`,
`cf`→`CF`, `uuid`→`UUID`, and a few more — see
`scripts/gen-bindings/cfgen/gen/naming.go`). Only words in the table are
replaced (`cacheTtl`→`CacheTTL`, but `asOrganization`→`AsOrganization`, not
`ASOrganization`, since `as` isn't in the table). `rename:` always wins over
the default. Names that collide with a Go keyword get a trailing `_`.
