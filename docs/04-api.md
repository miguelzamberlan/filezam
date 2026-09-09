# 04 · API HTTP

Base: `/api`. Respostas em JSON (`Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store`, gzip automático acima de 1400 bytes se aceito). Corpos de requisição JSON limitados a 1 MiB, campos desconhecidos rejeitados.

Erros: `{"error":{"code":"...","message":"...", ...extras}}`. O `code` é estável e traduzido pelo frontend (`strings.ts`).

**Requisitos de toda requisição mutante** (`POST`, `PUT`, `PATCH`, `DELETE`): header `X-Filezam: 1` e origem do mesmo site (ver [03](03-seguranca.md#csrf)).

Colunas de autenticação: `-` pública · `S` sessão válida · `U` sessão + senha em dia · `A` `U` + admin · `P` token de share.

Convenções de caminho: sempre relativos ao escopo do usuário, separador `/`, sem barra inicial; `""` é a raiz. Passados em `?path=` ou no JSON, nunca no path da URL.

## Objetos

```jsonc
Entry    { name, type: "file"|"dir"|"other", size, mtime /*ms*/, link?: true, nameInvalid?: true }
User     { id, username, role: "admin"|"user", restricted: bool, mustChangePassword: bool }
Job      { id, type: "copy"|"move"|"delete", state: "running"|"done"|"failed"|"cancelled",
           done, total, bytesDone, bytesTotal, current?, error?, warnings?: [], startedAt, finishedAt?, dirs?: [] }
Upload   { id, dir, name, size, mtime, chunkSize, chunks, received: [int], overwrite, createdAt, updatedAt }
Favorite { id, path, name, createdAt }
Share    { id, path, name, createdBy, mine, createdAt, expiresAt, expired, accessCount, lastAccessAt }
AdminUser{ id, username, role, scope, mustChangePassword, disabled, lockedUntil, createdAt, updatedAt }
Audit    { id, ts, userId, username, ip, action, detail /*JSON string*/ }
```

Timestamps: `mtime` em milissegundos; os demais em segundos Unix.

## Sistema

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/health` | - | `{ok:true, version}`; 503 `db`/`root` se banco ou raiz indisponíveis |
| GET | `/api/config` | S | `{chunkSize, batchMaxFiles, batchMaxBytes, batchFileMax, maxParallel, shareMaxTtl, previewMaxText, version}` |

## Autenticação

| Método | Rota | Auth | Corpo → Resposta |
|---|---|---|---|
| POST | `/api/auth/login` | - | `{username, password}` → `{user}`; 401 `bad_credentials`, 423 `locked`, 429 `rate_limited`/`busy` |
| POST | `/api/auth/logout` | S | → `{ok}`; limpa o cookie |
| GET | `/api/auth/me` | S | → `{user}` |
| POST | `/api/auth/password` | S | `{current, new}` → `{user}`; 401 `bad_credentials`, 400 `weak_password`. Revoga as outras sessões |

## Arquivos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/files?path=` | U | `{path, entries: Entry[]}`. Partes de upload órfãs são removidas de passagem |
| GET | `/api/files/stat?path=` | U | `{path, entry}` |
| GET | `/api/files/info?path=` | U | Propriedades: `{path, entry, totals?: {files, dirs, bytes, partial}, shares?: Share[], favorite?: bool}`. `totals`/`shares`/`favorite` só para pastas; a contagem para em 200 000 entradas ou 15 s (`partial: true`). Shares: os do usuário, ou todos se admin |
| GET | `/api/files/disk` | U | `{total, free, used}` em bytes do sistema de arquivos da raiz do escopo (`statfs`; zeros se desconhecido) |
| GET | `/api/files/content?path=&inline=0\|1` | U | Conteúdo com suporte a `Range`; `inline=1` só para tipos permitidos ([03](03-seguranca.md)) |
| PUT | `/api/files/content?path=&mtime=&overwrite=0\|1` | U | Upload pequeno (corpo bruto ≤ `chunkSize`) → 201 `{path, entry}`; 409 `exists`, 413 `too_large`, 507 `no_space` |
| POST | `/api/files/batch?dir=&overwrite=` | U | Multipart (ver [05](05-uploads.md#lote)) → `{dir, results:[{path, ok, code?, error?, entry?}]}` |
| GET | `/api/files/zip?path=a&path=b&name=` | U | ZIP streaming (método Store) das entradas; `name` opcional para o arquivo |
| POST | `/api/files/mkdir` | U | `{path}` → 201 `{path, entry}`; cria pais; 409 `exists` |
| POST | `/api/files/rename` | U | `{path, newName}` → `{path, entry}`; 409 `exists`, 400 `invalid_name` |
| POST | `/api/files/delete` | U | `{paths: []}` → `{job}` (sync-or-job) |
| POST | `/api/files/copy` | U | `{sources: [], destDir, onConflict: "rename"\|"overwrite"\|"skip"}` → `{job}` |
| POST | `/api/files/move` | U | idem → `{job}`; rename atômico, fallback copiar+apagar entre dispositivos |

Validações de copy/move: `destDir` deve existir e ser pasta; cada origem deve existir; origem não pode conter o destino (400 `nested`); a raiz não pode ser origem (400 `root_op`). Até 10 000 caminhos por chamada (400 `too_many`).

Sync-or-job: o servidor aguarda até 300 ms; se o job terminou, `job.state` já vem `done`; senão vem `running` e o cliente consulta `/api/jobs/{id}`.

## Jobs

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/jobs` | U | `{jobs: Job[]}` do usuário (últimos 50) |
| GET | `/api/jobs/{id}` | U | `{job}`; 404 se de outro usuário |
| DELETE | `/api/jobs/{id}` | U | Cancela → `{ok}` |

## Uploads chunked

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| POST | `/api/uploads` | U | `{dir, name, size, mtime, overwrite}` → 201 `Upload`; 409 `exists` (destino existe e `overwrite=false`), 409 `upload_in_progress`, 507 `no_space` |
| GET | `/api/uploads` | U | `{uploads: Upload[]}` pendentes do usuário dentro do escopo |
| GET | `/api/uploads/{id}` | U | `Upload` |
| PUT | `/api/uploads/{id}?index=N` | U | Corpo bruto do bloco N com `Content-Length` exato → `Upload`; 400 `bad_index`/`bad_length`, 411 `length_required`, 413 `too_large` |
| POST | `/api/uploads/{id}/complete` | U | → `{entry}`; 409 `incomplete` com `missing: [int]` |
| DELETE | `/api/uploads/{id}` | U | Aborta e remove a parte → `{ok}` |

## Favoritos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/favorites` | U | `{favorites}` (só os dentro do escopo atual) |
| POST | `/api/favorites` | U | `{path, name?}` → 201 `{favorite}`; só pastas (409 `not_dir`); 409 `conflict` se repetido |
| DELETE | `/api/favorites/{id}` | U | → `{ok}` |

## Compartilhamentos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/shares` | U | `{shares, now}`; admin vê todos, usuário só os seus |
| POST | `/api/shares` | U | `{path, expiresIn /*s*/, name?}` → 201 `{share, token, url}`. O token só aparece aqui. 400 `bad_expiry`, 409 `not_dir` |
| DELETE | `/api/shares/{id}` | U | Dono ou admin → `{ok}` |

## Público (sem sessão)

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/public/{token}` | P | `{name, expiresAt, now}`; conta um acesso |
| GET | `/api/public/{token}/list?path=` | P | `{path, entries}` |
| GET | `/api/public/{token}/content?path=&inline=` | P | Conteúdo (mesmas regras de inline) |
| GET | `/api/public/{token}/zip?path=...` | P | ZIP; sem `path` compacta a pasta inteira |

Tudo responde 404 `not_found` para token inválido/expirado/revogado; 429 por rate limit.

## Administração

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/admin/users` | A | `{users: AdminUser[]}` |
| POST | `/api/admin/users` | A | `{username, password, role?, scope?, mustChangePassword?}` → 201 `{user}`; 400 `invalid_username`/`invalid_role`/`invalid_scope`/`weak_password`, 409 `exists` |
| PATCH | `/api/admin/users/{id}` | A | Qualquer de `{role, scope, disabled, password, mustChangePassword}` → `{user}`; 409 `last_admin`. Mudanças de role/scope/senha/disabled revogam sessões |
| DELETE | `/api/admin/users/{id}` | A | → `{ok}`; 409 `self`/`last_admin`; remove partes de upload pendentes |
| GET | `/api/admin/dirs?path=` | A | `{path, dirs: [string]}` só diretórios reais da **raiz base**, para o seletor de escopo |
| GET | `/api/admin/audit?before=&limit=` | A | `{entries}` mais recentes primeiro; `before` = id para paginar; `limit` ≤ 500 |

Regras de `username`: 2–64 caracteres de `A-Z a-z 0-9 . _ - @`, único sem distinção de caixa.

## SPA

| Rota | Descrição |
|---|---|
| `/assets/*` | Arquivos do build com `Cache-Control: public, max-age=31536000, immutable` |
| `/favicon.svg` e outros estáticos | `max-age=3600` |
| qualquer outra (`/b/*`, `/s/{token}/*`, `/login`, `/admin/*`) | `index.html` com CSP estrita e `no-store` |

## Códigos de erro

| HTTP | code | Quando |
|---|---|---|
| 400 | `bad_json`, `invalid_path`, `invalid_name`, `nested`, `root_op`, `bad_index`, `bad_length`, `bad_conflict`, `bad_expiry`, `bad_id`, `bad_meta`, `bad_multipart`, `no_paths`, `too_many`, `too_many_files`, `weak_password`, `invalid_username`, `invalid_role`, `invalid_scope`, `unsupported` | Entrada inválida |
| 401 | `unauthorized`, `bad_credentials` | Sem sessão / credenciais erradas |
| 403 | `forbidden`, `csrf`, `password_change_required`, `scope_unavailable`, `fs_permission` | Sem permissão |
| 404 | `not_found` | Caminho, job, share, usuário |
| 409 | `exists`, `is_dir`, `not_dir`, `conflict`, `cross_device`, `upload_in_progress`, `incomplete`, `last_admin`, `self` | Conflito de estado |
| 411 | `length_required` | Chunk sem `Content-Length` |
| 413 | `too_large` | Corpo maior que o limite |
| 423 | `locked` | Conta bloqueada |
| 429 | `rate_limited`, `busy` | Limite de taxa ou concorrência (`Retry-After: 1`) |
| 499 | `cancelled` | Cliente desistiu |
| 503 | `db`, `root` | Health |
| 507 | `no_space` | Disco cheio |
| 500 | `internal` | Erro inesperado (detalhes só no log) |
