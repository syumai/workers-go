# workers-go

[![Go Reference](https://pkg.go.dev/badge/github.com/syumai/workers-go.svg)](https://pkg.go.dev/github.com/syumai/workers-go)
[![Discord Server](https://img.shields.io/discord/1095344956421447741?logo=discord&style=social)](https://discord.gg/tYhtatRqGs)

* `workers-go` is a Go module to run an HTTP server written in Go on [Cloudflare Workers](https://workers.cloudflare.com/). Its root package is `workers`.
* This package can easily serve *http.Handler* on Cloudflare Workers.
* Caution: This is an experimental project.

## Features

* [x] serve http.Handler
* [ ] R2
  - [x] Head
  - [x] Get
  - [x] Put
  - [x] Delete
  - [x] List
  - [ ] Options for R2 methods
* [ ] KV
  - [x] Get
  - [x] List
  - [x] Put
  - [x] Delete
  - [ ] Options for KV methods
* [x] Cache API
* [ ] Durable Objects
  - [x] Calling stubs
* [x] D1 (alpha)
* [x] Environment variables
* [x] FetchEvent
* [x] Cron Triggers
* [x] TCP Sockets
* [x] Inbound TCP Sockets (connect handler)
* [x] gRPC (over inbound TCP sockets, and over fetch via connect-go)
* [x] Queues
  - [x] Producer
  - [x] Consumer

## Installation

```
go get github.com/syumai/workers-go
```

### Migrating from `github.com/syumai/workers`

This module was published as `github.com/syumai/workers` up to v0.34.0 and was renamed to `github.com/syumai/workers-go` in v0.35.0 ([#173](https://github.com/syumai/workers-go/issues/173)). The old path still works: it is now a thin forwarding module that re-exports this module's API and follows its releases for a transition period, but it is marked deprecated. To switch, rewrite the import paths and tidy:

```
# Linux
find . \( -name '*.go' -o -name go.mod \) -exec sed -i 's|github.com/syumai/workers\([/" ]\)|github.com/syumai/workers-go\1|g' {} +
# macOS
find . \( -name '*.go' -o -name go.mod \) -exec sed -i '' 's|github.com/syumai/workers\([/" ]\)|github.com/syumai/workers-go\1|g' {} +
go mod tidy
```

## Usage

implement your http.Handler and give it to `workers.Serve()`.

```go
func main() {
	var handler http.HandlerFunc = func (w http.ResponseWriter, req *http.Request) { ... }
	workers.Serve(handler)
}
```

or just call `http.Handle` and `http.HandleFunc`, then invoke `workers.Serve()` with nil.

```go
func main() {
	http.HandleFunc("/hello", func (w http.ResponseWriter, req *http.Request) { ... })
	workers.Serve(nil) // if nil is given, http.DefaultServeMux is used.
}
```

For concrete examples, see `_examples` directory.

## Inbound TCP sockets and gRPC

Workers can also receive raw inbound TCP connections through the `connect()` handler. `github.com/syumai/workers-go/cloudflare/sockets` exposes the connection delivered to `connect()` as a `net.Listener`, so any Go server that accepts a `net.Listener` (`net/http`, `grpc.Server`, or a hand-rolled protocol) runs unmodified. Each TCP connection is one Worker invocation, so `sockets.Serve`/`sockets.Listen()` follow the same ready/block convention as `workers.Serve`:

```go
func main() {
	sockets.Serve(sockets.HandlerFunc(func(ctx context.Context, conn net.Conn) {
		io.Copy(conn, conn) // echo until the client closes
	}))
}
```

Because it's a `net.Listener`, this is also the recipe for gRPC on Workers, without a translation layer:

```go
func main() {
	mux := http.NewServeMux()
	mux.Handle(greetv1connect.NewGreeterServiceHandler(&greeter{}))
	workers.ServeNonBlock(mux) // fetch(): Connect / gRPC-Web / gRPC-over-HTTP

	var protos http.Protocols
	protos.SetHTTP1(true)
	protos.SetUnencryptedHTTP2(true) // h2c: gRPC clients connect with prior knowledge
	srv := &http.Server{Handler: mux, Protocols: &protos}
	log.Fatal(srv.Serve(sockets.Listen())) // connect(): native gRPC over the inbound socket
}
```

`grpc.Server.Serve(sockets.Listen())` works the same way if you'd rather bring [grpc-go](https://github.com/grpc/grpc-go) service implementations and interceptors.

`wrangler.jsonc` needs a `connect` trigger entry, and `wrangler dev` needs to be >= 4.125.0 to accept local TCP connections:

```jsonc
{
  "connect": [{ "protocol": "tcp", "port": 50051 }]
}
```

`connect()` also needs the `experimental` compatibility flag, but `wrangler deploy` rejects that flag in the config file (error code 10021), so pass it on the `wrangler dev` command line instead, and leave it out of `wrangler.jsonc`:

```
wrangler dev --compatibility-flags experimental
```

See the [`grpc-connect`](_examples/grpc-connect) (std `net/http` h2c + [connect-go](https://connectrpc.com/docs/go/getting-started), recommended) and [`grpc-go`](_examples/grpc-go) (grpc-go) examples, and the [`grpc-go` template](_templates/cloudflare/grpc-go), for full working projects.

Caveats:

* Inbound TCP sockets are currently in **private beta** for production deployments (Spectrum-fronted Workers); `wrangler dev` works without enrollment. `wrangler deploy` works with the config above as-is, but the `connect()` path is unreachable in production until your account is enrolled — the `fetch()` path (if the Worker has one) is unaffected.
* Inbound sockets are **not TLS-terminated** by the platform — clients (including gRPC clients) connect in plaintext (h2c), e.g. `grpcurl -plaintext`. Terminate TLS yourself with `crypto/tls` if you need it.
* One TCP connection = one Worker invocation. A long-lived gRPC connection keeps that invocation alive for as long as the client holds it open.
* gRPC (and any HTTP/2-based protocol) requires Go, not TinyGo: TinyGo's `net/http` has no HTTP/2 support.
* Go Wasm binaries with connect-go/grpc-go and protobuf are large (roughly 4-5MB gzip'd), which exceeds the Free plan's 3MB limit — a paid plan (10MB gzip'd) is required.

## Quick Start

* You can easily create and deploy a project from `Deploy to Cloudflare` button.

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https%3A%2F%2Fgithub.com%2Fsyumai%2Fworker-go-deploy)

* If you want to create a project manually, please follow the guide below.

### Requirements

* Node.js (and npm)
* Go 1.24.0 or later

### Create a new Worker project

Run the following command:

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/worker-go
```

After creating the project, follow the steps below to initialize it.

### Initialize the project

1. Navigate to your new project directory:

```console
cd my-app
```

2. Initialize Go modules:

```console
go mod init
go mod tidy
```

3. Start the development server:

```console
npm start
```

4. Verify the worker is running:

```console
curl http://localhost:8787/hello
```

You will see **"Hello!"** as the response.

If you want a more detailed description, please refer to the README.md file in the generated directory.

## FAQ

### How do I deploy a worker implemented in this package?

To deploy a Worker, the following steps are required.

* Create a worker project using [wrangler](https://developers.cloudflare.com/workers/wrangler/).
* Build a Wasm binary.
* Upload a Wasm binary with a JavaScript code to load and instantiate Wasm (for entry point).

The [worker-go template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-go) contains all the required files, so I recommend using this template.

But Go (not TinyGo) with many dependencies may exceed the size limit of the Worker (3MB for free plan, 10MB for paid plan). In that case, you can use the [TinyGo template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo) instead.

The TinyGo template requires TinyGo 0.42.0 or later. TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

### Where can I have discussions about contributions, or ask questions about how to use the library?

You can do both through GitHub Issues. If you want to have a more casual conversation, please use the [Discord server](https://discord.gg/tYhtatRqGs).
