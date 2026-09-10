# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Filezam is a self-hosted web file manager: a Go 1.26 backend (single static binary, SQLite via `modernc.org/sqlite`, no CGO) that embeds a React 19 + TypeScript + Vite + Tailwind v4 frontend. It exposes one host folder (`FILEZAM_ROOT`) to authenticated users with per-user folder scopes, chunked/resumable uploads, background copy/move/delete jobs and expiring public read-only share links. UI strings live in `web/src/i18n/pt-BR.ts` (reference) and `web/src/i18n/en.ts` (must mirror every key; `S` in `strings.ts` is the active locale); docs and README are pt-BR.

## Specs: read before changing anything

The `docs/` folder is the source of truth. Read the document for the area you touch and **update it in the same commit** when behavior changes.

| Area | Document |
|---|---|
| Requirements, stack, architecture decisions (ADRs) | `docs/01-visao-geral.md` |
| Packages, request flow, path referentials, process lifecycle, limits | `docs/02-arquitetura.md` |
| Threat model, path sandbox, auth, CSRF, headers, previews, shares, container | `docs/03-seguranca.md` |
| Every endpoint, payloads, error codes | `docs/04-api.md` |
| Upload modes, chunk sessions, resume, conflicts, cleanup | `docs/05-uploads.md` |
| SQLite schema, migrations, backup | `docs/06-banco-de-dados.md` |
| Frontend structure, state, upload manager, keyboard, styling | `docs/07-frontend.md` |
| Deploy, env vars, proxies, troubleshooting | `docs/08-operacao.md` |
| Test suites and manual checklist | `docs/09-testes.md` |
| Known limitations and next steps | `docs/10-roadmap.md` |

## Commands

```bash
make run                 # Go API on :8080 (FILEZAM_SECURE_COOKIES=false), serves ./data, DB in ./config
make dev                 # Vite dev server on :5173, proxies /api to :8080
make web                 # npm ci + vite build -> internal/server/webdist/dist (needed before `go build` embeds a real UI)
make build               # CGO_ENABLED=0 go build -> ./filezam
make test                # go test ./... + tsc --noEmit + vitest
go test ./internal/vfs/ -run TestEscapeAttemptsAreRefused   # single Go test
# no Go toolchain on this machine? run the Go suite in Docker (tmpfs: TestShareBoundToInode needs non-reused inodes):
docker run --rm -v "$PWD":/src -w /src --tmpfs /tmp:exec -e GOFLAGS=-buildvcs=false golang:1.26-alpine go test ./...
cd web && npx vitest run src/lib/paths.test.ts               # single frontend test
docker compose build && docker compose up -d
```

Port 8080 is often taken on the dev machine; use `FILEZAM_LISTEN=127.0.0.1:8765`. Scripted API calls need `-H 'X-Filezam: 1'`.

## Non-negotiable rules

- **All disk access goes through `internal/vfs` and `*os.Root`.** Never `os.Open(filepath.Join(root, p))`. New file operations get a method on `vfs.Root`, a mapping in `vfs.MapError`, and a case in `root_test.go` with the canary file.
- **Paths in `?path=` or JSON, never in URL path segments.** Normalize with `vfs.Normalize` (read) or `vfs.NormalizeWritable` (write). Scope root via `s.userRoot(r)`; shares via `s.shareRoot(r)`.
- **DB stores base-relative paths** (`vfs.Join(user.Scope, p)`); convert back with `scopeRel`/`relDir` and hide rows outside the scope.
- **Never serve `text/html` from user content**; keep `detectType` allow-list and the `sandbox` CSP on inline responses.
- **Stable error codes**: add new ones in `respond.go` *and* translate them in `web/src/i18n/pt-BR.ts` and `en.ts` (`errorCodes`) *and* list them in `docs/04-api.md`.
- **Schema changes = new migration file** `internal/store/migrations/NNN_*.sql`; never edit an applied one.
- **Every mutating endpoint must be covered in `internal/server/server_test.go`.**
- Tailwind 4: composite classes are `@utility` blocks in `web/src/index.css`; `@apply` of a custom class fails the build.
- `internal/server/webdist/dist/.keep` must survive (`vite.config.ts` recreates it) or `go:embed` breaks.

## Where things live

- Routes: `internal/server/server.go` (`routes()`), middleware chains `authed`/`user`/`admin`.
- Handlers: `handlers_{auth,admin,files,uploads,jobs,favorites,shares,public}.go`; SPA in `spa.go`.
- Upload protocol server side: `internal/uploads/service.go` + `vfs/upload.go`; client side: `web/src/upload/manager.ts`.
- Background maintenance: `Server.StartBackground` (stale uploads, expired sessions, audit prune).
- UI actions and keyboard: `web/src/pages/Browser.tsx`; imperative dialogs: `web/src/components/dialogs.tsx`.
