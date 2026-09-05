# workers (deprecated, moved to workers-go)

> [!IMPORTANT]
> This module has moved to **[github.com/syumai/workers-go](https://github.com/syumai/workers-go)**.
> See [syumai/workers-go#173](https://github.com/syumai/workers-go/issues/173) for the full rationale
> and migration plan.

`github.com/syumai/workers` is kept alive as a **forwarding module** ("mirror") so that existing
importers keep building. Every exported type, function, constant, and variable in this module is a
thin alias/wrapper over the corresponding symbol in `github.com/syumai/workers-go`:

```go
type Foo = workersgo.Foo
var Bar = workersgo.Bar
```

These forwarding files are regenerated and pushed here automatically as part of
[`github.com/syumai/workers-go`'s own release workflow](https://github.com/syumai/workers-go/blob/main/.github/workflows/release.yml):
right after a `workers-go` release is tagged, that workflow regenerates this module's forwarders
from the new tag, verifies them, and pushes the result here (over SSH, using a deploy key scoped
to this repository) together with a matching tag. This repository has **no workflows of its own** —
there is nothing here that runs on a schedule or reacts to activity in this repository. **Do not
send pull requests to this repository** — it contains no handwritten code, and any manual change
will be overwritten by the next sync. File issues and pull requests against
[syumai/workers-go](https://github.com/syumai/workers-go) instead.

## Migration

Update your import paths from `github.com/syumai/workers` to `github.com/syumai/workers-go`:

```sh
sed -i 's|github.com/syumai/workers|github.com/syumai/workers-go|g' $(grep -rl 'github.com/syumai/workers' --include='*.go' .)
```

On macOS, `sed -i` requires an explicit (possibly empty) backup suffix argument:

```sh
sed -i '' 's|github.com/syumai/workers|github.com/syumai/workers-go|g' $(grep -rl 'github.com/syumai/workers' --include='*.go' .)
```

Then update `go.mod`/`go.sum`:

```sh
go mod edit -droprequire=github.com/syumai/workers
go get github.com/syumai/workers-go@latest
go mod tidy
```

## Compatibility notes

* This module is versioned in lockstep with `workers-go`: `workers-go vX.Y.Z` ↔ `workers vX.Y.Z`.
* `cmd/workers-assets-gen` in this module is a stub; use
  `github.com/syumai/workers-go/cmd/workers-assets-gen` instead (see
  [`cmd/workers-assets-gen`](cmd/workers-assets-gen)).
* Sync from `workers-go` is push-based, driven entirely from `workers-go`'s release workflow; it
  normally lands within the same workflow run as the `workers-go` release. Old versions of this
  module are never broken by a failed sync — a failure there simply leaves this module on its
  previous tag until the next `workers-go` release (or a manual re-run) retries it.
* This module will eventually be frozen and archived once `workers-go` reaches a stable `v1.0.0`
  (or after a fixed maintenance window, whichever comes first) — see
  [syumai/workers-go#173](https://github.com/syumai/workers-go/issues/173) for the current policy.

## License

MIT, see [LICENSE.md](LICENSE.md).
