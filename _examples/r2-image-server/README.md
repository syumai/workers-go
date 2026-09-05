# r2-image-server

* An example server of R2.
* This server can store / load / delete images in R2.

## Usage

### Endpoints

* **GET `/{key}`**
  - Get an image object at the `key` and returns it.
* **POST `/{key}`**
  - Create an image object at the `key` and uploads image.
  - Request body must be binary and request header must have `Content-Type`.
* **DELETE `/{key}`**
  - Delete an image object at the `key`.

## Development

* See the following documents for details on how to use R2.
  - https://developers.cloudflare.com/r2/runtime-apis
  - https://pkg.go.dev/github.com/syumai/workers-go

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Commands

```
make dev           # run dev server
make build         # build Go Wasm binary
make create-bucket # create r2 bucket
make deploy        # deploy worker
```
