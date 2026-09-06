.PHONY: dev
dev:
	npx wrangler dev --compatibility-flags experimental

.PHONY: build
build:
	go run ../../cmd/workers-assets-gen -mode=go
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o ./build/app.wasm .

.PHONY: deploy
deploy:
	npx wrangler deploy

.PHONY: generate
generate:
	buf generate
