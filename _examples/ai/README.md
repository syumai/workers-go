# ai

Calls [Workers AI](https://developers.cloudflare.com/workers-ai/) through
[`exp/cloudflare/ai`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/ai).

`GET /` runs `Ai.Run` on `@cf/baai/bge-base-en-v1.5` with the input text
`hello from workers-go` and returns the embedding length plus the response
`shape`. It does not check the vector values. `@cf/meta/llama-3.1-8b-instruct`
is not used: Workers AI rejects it with error 5028 (deprecated 2026-05-30).

Local `wrangler dev` does not run the model inside workerd. The AI binding
proxies to Cloudflare, so this example needs an account.

## Requirements

* Go 1.22 or later
* [Wrangler](https://developers.cloudflare.com/workers/wrangler/) 4

## Run locally

```sh
wrangler login
# or export CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID

make dev
curl -sS http://127.0.0.1:8787/
# {"model":"@cf/baai/bge-base-en-v1.5","length":768,"shape":[1,768]}
```

`length` must be greater than 0. `768` is what this model returns today; the
example does not treat any other positive length as a failure.

## Deploy to a temporary account

`wrangler deploy --temporary` creates a short-lived workers.dev URL on a
temporary Cloudflare account. The AI binding is accepted there.

```sh
make deploy-temporary
# copy the printed https://….workers.dev URL
curl -sS https://<printed-host>/
```

`make deploy` deploys to the account `wrangler login` selected, instead of a
temporary one.
