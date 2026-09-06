# tcp-echo

- an inbound TCP socket (`connect()` handler) example: echoes back everything a client sends, until the client closes the connection.
- uses `sockets.Serve` (`github.com/syumai/workers-go/cloudflare/sockets`), which exposes the Worker's inbound socket as a `net.Conn`.

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler >= 4.125.0 (inbound TCP sockets support for `wrangler dev` was added in this release)
* go

### Commands

```
make dev    # run dev server
make build  # build Go Wasm binary
make deploy # deploy worker
```

`make dev` starts `wrangler dev`, which listens for TCP connections on port 4242 (see `wrangler.jsonc`'s `connect` config) and forwards them to the Worker's `connect()` handler. Try it with:

```
nc localhost 4242
```

Anything you type should be echoed back until you close the connection.

## Notes

- Requires the `experimental` compatibility flag (see `wrangler.jsonc`).
- Inbound TCP sockets are currently in private beta for production deployments (Spectrum-fronted Workers); `wrangler dev` works without enrollment.
- Inbound sockets are not TLS-terminated by the platform.
