# grpc-connect

- a gRPC Worker built with the standard library's `net/http` (h2c) and [connect-go](https://connectrpc.com/docs/go/getting-started).
- one `http.Handler` serves **both** triggers from a single binary:
  - `fetch()`: Connect / gRPC-Web / (Cloudflare-translated) gRPC over HTTP, via `workers.ServeNonBlock`.
  - `connect()`: native gRPC over HTTP/2 (h2c, prior knowledge) on the inbound TCP socket, via `http.Server.Serve(sockets.Listen())` (`github.com/syumai/workers-go/cloudflare/sockets`).
- this is the recommended path for gRPC on Workers: it needs no `golang.org/x/net/http2`, and the same handler answers both the HTTP entry point and the raw TCP entry point.

## Development

### Requirements

- Go >= 1.24 (`net/http.Protocols` / `SetUnencryptedHTTP2` was added in 1.24)
- Node.js
- wrangler >= 4.125.0 (inbound TCP sockets support for `wrangler dev`/`connect` config; installed locally via `npm install`, see `package.json`)
- [buf](https://buf.build/docs/installation/) (only needed if you change `proto/greet/v1/greet.proto` and want to regenerate `gen/`)
- [grpcurl](https://github.com/fullstorydev/grpcurl) (only needed to exercise the example manually)

### Paid plan note

The gzip'd Wasm binary for this example is around 4-5 MB (connect-go + protobuf reflection on top of `net/http`'s own HTTP/2 stack), which exceeds the Free plan's 3 MB limit. A Paid plan (10 MB gzip'd limit) is required to deploy this example.

### Commands

```
npm install     # install wrangler locally (see Requirements)
npm run build   # build Go Wasm binary
npm run dev     # run dev server (npx wrangler dev --compatibility-flags experimental)
npm run deploy  # deploy worker (npx wrangler deploy)
```

`npm run dev` starts `wrangler dev --compatibility-flags experimental`, which:

- serves `fetch()` (Connect / gRPC-Web / gRPC-over-HTTP) on `http://localhost:8787`.
- listens for TCP connections on port 50051 (see `wrangler.jsonc`'s `connect` config) and forwards them to the Worker's `connect()` handler, where native gRPC (h2c) is served.

### Verifying it works

Native gRPC over the inbound TCP socket:

```
$ grpcurl -plaintext -import-path proto -proto greet/v1/greet.proto \
    -d '{"name":"workers"}' localhost:50051 greet.v1.GreeterService/SayHello
{
  "name": "Hello, workers!"
}
```

Bidirectional streaming, over the same socket:

```
$ printf '{"message":"a"}{"message":"b"}' | grpcurl -plaintext -import-path proto -proto greet/v1/greet.proto \
    -d @ localhost:50051 greet.v1.GreeterService/Chat
{
  "message": "echo: a"
}
{
  "message": "echo: b"
}
```

Connect protocol over `fetch` (same binary, same `http.Handler`, different trigger):

```
$ curl -s -X POST http://localhost:8787/greet.v1.GreeterService/SayHello \
    -H 'Content-Type: application/json' -d '{"name":"workers"}'
{"name":"Hello, workers!"}
```

### Regenerating the protobuf/connect code

`gen/` is generated with [buf](https://buf.build/) and committed, so `make build`/`make build-examples` need no protobuf tooling. After editing `proto/greet/v1/greet.proto`:

```
npm run generate   # runs `buf generate` (see buf.gen.yaml)
# or
make generate
```

## Deploying

`npm run deploy` (or `wrangler deploy`) works with `wrangler.jsonc` as-is — no changes needed. `wrangler deploy` rejects the `experimental` compatibility flag (error code 10021), so it's not in the config file; that also means the `connect()`/inbound-TCP path is not reachable in production unless your account is enrolled in the inbound-sockets private beta (Spectrum). The `fetch()` path is unaffected and is reachable at your `*.workers.dev` URL right away.

Connect protocol and gRPC-Web both work over HTTPS at the deployed URL:

```
$ buf curl --protocol grpcweb --schema proto -d '{"name":"x"}' \
    https://grpc-connect.<subdomain>.workers.dev/greet.v1.GreeterService/SayHello
{"name": "Hello, x!"}

$ curl -s -X POST https://grpc-connect.<subdomain>.workers.dev/greet.v1.GreeterService/SayHello \
    -H 'Content-Type: application/json' -d '{"name":"x"}'
{"name":"Hello, x!"}
```

Native gRPC (`--protocol grpc`) to a `*.workers.dev` URL returns HTTP 403: Cloudflare's gRPC→gRPC-Web translation is only available on zones with gRPC enabled, which requires a custom domain.

## Notes

- `connect()` requires the `experimental` compatibility flag and a `connect` trigger entry in `wrangler.jsonc`. `wrangler deploy` rejects the flag, so it's passed on the command line for local development only (see `npm run dev` above and `wrangler.jsonc`'s comment); `wrangler deploy` works as-is (see "Deploying" above).
- Inbound TCP sockets are currently in **private beta** for production deployments (Spectrum-fronted Workers); `wrangler dev` works without enrollment.
- Inbound sockets are **not TLS-terminated** by the platform: gRPC clients must connect in plaintext (h2c), e.g. `grpcurl -plaintext`, grpc-go's `insecure.NewCredentials()`.
- One TCP connection = one Worker invocation. A long-lived gRPC connection (multiplexing many RPCs) keeps that invocation alive for as long as the client holds the connection open.
- gRPC over `connect()` needs Go (not TinyGo): TinyGo's `net/http` has no HTTP/2 support.
