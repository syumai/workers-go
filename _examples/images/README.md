# images

Reads and resizes an embedded 32×32 PNG with the
[Images binding](https://developers.cloudflare.com/images/transform-images/bindings/)
through [`exp/cloudflare/images`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/images).

`GET /` does two things:

1. `ImagesBinding.Info` on an embedded 32×32 PNG (format, width, height, file size).
2. `Input` → `Transform` (width 8) → `Output` (`image/png`) → `HTTPResponse`,
   then `Info` again on those bytes.

No image is fetched from the network. The handler returns JSON metadata, not
the PNG body.

Local `wrangler dev` runs the Images binding inside workerd, so `make dev`
does not need a Cloudflare account. A temporary account also accepts the
`[images]` binding; `Info` on a small PNG has been checked there.

## Requirements

* Go 1.22 or later
* [Wrangler](https://developers.cloudflare.com/workers/wrangler/) 4

## Run locally

```sh
make dev
curl -sS http://127.0.0.1:8787/
```

A working response looks like:

```json
{
  "format": "image/png",
  "width": 32,
  "height": 32,
  "fileSize": 109,
  "contentType": "image/png",
  "resizedBytes": 80,
  "resizedWidth": 8,
  "resizedHeight": 8
}
```

`format` should name a PNG, the source size should be 32×32, and
`resizedWidth` should be 8. Byte counts can differ from the numbers above.

## Deploy to a temporary account

```sh
make deploy-temporary
# copy the printed https://….workers.dev URL
curl -sS https://<printed-host>/
```

`make deploy` deploys to the account `wrangler login` selected, instead of a
temporary one.
