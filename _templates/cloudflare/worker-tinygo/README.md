# worker-template-tinygo

- A template for starting a Cloudflare Worker project with tinygo.
- This template uses [`workers`](https://github.com/syumai/workers-go) package to run an HTTP server.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Node.js
- tinygo 0.35.0 or later

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
