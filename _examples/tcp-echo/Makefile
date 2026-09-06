.PHONY: dev
dev:
	wrangler dev --compatibility-flags experimental

.PHONY: build
build:
	go run ../../cmd/workers-assets-gen -mode=go
	GOOS=js GOARCH=wasm go build -o ./build/app.wasm .

.PHONY: deploy
deploy:
	wrangler deploy
