# cron-tinygo

- A template for starting a Cloudflare Worker project with a cron job using Go.
- This template uses the [workers](https://github.com/syumai/workers-go) package to schedule and run cron jobs.

## Notice

- A free plan Cloudflare Workers only accepts ~1MB sized workers.
  - TinyGo Wasm binaries probably won't exceed this limit, so you might not need to use a paid plan of Cloudflare Workers.
  - There's also a Go version of this that can be found [here](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/cron-go).

## Usage

- `main.go` includes a simple cron job implementation. Feel free to edit this code and implement your own cron job logic.

## Requirements

- Node.js
- [wrangler](https://developers.cloudflare.com/workers/wrangler/)
  - Just run `npm install -g wrangler`
- Go 1.21.0 or later
- TinyGo 0.42.0 or later
  - TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

## Getting Started

- If not already installed, please install the [gonew](https://pkg.go.dev/golang.org/x/tools/cmd/gonew) command.

```console
go install golang.org/x/tools/cmd/gonew@latest
```

- Create a new project using this template.
  - The second argument passed to `gonew` is the module path of your new app.

```console
gonew github.com/syumai/workers-go/_templates/cloudflare/cron-tinygo your.module/my-app # e.g. github.com/syumai/my-app
cd my-app
go mod tidy
make dev # start running dev server
```

- To change the worker name, please edit the `name` property in `wrangler.toml`.

## Development

### Commands

```console
make dev     # run dev server
make build   # build Go Wasm binary
make deploy  # deploy worker
```

### Testing the Dev Server

- To test the cron job, you can simulate the cron event by sending an HTTP request to the dev server.

```console
curl -X POST http://localhost:8787/cron
```

- You should see the scheduled time printed in the console.
