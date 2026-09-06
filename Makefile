SHELL := /bin/bash

GO_VERSION ?= 1.27.1
TINYGO_VERSION ?= 0.42.0

.PHONY: test
test:
	@PATH=$(CURDIR)/testdata/wasm:$$PATH GOOS=js GOARCH=wasm go test ./...

.PHONY: test-host
test-host:
	go test . ./cmd/...

.PHONY: test-e2e
test-e2e:
	@test -d e2e || { echo "e2e/ not found"; exit 1; }
	go test -tags e2e -timeout 10m -count=1 ./e2e/...

.PHONY: test-all
test-all: test test-host test-e2e

.PHONY: check-generated
check-generated:
	$(MAKE) gen-wasm-exec
	git diff --exit-code -- testdata/wasm cmd/workers-assets-gen/assets

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
