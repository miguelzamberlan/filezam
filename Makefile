VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all web build test test-go test-web dev run docker clean

all: build

## Build the frontend into internal/server/webdist/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## Build the single binary (requires `make web` first for the embedded UI)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o filezam ./cmd/filezam

test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web && npm run typecheck && npm test

## Run the Go API locally (serves ./data, db in ./config) with plain-HTTP cookies
run:
	FILEZAM_SECURE_COOKIES=false FILEZAM_LOG_LEVEL=debug go run ./cmd/filezam

## Vite dev server with hot reload, proxying /api to :8080 (run `make run` in another terminal)
dev:
	cd web && npm run dev

docker:
	docker compose build --build-arg VERSION=$(VERSION)

clean:
	rm -rf filezam web/dist internal/server/webdist/dist/* && touch internal/server/webdist/dist/.keep
