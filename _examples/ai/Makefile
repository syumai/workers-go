.PHONY: dev
dev:
	wrangler dev

.PHONY: build
build:
	go run ../../cmd/workers-assets-gen -mode=go
	GOOS=js GOARCH=wasm go build -o ./build/app.wasm .

.PHONY: deploy
deploy:
	wrangler deploy

# Deploys onto a Cloudflare temporary account. Same credential requirement
# as `wrangler dev`: the AI binding is accepted there, and the embedding
# call runs against Workers AI.
.PHONY: deploy-temporary
deploy-temporary:
	wrangler deploy --temporary
