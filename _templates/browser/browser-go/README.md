# browser-go

- A template for starting a browser-based HTTP server with Go.
  - This template uses Cloudflare Workers to just serve a static page.
- This template uses [`workers-go`](https://github.com/syumai/workers-go) package to run an HTTP server.

## Usage

- `main.go` includes simple HTTP server implementation. Feel free to edit this code and implement your own HTTP server.

## Requirements

- Node.js
- Go 1.24.0 or later

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/browser/browser-go
```

- Initialize a project.

```console
cd my-app
go mod init your.module/my-app # e.g. github.com/syumai/my-app
go mod tidy
npm start # start running dev server
```

Then, open http://localhost:8787 in your browser.

## Development

### Commands

```
npm start      # run dev server
npm run build  # build Go Wasm binary
npm run deploy # deploy static assets
```
