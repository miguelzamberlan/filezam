# Filezam

> Fast, secure self-hosted web file manager: expose one folder of your server through the browser, with users, huge resumable uploads and expiring public links. A single Go binary with the UI embedded, built to run in a container behind your reverse proxy.

[![CI](https://github.com/miguelzamberlan/filezam/actions/workflows/ci.yml/badge.svg)](https://github.com/miguelzamberlan/filezam/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/miguelzamberlan/filezam)](https://github.com/miguelzamberlan/filezam/releases)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![SQLite](https://img.shields.io/badge/SQLite-no%20CGO-003B57?logo=sqlite&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-distroless-2496ED?logo=docker&logoColor=white)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[🇧🇷 Português](README.md) · 🇬🇧 English

<p align="center">
  <img src="docs/img/desktop-light.png" alt="Filezam on desktop, light theme" width="49%">
  <img src="docs/img/desktop-dark.png" alt="Filezam on desktop, dark theme" width="49%">
</p>

> The full documentation (`docs/`), the changelog and `CONTRIBUTING.md` are written in Brazilian Portuguese; links to them below are marked *(Portuguese)*. The UI itself is available in English and Portuguese. Issues and pull requests in English are welcome.

## Table of contents

- [Why it exists](#why-it-exists)
- [Features](#features)
- [How to run](#how-to-run)
  - [On your machine in a minute](#on-your-machine-in-a-minute)
  - [Without Docker](#without-docker)
  - [On a server, behind a proxy](#on-a-server-behind-a-proxy)
  - [On Easypanel](#on-easypanel)
- [Configuration](#configuration)
- [How to use](#how-to-use)
- [Security](#security)
- [Documentation](#documentation)
- [Development](#development)
- [Contributing](#contributing)
- [Roadmap and limitations](#roadmap-and-limitations)
- [Versions](#versions)
- [Author and license](#author-and-license)

## Why it exists

Filezam started from a concrete need: an external drive attached to a home Linux server, shared on the LAN over Samba, that had to be reachable from the internet by family and clients, each seeing only their own folder. Existing options were heavy, needed an external database, or treated filesystem safety as an afterthought.

Three priorities drive every decision, in this order:

1. **Security.** Reading or writing outside the exposed folder is impossible: every disk access goes through Go's `os.Root`, which validates each path component in the kernel, symlinks included. No file uploaded by one user can run script in another user's browser. Passwords, sessions and 2FA secrets are never stored in clear text.
2. **Speed.** Streaming I/O without buffering files in memory, few requests for many small files, parallel chunked uploads, and long operations in the background with progress.
3. **Operational simplicity.** One container, configured by environment variables, no external database, no shell in the image, running without root.

It is a personal, free and open-source project. It fits home servers, small offices and anyone who wants to hand files to clients without relying on third-party services. What it does **not** try to be: a Dropbox with sync, a document editor or a WebDAV/S3 server.

## Features

- **Users and scopes**: username and password login, admin/user roles, each account reaching the whole root or one subfolder that it sees as if it were the root.
- **Full management**: browse, create folders, rename, copy, cut/paste (move), delete, download file or ZIP, favorites, properties (computed size, item count, active links) and free disk space.
- **Search** by name across every subfolder the user can reach, ignoring case and accents, answered by a SQLite index (periodic rescan) and verified on disk.
- **Trash** with configurable retention and restore to the original place. `Shift+Del` deletes permanently.
- **Previews** for images, video, audio, PDF, text and rendered Markdown, without leaving the page.
- **Serious uploads**: multi-GB files in parallel resumable chunks, whole folders by drag and drop, thousands of small files batched, all with a progress panel, pause and retry.
- **Thumbnails**: photo folders show a preview of each image, generated on demand and cached. Can be turned off per installation and per person.
- **Extract and compress**: unpack a `.zip` into a new folder from the context menu, or zip the current selection. Extraction never overwrites what is already there and refuses entries that try to write outside the folder.
- **Built-in editor**: edit text, markdown and config files in the browser, with a rendered preview for `.md` and protection against two people overwriting each other.
- **Public links**: share a folder or a file read-only with an expiry, optional password, access count and instant revocation. The address can be a name you pick (`/s/budget-2026`), which then requires a password.
- **Drop links**: a public inbox where people without an account send files into a folder of yours, with a mandatory quota and expiry. Senders never see or download what is already there.
- **Two-factor authentication** (TOTP) with recovery codes and "trust this device for 30 days"; optionally mandatory for administrators.
- **Administration**: users with disk quotas and 2FA reset, audit log of logins and changes, progressive brute-force lockout, operation history and Prometheus metrics.
- **UI**: Portuguese or English, light/dark/system theme, custom colors, list zoom, per-type icons, drag and drop to move, paginated listing for huge folders, keyboard shortcuts and comfortable use on phones.
- **Operations**: static binary, `distroless` image without a shell, read-only rootfs, embedded SQLite (no CGO), automatic migrations, healthcheck.

<p align="center">
  <img src="docs/img/mobile.png" alt="Filezam on a phone" width="30%">
</p>

## How to run

Every scenario, with proxies (nginx, Caddy, Traefik, Cloudflare Tunnel), systemd, backup and troubleshooting, is covered in [`docs/08-operacao.md`](docs/08-operacao.md) *(Portuguese)*. Here is the essential part of each.

### On your machine in a minute

Requirements: Docker with Compose v2.

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
docker compose up -d --build
docker compose logs filezam | grep password   # the admin password, printed once
```

No `.env` needed: every value has a default. Open `http://127.0.0.1:8080` and log in as `admin` with the password from the log — changing it is mandatory on first login. Data lands in `./data`, the database in `./config`.

For real use (host folder, your user, a proxy), copy `.env.example` to `.env` and adjust; compose reads the file when it exists.

> **Windows and macOS**: keep the data **inside WSL2** (or in a Docker volume), never on a mounted
> `C:\...`. Besides being much faster, NTFS and APFS are case-insensitive and Filezam needs a
> case-sensitive filesystem — see [`docs/08`](docs/08-operacao.md#sistema-de-arquivos-da-pasta-de-dados) *(Portuguese)*.

### Without Docker

Requirements: Go (version in `go.mod`) and Node 22+.

```bash
make web && make build     # frontend + ./filezam binary with the UI embedded
FILEZAM_ROOT=$HOME/Files FILEZAM_DATA_DIR=$HOME/.filezam FILEZAM_SECURE_COOKIES=false ./filezam
```

A single binary, with no runtime dependencies. To run it as a service there is a hardened systemd unit example in [`docs/08-operacao.md`](docs/08-operacao.md#4-binário-direto-com-systemd) *(Portuguese)*.

### On a server, behind a proxy

The scenario Filezam was built for: the container listens on `127.0.0.1` only and an HTTPS proxy sits in front.

```bash
sudo mkdir -p /opt/filezam && cd /opt/filezam
git clone https://github.com/miguelzamberlan/filezam.git . && cp .env.example .env
```

```ini
# .env
PUID=1000                                   # owner of the data folder (id -u)
PGID=1000
FILEZAM_HOST_ROOT=/mnt/data                 # exposed folder
FILEZAM_HOST_CONFIG=/opt/filezam/config     # database + secret.key, outside the exposed folder
FILEZAM_TRUSTED_PROXIES=172.31.250.1        # compose network gateway: a proxy on the host connects from it
FILEZAM_SECURE_COOKIES=true
FILEZAM_PUBLIC_URL=https://files.example.com
FILEZAM_ADMIN_PASSWORD=change-me
FILEZAM_REQUIRE_2FA_ADMINS=true
```

```bash
docker compose up -d --build && docker compose logs -f filezam
```

**LAN only, no proxy**: `FILEZAM_BIND=0.0.0.0`, `FILEZAM_TRUSTED_PROXIES=` (empty) and `FILEZAM_SECURE_COOKIES=false`; open `http://server-ip:8080`. Never put your LAN range (`192.168.0.0/16`) or `0.0.0.0/0` in `FILEZAM_TRUSTED_PROXIES`: any machine could then pick its own IP and dodge the login lockout. Severity and combinations (proxy plus local port, proxy in a container) in [docs/08](docs/08-operacao.md#porta-local-e-proxies-confiáveis) *(Portuguese)*.

The proxy must not limit or buffer request bodies. Caddy works out of the box (`reverse_proxy 127.0.0.1:8080`); nginx needs:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_read_timeout 600s;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Traefik on the same Docker network: use the `labels` commented out in `docker-compose.yml` and raise the entrypoint `readTimeout`. Cloudflare (orange proxy or Tunnel): keep the upload chunk at or below 64 MiB (the 16 MiB default is fine).

### On Easypanel

Easypanel builds the image from the repository and puts its own Traefik in front with automatic HTTPS:

1. **+ Service → App**. *Source*: GitHub, repository `miguelzamberlan/filezam`, branch `main`. *Build*: Dockerfile.
2. **Environment**:
   ```ini
   FILEZAM_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
   FILEZAM_SECURE_COOKIES=true
   FILEZAM_PUBLIC_URL=https://files.example.com
   FILEZAM_ADMIN_PASSWORD=change-me
   FILEZAM_REQUIRE_2FA_ADMINS=true
   ```
3. **Mounts**: a *Volume Mount* at `/config` (writable from the start, the folder already exists in the image owned by uid `65532`) and a *Bind Mount* of the host folder at `/data`, writable by uid `65532` (`sudo chown -R 65532:65532 /mnt/data`).
4. **Domains**: your domain on port `8080` with HTTPS.
5. **Deploy** and log in with `admin` plus the password from the variable.

If the data folder belongs to another user (shared over Samba, for instance), use Easypanel's *Compose* service type with the repository's `docker-compose.yml` and the owner's `user: "uid:gid"`. Details in [`docs/08-operacao.md`](docs/08-operacao.md#3-no-easypanel) *(Portuguese)*.

## Configuration

### Folders and permissions

| Volume | Contents |
|---|---|
| `${FILEZAM_HOST_ROOT}` → `/data` | Root folder shown by the file manager. To expose several host folders, mount them as subfolders (right below). |
| `${FILEZAM_HOST_CONFIG}` → `/config` | SQLite database (users, sessions, links, trash, index, audit) and `secret.key` (2FA). Never served, and cannot live inside `/data`. Back up the whole folder. |

The container runs without root, as `PUID:PGID`, and both folders must be writable by that user. Since Docker creates missing *bind mount* folders as `root`, compose ships an `init` service (Alpine, runs once) that fixes the ownership of `/config` and of the Filezam files inside it (never recursively, and it refuses to touch a folder that contains subfolders, so that a wrong `FILEZAM_HOST_CONFIG` cannot re-own a whole host tree) and of `/data`, but only when it is empty. It never changes the owner of a data folder that already has content. If a folder is not writable, the app exits at startup with the suggested `chown`. Never use `PUID=0`.

### Exposing several host folders

Filezam exposes a single root, but you can mount as many host folders as you like **as subfolders** of it in `docker-compose.yml`. There is no environment variable for this: the compose file is what mounts them.

```yaml
    volumes:
      - ${FILEZAM_HOST_ROOT:-./data}:/data
      - /mnt/media:/data/media                 # shows up as the "media" folder in the root
      - /mnt/backup/projects:/data/projects
      - ${FILEZAM_HOST_CONFIG:-./config}:/config
```

Caveats: the extra folders must already be writable by `PUID:PGID` (the `init` service does not touch them); moving or deleting across mounts becomes copy + remove, since they are different filesystems, and the trash always lives at the root of the scope; the free-space figure is that of the disk holding the scope's root, never the sum of the mounts; and a symlink is no shortcut — `os.Root` refuses links pointing outside the root, it has to be a bind mount. Details in [`docs/08`](docs/08-operacao.md#expondo-várias-pastas-do-host) *(Portuguese)*.

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `FILEZAM_ROOT` | `/data` (image) / `./data` | Root folder of the files |
| `FILEZAM_DATA_DIR` | `/config` (image) / `./config` | Database and `secret.key`. Must be outside `FILEZAM_ROOT` |
| `FILEZAM_LISTEN` | `:8080` | Listen address |
| `FILEZAM_TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs of the reverse proxy. Trust only the proxy: whoever is listed picks the IP used for the audit log and login limits ([local port and proxies](docs/08-operacao.md#porta-local-e-proxies-confiáveis), *Portuguese*) |
| `FILEZAM_SECURE_COOKIES` | `auto` | `true` / `false` / `auto` (from `X-Forwarded-Proto` of a trusted proxy) |
| `FILEZAM_PUBLIC_URL` | empty | Base of public links (`https://...`); the UI uses the browser origin when empty |
| `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD` | `admin` / *(random)* | Admin created on first start; an empty password is drawn at random and printed once in the log. Change forced at first login |
| `FILEZAM_SESSION_TTL` | `168h` | Sliding session lifetime (absolute cap: 30 days) |
| `FILEZAM_MAX_UPLOAD_CHUNK` | `16MiB` | Upload chunk size (1 MiB–1 GiB) |
| `FILEZAM_UPLOAD_MAX_RESERVED` | `100GiB` | Disk space one user's unfinished uploads may reserve; `0` = no cap |
| `FILEZAM_SHARE_MAX_TTL` | `720h` | Maximum lifetime of a public link |
| `FILEZAM_TRASH_RETENTION` | `720h` | Time in the trash before permanent removal; `0` disables the trash. Initial value: the admin changes it under **Configurações do sistema** (system settings) |
| `FILEZAM_INDEX_INTERVAL` | `6h` | Full rescan of the search name index; `0` disables the index. Initial value (minimum 15 min): the admin changes the interval under **Configurações do sistema** (system settings) |
| `FILEZAM_METRICS_TOKEN` | empty | Enables `GET /metrics` (Prometheus) with `Authorization: Bearer` |
| `FILEZAM_SECRET_KEY` | empty | 64-hex key encrypting 2FA secrets; empty = `/config/secret.key` generated on first start |
| `FILEZAM_REQUIRE_2FA_ADMINS` | `false` | Force administrators to enable two-factor authentication |
| `FILEZAM_FSYNC` | `true` | `fsync` before finalizing each upload |
| `FILEZAM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

The compose `.env` also has `PUID`/`PGID`, `FILEZAM_HOST_ROOT`, `FILEZAM_HOST_CONFIG`, `FILEZAM_BIND` and `FILEZAM_PORT` (the local port the proxy connects to; `127.0.0.1` only by default).

## How to use

### Users and scopes

Under **Administration → Users** an admin creates accounts and sets the role (administrator or user) and the **access folder**: the whole root or one specific subfolder. A restricted user sees that subfolder as if it were the root and cannot reach anything outside it. Changing the scope or the password invalidates that user's sessions; narrowing the scope also deletes the public links they had outside the new folder. The last administrator cannot be removed or demoted.

After 10 failed logins in a row from the same address, that user + IP pair is locked for 15 minutes (doubling on repeat); the response is the same as a wrong password, so nobody learns whether the account exists, and the legitimate user can still log in from another address. There are also per-IP and per-user attempt limits. An admin can give each account a **disk quota** (in GB). Everything is recorded under **Audit**.

### Public links

Select a folder or a file, click **Share**, choose the expiry and, if you want, a password: the link (`/s/<token>`) is copied right away and can be copied again under **Shares** or in the folder properties. Whoever has the link can list, preview and download (file or ZIP), nothing else. The token is 256 bits. Revoking or expiring it invalidates the link immediately; so does disabling the user or taking the folder out of their scope. Moving, renaming or recreating the shared item invalidates the link.

**Custom address.** Instead of a token you can pick the address (`/s/budget-2026`), handy to dictate over the phone or print. Since such a name is easy to guess, it requires a password — that password is what protects the link. A revoked address stays reserved to you: nobody else can create a link with it until you release it under **Shares**.

### Drop links

Need a client to send you documents without creating an account for them? Under **Share**, pick **Receive files**: give the name of a new folder, the quota and the expiry (30 days at most). The folder is created on the spot and must be empty — nothing you already have is exposed.

Whoever opens the link can only upload. They cannot list, cannot download, cannot see what other people sent (only their own uploads) and never overwrite anything: a repeated name is saved as `name (1).ext`. Every upload shows up under **Audit** with the source IP.

The feature ships **off**. An admin turns it on under **Administration → System settings**, which is also where the ceilings live: maximum quota per link, maximum expiry, size per file, number of files and how many links each user may have.

### Uploads

| Size | Method |
|---|---|
| ≤ 1 MiB | Several files per request (`multipart`), up to 200 files / 32 MiB per batch |
| ≤ chunk | A single direct `PUT` |
| > chunk | Chunked session: parallel, out-of-order, idempotent chunks; `complete` only after all of them |

Files are written to `.filezam-upload-*.part` in the destination folder and renamed atomically at the end, so a half-written file never shows up. Abandoned sessions are removed after 24 h. If the browser is closed in the middle of a large upload, a notice appears when you come back: drop the same file into the same folder and only the missing chunks are sent.

### Interface

- **Files** (side menu) is the browsing itself. The filter at the top of the list only sifts the current folder; **Search** looks for the name across every subfolder. The result takes you to the folder with the item selected.
- **Uploads** happen by dragging files/folders onto the listing or through the **Upload files**/**Upload folder** buttons; progress shows in a floating panel with pause, cancel and retry.
- **Trash**: whatever you delete stays there for the period chosen in **System settings** (30 days by default) and can be restored. Files changed outside Filezam (Samba, SSH) appear in the search after the next index rescan (interval also in **System settings**) or when an admin clicks **Scan now**.
- **Operations**: copies, moves and deletions in progress, with cancellation, plus the history of the last 30 days.
- **Account**: change the password and enable two-factor authentication by scanning the QR code in your authenticator app; keep the 10 recovery codes. At login, "Trust this device for 30 days" skips the code in that browser.
- **Theme, colors and zoom** (**My account**, the card with your name at the bottom of the menu, together with password and two-step verification): light, dark or follow the system; accent colors from ready-made combinations or a free picker; listing zoom without touching the browser zoom. All of it stays in the browser, per user.
- **Drag and drop**: drag items from the listing onto a folder or onto a level of the breadcrumb to move them.
- **Phone**: the menu becomes a drawer, the action bar shrinks to the essentials plus **⋯**, a tap opens, a long press selects and opens the menu.

Shortcuts: `↑ ↓ Home End PgUp PgDn` navigate · `Shift`/`Ctrl` multiple selection · `Ctrl+A` all · `Enter` open · `Backspace`/`Alt+↑` go up · `F2` rename · `Del` delete · `Ctrl+C` `Ctrl+X` `Ctrl+V` copy/cut/paste · `Ctrl+Shift+N` new folder · `Esc` clear · typing letters jumps to the name.

## Security

A summary of what the project guarantees. The full threat model, the controls and the accepted limitations are in [`docs/03-seguranca.md`](docs/03-seguranca.md) *(Portuguese)*; how to report a flaw, in [`SECURITY.md`](SECURITY.md).

- **Path sandbox**: every disk access goes through `os.Root`; `..`, escaping symlinks and internal names (`.filezam-*`) are rejected on input, reads included. New names do not accept control characters. Tests keep a canary file outside the root and assert that no operation reaches it.
- **User content never becomes a page**: HTML/JS/SVG-as-text are served as `text/plain`, everything else as a download; inline previews carry a `sandbox` CSP and `nosniff`; Markdown is rendered client-side with raw HTML dropped.
- **Sessions**: `HttpOnly` + `SameSite=Strict` cookie, only the hash stored, sliding lifetime capped at 30 days; Argon2id passwords; progressive lockout and rate limits on login **and** password change; TOTP 2FA with AES-GCM encrypted secrets and replay protection.
- **CSRF**: the `X-Filezam: 1` header required on every mutating request, plus `Sec-Fetch-Site`/`Origin` checks.
- **Headers**: strict CSP on the SPA, `X-Frame-Options`, `Referrer-Policy: same-origin`, HSTS behind a trusted HTTPS proxy; share tokens never reach the logs.
- **Resources**: per-user disk quota, caps on concurrent zips, jobs, searches and uploads, rate limits on public routes.
- **Container**: distroless, no shell, non-root, read-only rootfs, `cap_drop ALL`, `no-new-privileges`. Go version pinned; `govulncheck` and `npm audit` run in CI.
- **Accepted limitations**: an admin with a scope sees everyone's links; the link token is stored in clear so it can be copied again; links are bound to path + inode; whoever shares an IP with an attacker is locked out along with them. Full list in [`docs/10-roadmap.md`](docs/10-roadmap.md) *(Portuguese)*.

## Documentation

The [`docs/`](docs/README.md) folder is the project's source of truth and is updated in the same commit that changes behavior. It is written in Brazilian Portuguese:

| Document | Contents |
|---|---|
| [01 · Overview](docs/01-visao-geral.md) | Goal, requirements, stack and architecture decisions (ADRs) |
| [02 · Architecture](docs/02-arquitetura.md) | Packages, request flow, process lifecycle, limits |
| [03 · Security](docs/03-seguranca.md) | Threat model, sandbox, authentication, CSRF, headers, public links, container |
| [04 · HTTP API](docs/04-api.md) | Every endpoint, payload and error code |
| [05 · Uploads](docs/05-uploads.md) | Upload modes, chunked sessions, resume, conflicts, cleanup |
| [06 · Database](docs/06-banco-de-dados.md) | SQLite schema, migrations, backup |
| [07 · Frontend](docs/07-frontend.md) | React structure, state, uploads, keyboard, theme, phone |
| [08 · Operations](docs/08-operacao.md) | Local, server, Easypanel, systemd, variables, proxies, upgrade, troubleshooting |
| [09 · Tests](docs/09-testes.md) | Automated suites and the manual release checklist |
| [10 · Roadmap](docs/10-roadmap.md) | Known limitations and next steps |

## Development

Requirements: Go (the version in `go.mod` is downloaded automatically by `go`), Node 22+.

```bash
make run     # API on :8080 serving ./data (cookies without Secure)
make dev     # Vite on :5173 proxying /api to :8080
make web     # frontend build into internal/server/webdist/dist
make build   # ./filezam binary with the embedded UI
make test    # go test ./... + tsc + vitest
make vuln    # govulncheck + npm audit
```

Two parts: `cmd/` and `internal/` (Go: `vfs` is the security core, `server` the HTTP handlers, `store` SQLite, `uploads` the upload protocol, `jobs` the background operations) and `web/` (React 19 + TypeScript + Vite + Tailwind 4, embedded in the binary via `go:embed`). Scripted API calls need the `X-Filezam: 1` header.

Binary subcommands: `serve` (default), `healthcheck` (used by Docker), `reset-admin [password]` (resets the admin password and forces a change at the next login), `version`.

GitHub CI runs the tests, `govulncheck`, `npm audit` and the image build on every PR; `v*` tags publish the multi-arch image to `ghcr.io/miguelzamberlan/filezam`. The manual checklist before a release is in [`docs/09-testes.md`](docs/09-testes.md#checklist-manual-antes-de-uma-versão) *(Portuguese)*.

## Contributing

Issues and pull requests are welcome, in English or Portuguese. [`CONTRIBUTING.md`](CONTRIBUTING.md) *(Portuguese)* explains how to set up the environment, the non-negotiable rules (every disk access through `internal/vfs`, paths never in URL segments, never serve user-uploaded HTML, docs updated in the same commit, tests for every endpoint that changes state) and the flow of a PR. For vulnerabilities, follow [`SECURITY.md`](SECURITY.md) instead of opening a public issue.

## Roadmap and limitations

What is known to be missing (resuming interrupted operations, passkeys) and what was dropped on purpose (full-text search, automatic upload resume) is in [`docs/10-roadmap.md`](docs/10-roadmap.md) *(Portuguese)*. Suggestions go through an issue before becoming code.

## Versions

The current version is **1.2.0**; the history is in [`CHANGELOG.md`](CHANGELOG.md) *(Portuguese)*. The project uses semantic versioning: published versions never change, and fixes and features ship as new versions. In production, pin a version (`ghcr.io/miguelzamberlan/filezam:1.2.0`, or `:1.2` to receive only fixes) instead of following `main`. Details in [`CONTRIBUTING.md`](CONTRIBUTING.md#versões-e-lançamentos) *(Portuguese)*.

## Author and license

**Miguel Zamberlan** ([@miguelzamberlan](https://github.com/miguelzamberlan)) is the author and maintainer: they set the priorities, review contributions and publish the releases. Filezam is developed in spare time, with the help of AI tools for review and implementation, and all code goes through automated tests and manual verification before reaching `main`.

[MIT](LICENSE) © 2026 Miguel Zamberlan. Use, modify and distribute freely, keeping the license notice.
