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
- TinyGo 0.42.0 or later
  - TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/cron-tinygo
```

- Initialize a project.

```console
cd my-app
go mod init
go mod tidy
npm start # start running dev server
```

## Development

### Commands

```
npm start      # run dev server
npm run build  # build Go Wasm binary
npm run deploy # deploy worker
```

### Testing the Dev Server

- To test the cron job, you can simulate the cron event by sending an HTTP request to the dev server.

```console
curl -X POST http://localhost:8787/cron
```

- You should see the scheduled time printed in the console.
