# images

Reads and resizes an embedded 1×1 PNG with the
[Images binding](https://developers.cloudflare.com/images/transform-images/bindings/)
through [`exp/cloudflare/images`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/images).

`GET /` does two things:

1. `ImagesBinding.Info` on the embedded PNG (format, width, height, file size).
2. `Input` → `Transform` (width 8) → `Output` (`image/png`) → `HTTPResponse`,
   then `Info` again on those bytes.

No image is fetched from the network. The handler returns JSON metadata, not
the PNG body.

A temporary Cloudflare account accepts the `[images]` binding. `Info` on a
1×1 PNG has been checked there. Local `wrangler dev` may implement Images
inside workerd, or it may proxy to Cloudflare the way the AI binding does.
If a request fails with an authentication error, log in and retry.

## Requirements

* Go 1.22 or later
* [Wrangler](https://developers.cloudflare.com/workers/wrangler/) 4

## Run locally

```sh
wrangler login
# or export CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID

make dev
curl -sS http://127.0.0.1:8787/
```

A working response looks like:

```json
{
  "format": "image/png",
  "width": 1,
  "height": 1,
  "fileSize": 69,
  "contentType": "image/png",
  "resizedBytes": 80,
  "resizedWidth": 8,
  "resizedHeight": 8
}
```

`format` should name a PNG, the source size should be 1×1, and
`resizedWidth` should be 8. Byte counts can differ from the numbers above.

## Deploy to a temporary account

```sh
make deploy-temporary
# copy the printed https://….workers.dev URL
curl -sS https://<printed-host>/
```

`make deploy` deploys to the account `wrangler login` selected, instead of a
temporary one.
