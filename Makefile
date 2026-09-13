VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all web build test test-go test-web headers headers-apply vuln dev run docker clean

all: build

## Build the frontend into internal/server/webdist/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## Build the single binary (requires `make web` first for the embedded UI)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o filezam ./cmd/filezam

test: headers test-go test-web

## License header on every source file (AGPL-3.0-only, see NOTICE.md); headers-apply adds missing ones
headers:
	scripts/license-header.sh check

headers-apply:
	scripts/license-header.sh apply

test-go:
	go test ./...

test-web:
	cd web && npm run typecheck && npm test

## Dependency vulnerability scans (same as CI)
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd web && npm audit --omit=dev

## Run the Go API locally (serves ./data, db in ./config) with plain-HTTP cookies
run:
	FILEZAM_SECURE_COOKIES=false FILEZAM_LOG_LEVEL=debug FILEZAM_ADMIN_PASSWORD=admin1234 go run ./cmd/filezam

## Vite dev server with hot reload, proxying /api to :8080 (run `make run` in another terminal)
dev:
	cd web && npm run dev

docker:
	docker compose build --build-arg VERSION=$(VERSION)

clean:
	rm -rf filezam web/dist internal/server/webdist/dist/* && touch internal/server/webdist/dist/.keep
