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

# Deploys onto a Cloudflare temporary account. QA confirmed that account
# accepts the Images binding and that Info works on a 1×1 PNG.
.PHONY: deploy-temporary
deploy-temporary:
	wrangler deploy --temporary
