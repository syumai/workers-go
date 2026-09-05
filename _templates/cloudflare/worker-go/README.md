# worker-template-go

- A template for starting a Cloudflare Worker project with Go.
- This template uses [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server.

## Notice

- Go (not TinyGo) with many dependencies may exceed the size limit of the Worker (3MB for free plan, 10MB for paid plan). In that case, you can use the [TinyGo template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo) instead.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Node.js
- Go 1.24.0 or later

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/worker-go
```

- Initialize a project.

```console
cd my-app
go mod init
go mod tidy
npm start # start running dev server
curl http://localhost:8787/hello # outputs "Hello!"
```

## Development

### Commands

```
npm start      # run dev server
# or
go run .       # run dev server without Wrangler (Cloudflare-related features are not available)
npm run build  # build Go Wasm binary
npm run deploy # deploy worker
```

### Testing dev server

- Just send HTTP request using some tools like curl.

```
$ curl http://localhost:8787/hello
Hello!
```

```
$ curl -X POST -d "test message" http://localhost:8787/echo
test message
```
