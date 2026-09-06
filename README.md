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
