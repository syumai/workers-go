# worker-template-tinygo

- A template for starting a Cloudflare Worker project with tinygo.
- This template uses [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Node.js
- TinyGo 0.42.0 or later
  - TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/worker-tinygo
```

- Initialize a project.

```console
cd my-app # A directory of the project created by the above command
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
