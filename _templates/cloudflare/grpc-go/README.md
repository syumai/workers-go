# grpc-worker-template-go

- A template for starting a gRPC Cloudflare Worker project with Go.
- This template uses the [`workers-go`](https://github.com/syumai/workers-go) package's `cloudflare/sockets` package to serve gRPC over the Worker's inbound TCP socket (`connect()` handler), together with the standard library's `net/http` (h2c) and [connect-go](https://connectrpc.com/docs/go/getting-started).
- The same `http.Handler` also answers `fetch()` (Connect / gRPC-Web / Cloudflare-translated gRPC over HTTP), so you get both a native gRPC endpoint and an HTTP-friendly one from a single binary.
- See the [`grpc-connect`](https://github.com/syumai/workers-go/tree/main/_examples/grpc-connect) and [`grpc-go`](https://github.com/syumai/workers-go/tree/main/_examples/grpc-go) examples for more background and verification steps.

## Notice

- gRPC support requires Go (not TinyGo): TinyGo's `net/http` has no HTTP/2 support.
- Go Wasm binaries with connect-go/protobuf easily exceed the Free plan's Worker size limit (3MB gzip'd); you'll need a paid plan of Cloudflare Workers (10MB gzip'd).
- Inbound TCP sockets require the `experimental` compatibility flag (already set in `wrangler.jsonc`) and are currently in **private beta** for production deployments (Spectrum-fronted Workers). `wrangler dev` works without enrollment.
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
