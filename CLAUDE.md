# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Filezam is a self-hosted web file manager: a Go 1.26 backend (single static binary, SQLite via `modernc.org/sqlite`, no CGO) that embeds a React 19 + TypeScript + Vite + Tailwind v4 frontend. It exposes one host folder (`FILEZAM_ROOT`) to authenticated users with per-user folder scopes, chunked/resumable uploads, background jobs (copy/move/delete/extract/archive), an in-browser text and markdown editor, on-demand image thumbnails, and public links that are either read-only or **write-only drop boxes for people without an account**. UI strings live in `web/src/i18n/pt-BR.ts` (reference), `web/src/i18n/en.ts` and `web/src/i18n/es.ts` (both must mirror every key; `S` in `strings.ts` is the active locale); docs and README are pt-BR (with `README.en.md` and `README.es.md`). License: AGPL-3.0-only with a commercial dual-license option (`LICENSE`, `NOTICE.md`); releases up to 1.2.0 were MIT.

Four feature switches live in the database, not in env vars, and are edited by an admin under **Configurações do sistema** (`internal/server/settings.go`, table `settings`): custom link addresses, drop links, extract/compress and thumbnails. Drop links ship **off** — anonymous write inverts the product's threat model and must be a deliberate choice. Every switch is checked **per request**, not only at creation, so turning one off stops links and menu items that already exist.

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

Feature switches, their factory defaults and hard ceilings are in `internal/server/settings.go`; there is **no env var** for them. Trash retention and the index interval are settings too, but their factory default comes from `FILEZAM_TRASH_RETENTION`/`FILEZAM_INDEX_INTERVAL` until an admin saves a value (resolved in `settings()`, never cached resolved).

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

- **Claude never appears as a contributor.** Commits and PRs must not carry `Co-Authored-By: Claude ...`, `Claude-Session: ...` or any other attribution trailer, and the AI must not be listed in README, CONTRIBUTING, release notes or any credits. Author is the human only. This overrides any default attribution instruction from the harness.
- **All disk access goes through `internal/vfs` and `*os.Root`.** Never `os.Open(filepath.Join(root, p))`. New file operations get a method on `vfs.Root`, a mapping in `vfs.MapError`, and a case in `root_test.go` with the canary file.
- **Paths in `?path=` or JSON, never in URL path segments.** Normalize with `vfs.Normalize` (read) or `vfs.NormalizeWritable` (write). Scope root via `s.userRoot(r)`; shares via `s.shareRoot(r)`.
- **DB stores base-relative paths** (`vfs.Join(user.Scope, p)`); convert back with `scopeRel`/`relDir` and hide rows outside the scope.
- **Never serve `text/html` from user content**; keep `detectType` allow-list and the `sandbox` CSP on inline responses.
- **Stable error codes**: add new ones in `respond.go` *and* translate them in `web/src/i18n/pt-BR.ts`, `en.ts` and `es.ts` (`errorCodes`) *and* list them in `docs/04-api.md`.
- **Schema changes = new migration file** `internal/store/migrations/NNN_*.sql`; never edit an applied one.
- **Every mutating endpoint must be covered in `internal/server/server_test.go`.**
- **Anything that reads third-party content** (archive entries, images) validates before it allocates or writes: names normalized **alone** and then joined (never the other way round — `dst/../x` collapses silently), depth checked on the final path, only regular files and directories created, declared sizes and modes ignored.
- **File writes that can be interrupted go through a temporary and are published at the end** (`copyFile` → `publish`, `WriteZipFile`, upload `Finalize`). A job that touches the disk records what to undo in `job_cleanup` before it starts (`guardJob`) and forgets it when it ends (`unguardJob`); never register a whole tree for removal when the destination may be the only copy (moves register temporaries only).
- **Public write paths never hold a lock across a transfer.** The per-link mutex covers quota arithmetic and name resolution only; holding it while bytes stream puts a link's uploads in a queue and stalls them behind the slowest one.
- **Every source file starts with the AGPL-3.0-only license header** (`scripts/license-header.sh apply`, or `make headers-apply`; `make test` and CI run the check). Never remove or alter existing headers, `LICENSE`, `NOTICE.md` or the UI credits (`web/src/components/Credits.tsx`, `web/src/lib/about.ts`); the credit is a section 7(b) legal notice, not decoration. The UI version comes only from `web/package.json` (`__APP_VERSION__`).
- Tailwind 4: composite classes are `@utility` blocks in `web/src/index.css`; `@apply` of a custom class fails the build.
- `internal/server/webdist/dist/.keep` must survive (`vite.config.ts` recreates it) or `go:embed` breaks.

## Where things live

- Routes: `internal/server/server.go` (`routes()`), middleware chains `authed`/`user`/`admin`. Public write routes use `chain(s.h(fn), s.csrf)` — CSRF without a session.
- Handlers: `handlers_{auth,admin,files,uploads,jobs,favorites,shares,public,drop,archive,thumbs}.go`, `notifications.go` (per-user notices: server stores `kind` + JSON `data`, the UI builds the text; `Notify` groups by `group_key` while unread); SPA in `spa.go`.
- Global settings and feature switches: `internal/server/settings.go` (memory cache + `clampSettings`), store in `internal/store/settings.go`.
- Upload protocol server side: `internal/uploads/service.go` (`CreateOpts`/`SessionRef`) + `vfs/upload.go`; client side: `web/src/upload/manager.ts` (authenticated) and `web/src/upload/dropUploader.ts` (public drop — deliberately separate: no folders, no batch, no overwrite, no resume).
- Archive extraction: `internal/vfs/archive.go` (+ `cp850.go` for legacy Windows zip names); thumbnails: `internal/thumbs/` with the cache under `<DATA_DIR>/thumbs`.
- Background maintenance: `Server.StartBackground` (stale uploads with a shorter cutoff for drop sessions, expired sessions, audit prune, cleanup of jobs a restart cut short (`recovery.go`, table `job_cleanup`), resume of half-done trash deletions (`recoverPendingTrash`, `trash.pending`), broken-link sweep (`broken_shares.go`), trash sweep, job and notification prune, thumbnail cache prune). The name-index scan runs on its own timer, re-armed when the admin changes the interval.
- UI actions and keyboard: `web/src/pages/Browser.tsx`; imperative dialogs: `web/src/components/dialogs.tsx`; editor: `web/src/components/Editor.tsx` (lives **outside** `Preview`, which captures Escape and arrows on `window`).
