# grpc-worker-template-go

- A template for starting a gRPC Cloudflare Worker project with Go.
- This template uses the [`workers-go`](https://github.com/syumai/workers-go) package's `cloudflare/sockets` package to serve gRPC over the Worker's inbound TCP socket (`connect()` handler), together with the standard library's `net/http` (h2c) and [connect-go](https://connectrpc.com/docs/go/getting-started).
- The same `http.Handler` also answers `fetch()` (Connect / gRPC-Web / Cloudflare-translated gRPC over HTTP), so you get both a native gRPC endpoint and an HTTP-friendly one from a single binary.
- See the [`grpc-connect`](https://github.com/syumai/workers-go/tree/main/_examples/grpc-connect) and [`grpc-go`](https://github.com/syumai/workers-go/tree/main/_examples/grpc-go) examples for more background and verification steps.

## Notice

- gRPC support requires Go (not TinyGo): TinyGo's `net/http` has no HTTP/2 support.
- Go Wasm binaries with connect-go/protobuf easily exceed the Free plan's Worker size limit (3MB gzip'd); you'll need a paid plan of Cloudflare Workers (10MB gzip'd).
- Inbound TCP sockets require the `experimental` compatibility flag and are currently in **private beta** for production deployments (Spectrum-fronted Workers). `wrangler deploy` rejects that flag (error code 10021), so it's not in `wrangler.jsonc`; it's passed on the command line for `wrangler dev` instead (already wired up in `npm start`/`npm run dev`, see `wrangler.jsonc`'s comment). `wrangler dev` works without enrollment; `wrangler deploy` works with the config as-is, but the `connect()`/inbound-TCP path is only reachable in production once your account is enrolled.
- Inbound sockets are **not TLS-terminated** by the platform: gRPC clients must connect in plaintext (h2c), e.g. `grpcurl -plaintext`.
- `wrangler` >= 4.125.0 is required for local `connect` (inbound TCP socket) support in `wrangler dev`.

## Usage

- `main.go` includes a `GreeterService` implementation (unary `SayHello`, bidi-streaming `Chat`). Feel free to edit `proto/greet/v1/greet.proto` and `main.go` to implement your own gRPC service.
- The generated protobuf/connect code lives in `gen/` and is checked in, so a normal `npm run build` needs no protobuf tooling. Regenerate it with `npm run generate` (runs `buf generate`, requires [buf](https://buf.build/docs/installation/)) after editing the `.proto` file.

## Requirements

- Node.js
- Go 1.24.0 or later
- [buf](https://buf.build/docs/installation/) (only needed to regenerate `gen/`)

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/grpc-go
```

- Initialize a project. The generated code in `gen/` was produced with the Go module path `grpc-go` (see `gen/greet/v1/greet.pb.go`'s import path and `proto/greet/v1/greet.proto`'s `go_package` option), so `go mod init` must use the same name unless you regenerate `gen/` with `npm run generate` after changing it.

```console
cd my-app
go mod init grpc-go
go mod tidy
npm start # start running dev server
```

## Development

### Commands

```
npm start      # run dev server (fetch() on :8787, connect() TCP on :50051)
npm run build  # build Go Wasm binary
npm run deploy # deploy worker
npm run generate # regenerate gen/ from proto/greet/v1/greet.proto with buf
```

`npm start`/`npm run dev` runs `wrangler dev --compatibility-flags experimental` — the flag is required by `connect()` but is passed on the command line, not in `wrangler.jsonc`, because `wrangler deploy` rejects it.

### Testing dev server

- Native gRPC over the inbound TCP socket (requires [grpcurl](https://github.com/fullstorydev/grpcurl)):

```console
$ grpcurl -plaintext -import-path proto -proto greet/v1/greet.proto \
    -d '{"name":"workers"}' localhost:50051 greet.v1.GreeterService/SayHello
{
  "name": "Hello, workers!"
}
```

- Connect protocol over `fetch`:

```console
$ curl -s -X POST http://localhost:8787/greet.v1.GreeterService/SayHello \
    -H 'Content-Type: application/json' -d '{"name":"workers"}'
{"name":"Hello, workers!"}
```

## Deploying

`npm run deploy` (or `wrangler deploy`) works with `wrangler.jsonc` as-is — no changes needed. Because `wrangler deploy` rejects the `experimental` compatibility flag, the `connect()`/inbound-TCP path is not reachable in production unless your account is enrolled in the inbound-sockets private beta (Spectrum). The `fetch()` path is unaffected and is reachable at your `*.workers.dev` URL right away.

Connect protocol and gRPC-Web both work over HTTPS at the deployed URL:

```console
$ buf curl --protocol grpcweb --schema proto -d '{"name":"x"}' \
    https://<name>.<subdomain>.workers.dev/greet.v1.GreeterService/SayHello
{"name": "Hello, x!"}

$ curl -s -X POST https://<name>.<subdomain>.workers.dev/greet.v1.GreeterService/SayHello \
    -H 'Content-Type: application/json' -d '{"name":"x"}'
{"name":"Hello, x!"}
```

Native gRPC (`--protocol grpc`) to a `*.workers.dev` URL returns HTTP 403: Cloudflare's gRPC→gRPC-Web translation is only available on zones with gRPC enabled, which requires a custom domain.
