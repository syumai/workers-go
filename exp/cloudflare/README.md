# exp/cloudflare

`exp/cloudflare/<pkg>` holds Go bindings for Cloudflare Workers runtime APIs
that are mechanically generated from the `@cloudflare/workers-types` TypeScript
definitions. Each subpackage wraps one JS interface/class family: one JS
interface or class becomes one Go type, one method becomes one Go method,
one property becomes one Go getter (or struct field, for data types).

## Experimental contract

Everything under `exp/`, including this tree, is **experimental**:

* Its shape follows `@cloudflare/workers-types` directly, so it can gain,
  lose, or change fields and methods whenever `exp/internal/gen/ir/index.json`
  is regenerated from a newer `workers-types` release — including in a minor
  version release of this module.
* There is no attempt to hide Cloudflare's TypeScript API surface behind a
  more Go-idiomatic one here; the goal is coverage, not idiom. Prefer the
  hand-written packages under `cloudflare/` (`kv`, `r2`, `d1`, `queues`,
  `sockets`, ...) where one already exists for the API you need.
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
exp/internal/gen/ir/index.json                              (committed IR)
exp/internal/gen/ir/SOURCE                                  (package@version + extraction date)
        │  scripts/gen-bindings/cfgen (Go) + exp/internal/gen/overrides/<pkg>.yaml
        ▼
exp/cloudflare/<pkg>/z<pkg>_gen.go                           (generated, DO NOT EDIT)
exp/cloudflare/<pkg>/<pkg>.go                                (hand-written, only where needed)
```

* **`exp/internal/gen/ir/index.json`** is a JSON dump of every top-level
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
  build, a bridge to another package's types, ...). See `hyperdrive`
  (`Hyperdrive.connect` is `handwritten:`, pending a bridge to
  `cloudflare/sockets`) and `cf` (`FromRequest`/`FromJS`, which read
  `request.cf` off a `*http.Request` — there's no IR declaration to
  generate those from at all) for examples.

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
failures, just a reminder of what fell back to `js.Value`.

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
                                    # supported values: js.Value, []string,
                                    # []float32, []float64, []bool, int,
                                    # *int, map[string]any
  AnalyticsEngineDataPoint.indexes: "[]string"
  Vectorize.query.params.vector: "[]float32"
  Hyperdrive.connect.returns: "js.Value"
overloads:                         # split an overloaded method (rare; not
                                    # currently used by any generated package)
  KVNamespace.get:
    by: "literal:1"                  # discriminate by the literal value of
                                      # params[1]
    names: { text: GetText, json: GetJSON }
handwritten:                       # skip generating this declaration/member;
                                    # you provide it in a hand-written file in
                                    # the same package instead
  - Hyperdrive.connect
exclude:                           # drop a specific member of an included
                                    # declaration (e.g. one cfgen can't
                                    # generate, or one you don't want yet)
  - Workflow.createBatch
```

### Adding a new generated package

1. Find the declaration name(s) you need in the committed IR:
   `jq '.decls[] | select(.name | test("MyThing")) | .name' exp/internal/gen/ir/index.json`.
   Inspect the full declaration with
   `jq '.decls[] | select(.name=="MyThing")' exp/internal/gen/ir/index.json`
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
| `ref ArrayBuffer` / `Uint8Array` | `[]byte` | `jsrt.BytesFromJS`/`BytesToJS` |
| `ref Float32Array` / `Float64Array` | `[]float32` / `[]float64` | `jsrt.Float32ArrayFromJS`/`ToJS` (and `Float64...`) |
| `ref ReadableStream<...>` | `io.ReadCloser` | `jsrt.ReadCloser` |
| `ref Date` | `time.Time` | `jsrt.DateToTime`/`TimeToDate` |
| `ref Headers` | `http.Header` | `jsrt.HeadersFromJS`/`HeadersToJS` |
| `ref Request` / `Response` | `js.Value` | direct (left as a raw escape hatch for hand-written L2 packages) |
| `ref` to a type in this package's `include:` | that type's Go type | `fromJS`/`toJS` for a data type, `FromJS`/`JSValue()` for a handle type |
| `ref` to a type *not* in `include:` | `js.Value` | direct, with a warning |
| an optional (`field?:`) or nullable (`field: X \| null`/`undefined`) data-type struct field whose type is itself a nested `include:`-ed data type | `*X`, omitted from `toJS()` when nil and only allocated/decoded in `fromJS` when present | pointer-wrap |
| a single `literal` | its base type (`string`, ...) | direct |
| `union` of all string literals | `string` (a named enum type + `const`s, if it's a top-level alias) | direct |
| `union` of `T \| null \| undefined` | `T` (treated as optional) | direct |
| any other `union` / `intersection` (non-data) / `function` / `typeParam` / `unsupported` | `js.Value` | direct, with a warning |

Data-type (property-only) declarations composed via TypeScript `extends` or
an `intersection` of other data shapes are flattened into one Go struct with
every field reachable directly on it (own fields win over inherited ones with
the same name) — see `exp/cloudflare/cf` for the most involved example
(`IncomingRequestCfProperties` merges five constituent interfaces, one of
which itself uses `extends`). An intersection with even one unresolvable
operand (a TypeScript utility type like `Pick`/`Omit`/`Partial`, or a ref to
something outside the IR) falls back to `js.Value` for the whole declaration
rather than silently keeping only the fields it could resolve — see
`exp/cloudflare/vectorize`'s `VectorizeMatch` for an example.

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
