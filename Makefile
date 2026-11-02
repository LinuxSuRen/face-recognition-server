# face-recognition-server developer tasks
#
# Local, CGO-free loop (no inference engine; pure-Go logic + API/UI):
#   make dev-build dev-test
#
# Full build with the ONNX Runtime engine (requires a C compiler + onnxruntime):
#   make build
#
# Frontend:
#   make web-install web-build

GO ?= go
WEB_DIR ?= web
MODELS_DIR ?= models

.PHONY: build build-nocgo test test-nocgo vet web-install web-build models clean

## build: full server binary with the ONNX Runtime engine (CGO required)
build:
	CGO_ENABLED=1 $(GO) build -o bin/server ./cmd/server

## build-nocgo: binary without the inference engine (health + UI only)
build-nocgo:
	CGO_ENABLED=0 $(GO) build -o bin/server-nocgo ./cmd/server

## test: run all tests with the engine enabled (CGO + onnxruntime required)
test:
	CGO_ENABLED=1 $(GO) test ./...

## test-nocgo: run pure-Go tests (default local loop)
test-nocgo:
	CGO_ENABLED=0 $(GO) test ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...

web-install:
	cd $(WEB_DIR) && pnpm install

web-build:
	cd $(WEB_DIR) && pnpm build

models:
	pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/download-models.ps1 -OutputDir $(MODELS_DIR)

clean:
	rm -rf bin $(WEB_DIR)/dist
