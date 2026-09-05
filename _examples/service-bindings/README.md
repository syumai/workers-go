# service-bindings

- Service bindings are an API that facilitate Worker-to-Worker communication via explicit bindings defined in your configuration.
- In this example, invoke [hello](https://github.com/syumai/workers-go/tree/main/_examples/hello) using Service bindings.

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Deploy Steps

1. Deploy [hello](https://github.com/syumai/workers-go/tree/main/_examples/hello) first.
2. Define service bindings in `wrangler.toml`.
    ```toml
    services = [
        { binding = "hello", service = "hello" }
    ]
    ```
3. Deploy this example.
    ```
    make build   # build Go Wasm binary
    make deploy  # deploy worker
    ```

## Documents

- https://developers.cloudflare.com/workers/runtime-apis/service-bindings/