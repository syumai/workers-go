# analytics

Writes one data point to a
[Workers Analytics Engine](https://developers.cloudflare.com/analytics/analytics-engine/)
dataset through [`exp/cloudflare/analytics`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/analytics).

`GET /` calls `WriteDataPoint` with:

- index `example`
- blobs: the request method and path
- double `1`

The handler returns that point as JSON after the call returns. Local
`wrangler dev` accepts `writeDataPoint` and discards the point. There is no
local API to read it back. Querying the dataset needs the Analytics Engine
SQL API on an account where the product is enabled.

A temporary Cloudflare account rejects this binding at deploy time
(`You need to enable Analytics Engine`, code 10089). `make dev` does not
need an account.

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
{"ok":true,"indexes":["example"],"blobs":["GET","/"],"doubles":[1]}
```

`ok` is true when `WriteDataPoint` returned no error. The other fields are
the point that was submitted, not a row read from the dataset.

## Deploy

`make deploy` deploys to the account `wrangler login` selected. That
account must have Analytics Engine enabled. `wrangler deploy --temporary`
fails with error 10089 on an account that does not.
