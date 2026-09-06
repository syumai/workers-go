SHELL := /bin/bash

GO_VERSION ?= 1.27.1
TINYGO_VERSION ?= 0.42.0

.PHONY: test
test:
	@PATH=$(CURDIR)/testdata/wasm:$$PATH GOOS=js GOARCH=wasm go test ./...

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

.PHONY: gen-bindings-extract
gen-bindings-extract:
	pnpm -C scripts/gen-bindings install --frozen-lockfile && pnpm -C scripts/gen-bindings run extract

.PHONY: gen-bindings
gen-bindings: gen-bindings-extract
	go run -C scripts/gen-bindings ./cfgen -root $(CURDIR)

.PHONY: gen-bindings-check
gen-bindings-check:
	go run -C scripts/gen-bindings ./cfgen -root $(CURDIR) -check
