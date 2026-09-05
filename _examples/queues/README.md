# queues

An example of using Cloudflare Workers that interact with [Cloudflare Queues](https://developers.cloudflare.com/queues/).

## Running

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Supported commands

```
make dev          # run dev server
make build        # build Go Wasm binary
make create-queue # creates the queue
make deploy       # deploy worker
```

### Interacting with the local queue


NOTE: Wrangler does not support running multiple workers interacting with the same _local_ queue. Therefore, for the demostrational purposes,
we use the same worker to both produce and consume messages from the queue. For a real-world scenario, please consider the differences
between [queues.Consume](https://github.com/syumai/workers-go/blob/main/cloudflare/queues/consumer.go#L65) and 
(queues.ConsumeNonBlock)(https://github.com/syumai/workers-go/blob/main/cloudflare/queues/consumer.go#L75) functions.

1. Start the dev server.
```sh
make dev
```

2. Send a message to the queue.
```sh
curl -v -X POST http://localhost:8787/ -d '{"message": "Hello, World!"}' -H "Content-Type: application/json"
```

3. Observe the response and server logs

4. You can pass `text/plain` content type to write queue message as the string or omit the `Content-Type` header to write queue message as 
byte array.

