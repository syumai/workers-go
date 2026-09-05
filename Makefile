SHELL := /bin/bash

GO_VERSION ?= 1.27.1
TINYGO_VERSION ?= 0.42.0

.PHONY: test
test:
	@PATH=$(CURDIR)/misc/wasm:$$PATH GOOS=js GOARCH=wasm go test ./...

.PHONY: build-examples
build-examples:
	for dir in $(shell find ./_examples -maxdepth 1 -type d); do \
		if [ $$dir = "./_examples" ]; then continue; fi; \
		echo 'build:' $$dir; \
		cd $$dir && GOOS=js GOARCH=wasm go build -o ./build/app.wasm; \
		cd ../../; \
	done

.PHONY: gen-wasm-exec
gen-wasm-exec:
	cd scripts/gen-wasm-exec && pnpm run gen --go $(GO_VERSION) --tinygo $(TINYGO_VERSION)
