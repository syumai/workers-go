# grpc-go

- a gRPC Worker built with [grpc-go](https://github.com/grpc/grpc-go), served over the inbound TCP socket delivered to the Worker's `connect()` handler.
- uses `sockets.Listen()` (`github.com/syumai/workers-go/cloudflare/sockets`), which exposes the Worker's inbound socket as a `net.Listener`, so `grpc.Server.Serve` works unmodified.
- registers both `GreeterService` and the standard gRPC health service (`grpc.health.v1.Health`), plus [server reflection](https://pkg.go.dev/google.golang.org/grpc/reflection) so `grpcurl` can call methods without a local `.proto` file.
- use this example if you already have grpc-go service implementations/interceptors you want to bring to Workers; otherwise prefer [`grpc-connect`](../grpc-connect), which also serves gRPC-Web/Connect over `fetch` from the same binary.

## Development

### Requirements

- Go >= 1.21 (kept at `go 1.24` in `go.mod` for consistency with `grpc-connect`)
- Node.js
- wrangler >= 4.125.0 (inbound TCP sockets support for `wrangler dev`/`connect` config; installed locally via `npm install`, see `package.json`)
- [buf](https://buf.build/docs/installation/) (only needed if you change `proto/greet/v1/greet.proto` and want to regenerate `gen/`)
- [grpcurl](https://github.com/fullstorydev/grpcurl) (only needed to exercise the example manually)

### Paid plan note

The gzip'd Wasm binary for this example is around 4-5 MB (grpc-go + `x/net/http2` + protobuf), which exceeds the Free plan's 3 MB limit. A Paid plan (10 MB gzip'd limit) is required to deploy this example.

### Commands

```
npm install     # install wrangler locally (see Requirements)
npm run build   # build Go Wasm binary
npm run dev     # run dev server (npx wrangler dev)
npm run deploy  # deploy worker (npx wrangler deploy)
```

`npm run dev` starts `wrangler dev`, which listens for TCP connections on port 50052 (see `wrangler.jsonc`'s `connect` config) and forwards them to the Worker's `connect()` handler, where `grpc.Server.Serve` handles them.

### Verifying it works

```
$ grpcurl -plaintext -import-path proto -proto greet/v1/greet.proto \
    -d '{"name":"workers"}' localhost:50052 greet.v1.GreeterService/SayHello
{
  "name": "Hello, workers!"
}

$ grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check
{
  "status": "SERVING"
}
```

### Regenerating the protobuf/grpc code

`gen/` is generated with [buf](https://buf.build/) and committed, so `make build`/`make build-examples` need no protobuf tooling. After editing `proto/greet/v1/greet.proto`:

```
npm run generate   # runs `buf generate` (see buf.gen.yaml)
# or
make generate
```

## Notes

- Requires the `experimental` compatibility flag and a `connect` trigger entry in `wrangler.jsonc`.
- Inbound TCP sockets are currently in **private beta** for production deployments (Spectrum-fronted Workers); `wrangler dev` works without enrollment.
- Inbound sockets are **not TLS-terminated** by the platform: gRPC clients must connect in plaintext, e.g. `grpcurl -plaintext`, grpc-go's `insecure.NewCredentials()`.
- One TCP connection = one Worker invocation, for as long as the gRPC client holds the connection open. This example uses grpc-go's default keepalive/idle settings; if you need `wrangler dev` (or the runtime) to reclaim idle connections sooner, configure `grpc.NewServer(grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: ...}))`.
- gRPC over `connect()` needs Go (not TinyGo): TinyGo does not support `google.golang.org/grpc` or `golang.org/x/net/http2`.
