# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Filezam is a self-hosted web file manager: a Go 1.26 backend (single static binary, SQLite via `modernc.org/sqlite`, no CGO) that embeds a React 19 + TypeScript + Vite + Tailwind v4 frontend. It exposes one host folder (`FILEZAM_ROOT`) to authenticated users, with per-user folder scopes, chunked/resumable uploads, background copy/move/delete jobs and expiring public read-only share links. UI strings are pt-BR (`web/src/strings.ts`).

## Commands

```bash
make run                 # Go API on :8080 (FILEZAM_SECURE_COOKIES=false), serves ./data, DB in ./config
make dev                 # Vite dev server on :5173, proxies /api to :8080
make web                 # npm ci + vite build -> internal/server/webdist/dist (must exist before `go build` embeds a real UI)
make build               # CGO_ENABLED=0 go build -> ./filezam
make test                # go test ./... + tsc --noEmit + vitest
go test ./internal/vfs/ -run TestEscapeAttemptsAreRefused   # single Go test
cd web && npx vitest run src/lib/paths.test.ts               # single frontend test
docker compose build && docker compose up -d
```

Port 8080 may be taken on the dev machine; override with `FILEZAM_LISTEN=127.0.0.1:8765`.

## Architecture

**Request flow**: `cmd/filezam/main.go` (subcommands `serve`, `healthcheck`, `reset-admin`) → `internal/config` (env parsing) → `internal/server` (`routes.go` wires every endpoint; middleware chain in `middleware.go`: recover → realIP → security headers → logging, then per-route `requireUser` → `requirePasswordOK` → `requireAdmin`). Handlers return `error`; `respond.go` maps domain errors to `{error:{code,message}}` with stable codes that the frontend translates in `strings.ts`.

**Filesystem sandbox (`internal/vfs`)** is the security core. Every disk access goes through `*os.Root` (kernel-enforced, symlink-safe). `Normalize` rejects `..`/NUL/invalid UTF-8 and returns a slash path with `""` meaning root; `NormalizeWritable` also rejects the reserved `.filezam-` prefix used for upload part files. Per request, `Server.scopeRoot(user)` opens `base.Sub(user.Scope)`; public shares open `base.Sub(share.Path)`. Never use `os.*` with joined paths. Paths travel in query strings or JSON bodies, never in URL path segments.

**Scope-relative vs base-relative**: users see paths relative to their scope. The DB stores base-relative paths for favorites, shares and upload sessions (`vfs.Join(user.Scope, p)`); `scopeRel`/`relDir` convert back and hide rows outside the current scope.

**Uploads** (`internal/uploads` + `handlers_files.go`): three modes chosen client-side by size — multipart batch of small files (`POST /api/files/batch`, manifest in part `meta`, file parts named by index because Go strips directories from part filenames), single streaming `PUT /api/files/content`, and chunked sessions (`/api/uploads`: preallocated `.part` file, fixed-size chunks written at offsets, received bitset persisted in SQLite, `Finalize` uses `Link`+`Remove` for no-replace atomic rename). Orphan `.part` files are pruned lazily on listing and by an hourly stale-session sweep.

**Jobs** (`internal/jobs`): copy/move/delete run as goroutines with progress; handlers wait up to 300 ms ("sync-or-job") then return the job snapshot. Jobs open their own scope root because the request's root is closed on return. State is in-memory only.

**Frontend** (`web/src`): `api/client.ts` adds the `X-Filezam: 1` CSRF header and maps 401 to `/login`. Server state lives in react-query (`['list', path]` keys invalidated via `useInvalidateDirs`); UI state (selection, clipboard, sort) in zustand (`store/ui.ts`). `upload/manager.ts` is framework-free: it batches/chunks, runs `maxParallel` slots, retries with backoff, serializes conflict dialogs ("apply to all"), matches pending server sessions to re-dropped files for resume, and publishes a throttled snapshot via `useSyncExternalStore`. `components/dialogs.tsx` exposes imperative `dialogs.prompt/confirm/conflict` promises. `FileList` is virtualized (`@tanstack/react-virtual`) and shared by the authenticated browser and the public share page.

**Build/embed**: Vite writes to `internal/server/webdist/dist` (a `.keep` file is preserved so `//go:embed all:dist` compiles without a build); `spa.go` serves `/assets/*` immutable and everything else as `index.html` with a strict CSP. Tailwind v4: custom classes are declared with `@utility` in `web/src/index.css` (plain `@apply` of custom classes fails the build).

## Conventions and gotchas

- Config lives only in env vars (see `.env.example`); `FILEZAM_DATA_DIR` must not be inside `FILEZAM_ROOT` (startup refuses).
- Mutating API calls require the `X-Filezam` header and same-origin fetch metadata; curl tests must send `-H 'X-Filezam: 1'`.
- Inline previews are allow-listed (`detectType` in `handlers_files.go`); HTML is always served as `text/plain` or attachment, with a `sandbox` CSP.
- The Docker image is distroless (no shell); the healthcheck is the binary's own `healthcheck` subcommand. Compose runs as `PUID:PGID` with a read-only rootfs; the app probes writability of `/data` and `/config` at startup.
- Integration tests in `internal/server/server_test.go` spin up the real handler with a temp root and SQLite; `internal/vfs/root_test.go` keeps a canary file outside the root to prove no escape.
