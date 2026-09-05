# ai

* This app show some examples of using Cloudflare AI.

## Demo

* https://workers-ai-example.syumai.workers.dev/

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler
* tinygo

### Commands

```
make dev     # run dev server
make build   # build Go Wasm binary
make deploy # deploy worker
```

### Testing AI 

* With curl command below, you can test basic ai functionality for text generation
  - see: https://developers.cloudflare.com/workers-ai/models/llama-3.1-8b-instruct/

```
curl "http://localhost:8787/ai"
```

* With curl command below, you can test basic ai functionality to generate image and see in the browser
  - see: https://developers.cloudflare.com/workers-ai/models/flux-1-schnell/

```
curl "http://localhost:8787/ai-text-to-image"
```

* WIP - With curl command below, you can test basic ai functionality for 
  - see: https://developers.cloudflare.com/workers-ai/models/stable-diffusion-v1-5-img2img/

```
curl "http://localhost:8787/ai-image-to-image"
```


