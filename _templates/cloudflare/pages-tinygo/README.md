# pages-tinygo

- A template for starting a Cloudflare Pages Functions project with tinygo.
- This template uses the [`workers-go`](https://github.com/syumai/workers-go) package to run.

## Usage

- `main.go` includes a [chi](https://github.com/go-chi/chi) HTTP router implementation with three different routes. Feel free to edit this code and implement your own HTTP router.

## Requirements

- Node.js
- [wrangler](https://developers.cloudflare.com/workers/wrangler/)
  - just run `npm install -g wrangler`
- TinyGo 0.42.0 or later
  - TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

## Getting Started

* If not already installed, please install the [gonew](https://pkg.go.dev/golang.org/x/tools/cmd/gonew) command.

```console
go install golang.org/x/tools/cmd/gonew@latest
```

* Create a new project using this template.
  - Second argument passed to `gonew` is a module path of your new app.

```console
gonew github.com/syumai/workers-go/_templates/cloudflare/pages-tinygo your.module/my-app # e.g. github.com/syumai/my-app
cd my-app
go mod tidy
make build # build Go Wasm binary
make dev # start running dev server
curl http://localhost:8787/api/hello # outputs "Hello, Pages Functions!"
```

## Development

### Commands

```
make dev     # run dev server
make build   # build Go Wasm binary
make deploy # deploy worker
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

