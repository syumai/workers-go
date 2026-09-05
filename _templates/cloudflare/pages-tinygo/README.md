# pages-tinygo

- A template for starting a Cloudflare Pages Functions project with tinygo.
- This template uses the [`workers-go`](https://github.com/syumai/workers-go) package to run.

## Usage

- `main.go` includes a [chi](https://github.com/go-chi/chi) HTTP router implementation with three different routes. Feel free to edit this code and implement your own HTTP router.

## Requirements

- Node.js
- TinyGo 0.42.0 or later
  - TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/pages-tinygo
```

- Initialize a project.

```console
cd my-app
go mod init
go mod tidy
npm run build # build Go Wasm binary
npm start # start running dev server
curl http://localhost:8787/api/hello # outputs "Hello, Pages Functions!"
```

## Development

### Commands

```
npm start      # run dev server
npm run build  # build Go Wasm binary
npm run deploy # deploy worker
```

### Testing dev server

- You can send HTTP requests using tools like curl.

```
$ curl http://localhost:8787/api/hello
Hello, Pages Functions!
```

```
$ curl http://localhost:8787/api/hello?name=Example
Hello, Example!
```

```
$ curl http://localhost:8787/api/hello2
Hello, Hello world!
```

```
$ curl http://localhost:8787/api/hello3
Hello, Hello, Hello world!
```
