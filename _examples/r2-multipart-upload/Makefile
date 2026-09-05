.PHONY: dev
dev:
	wrangler dev

.PHONY: build
build:
	go run ../../cmd/workers-assets-gen -mode=go
	GOOS=js GOARCH=wasm go build -o ./build/app.wasm ./...

.PHONY: create-bucket
create-bucket:
	wrangler r2 bucket create r2-multipart-upload

.PHONY: deploy
deploy:
	wrangler deploy
