# 04 · API HTTP

Base: `/api`. Respostas em JSON (`Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store`, gzip automático acima de 1400 bytes se aceito). Corpos de requisição JSON limitados a 1 MiB, campos desconhecidos rejeitados.

Erros: `{"error":{"code":"...","message":"...", ...extras}}`. O `code` é estável e traduzido pelo frontend (`web/src/i18n/*.ts`, chave `errorCodes`).

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
Share    { id, token, kind: "dir"|"file", hasPassword, path, name, createdBy, mine, createdAt, expiresAt, expired, accessCount, lastAccessAt }
AdminUser{ id, username, role, scope, mustChangePassword, disabled, lockedUntil, createdAt, updatedAt }
Audit    { id, ts, userId, username, ip, action, detail /*JSON string*/ }
```

Timestamps: `mtime` em milissegundos; os demais em segundos Unix.

## Sistema

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/health` | - | `{ok:true, version}`; 503 `db`/`root` se banco ou raiz indisponíveis |
| GET | `/api/config` | S | `{chunkSize, batchMaxFiles, batchMaxBytes, batchFileMax, maxParallel, shareMaxTtl, publicUrl, trashRetention, require2fa, previewMaxText, version}` (`trashRetention` em segundos; `0` = lixeira desativada) (`publicUrl` = `FILEZAM_PUBLIC_URL`, `""` quando não definido; `require2fa` = `FILEZAM_REQUIRE_2FA_ADMINS`) |

## Autenticação

| Método | Rota | Auth | Corpo → Resposta |
|---|---|---|---|
| POST | `/api/auth/login` | - | `{username, password}` → `{user}`, ou `{totpRequired: true, token}` quando a conta tem 2FA e não há cookie de dispositivo confiável; 401 `bad_credentials` (também para conta bloqueada, desativada ou inexistente), 429 `rate_limited`/`busy` |
| POST | `/api/auth/totp` | - | `{token, code, trust?}` → `{user}` + cookie de sessão (e `fz_trust` por 30 dias com `trust`). `code` = 6 dígitos ou código de recuperação. 401 `bad_totp` / `totp_expired`, 429 |
| POST | `/api/auth/totp/setup` | S | → `{secret, uri}` (segredo pendente por 10 min); 409 `totp_already_enabled` se o 2FA já está ativo (desative antes) |
| POST | `/api/auth/totp/enable` | S | `{password, code}` → `{user, recoveryCodes: [10]}`; 401 `bad_credentials`/`bad_totp`, 409 `totp_setup_expired`/`totp_already_enabled`, 429 `rate_limited`/`busy` (mesmos limites da troca de senha). Derruba as outras sessões |
| POST | `/api/auth/totp/disable` | S | `{password, code}` → `{user}`; 401 `bad_credentials`/`bad_totp`, 409 `totp_not_enabled` |
| POST | `/api/auth/totp/recovery` | S | `{password, code}` → `{recoveryCodes}` novos (os antigos morrem) |
| POST | `/api/auth/logout` | S | → `{ok}`; limpa o cookie |
| GET | `/api/auth/me` | S | → `{user}` |
| POST | `/api/auth/password` | S | `{current, new}` → `{user}`; 401 `bad_credentials`, 400 `weak_password`, 429 `rate_limited`/`busy` (mesmos limites do login). Revoga as outras sessões |

## Arquivos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/files?path=&limit=&offset=&sort=name\|size\|mtime\|type&dir=asc\|desc&hidden=0\|1` | U | Sem `limit`: `{path, entries: Entry[]}` com tudo. Com `limit` (≤ 5000): o servidor exclui ocultos (salvo `hidden=1`), ordena (pastas primeiro, nomes em ordem natural sem diferenciar maiúsculas) e devolve `{path, entries, total, offset, hidden}`; a interface pede páginas de 2000 conforme a rolagem. Partes de upload órfãs são removidas de passagem |
| GET | `/api/files/stat?path=` | U | `{path, entry}` |
| GET | `/api/files/info?path=` | U | Propriedades: `{path, entry, totals?: {files, dirs, bytes, partial}, shares?: Share[], favorite?: bool}`. `totals`/`shares`/`favorite` só para pastas; a contagem para em 200 000 entradas ou 15 s (`partial: true`). Shares: os do usuário, ou todos se admin |
| GET | `/api/files/search?path=&q=&limit=` | U | Pesquisa por nome (substring, sem diferenciar maiúsculas) a partir de `path` (pasta; 409 `not_dir`), dentro do escopo: `{path, q, results: [{dir, entry}], partial, source: "index"\|"walk", indexedAt}`. Com o índice de nomes pronto (`source: index`) a resposta vem do SQLite e cada acerto é conferido no disco (fantasmas somem); senão percorre o disco (`walk`). `dir` é a pasta do item relativa ao escopo. Para em 500 resultados (`limit` ≤ 500), 200 000 entradas visitadas ou 10 s → `partial: true`. Symlinks não são seguidos; nomes `.filezam-*` nunca aparecem. 400 `bad_query` sem `q` (máx. 255 bytes); 429 `busy` acima de 2 pesquisas simultâneas por usuário |
| GET | `/api/files/disk` | U | `{total, free, used}` em bytes do sistema de arquivos da raiz do escopo (`statfs`; zeros se desconhecido); com cota, também `{quota, quotaUsed}` |
| GET | `/api/files/content?path=&inline=0\|1` | U | Conteúdo com suporte a `Range`; `inline=1` só para tipos permitidos ([03](03-seguranca.md)) |
| PUT | `/api/files/content?path=&mtime=&overwrite=0\|1` | U | Upload pequeno (corpo bruto ≤ `chunkSize`) → 201 `{path, entry}`; 409 `exists`, 413 `too_large`, 507 `no_space` |
| POST | `/api/files/batch?dir=&overwrite=` | U | Multipart (ver [05](05-uploads.md#lote)) → `{dir, results:[{path, ok, code?, error?, entry?}]}` |
| GET | `/api/files/zip?path=a&path=b&name=` | U | ZIP streaming (método Store) das entradas; `name` opcional para o arquivo. Máximo 2 simultâneos por usuário (429 `busy`) |
| POST | `/api/files/mkdir` | U | `{path}` → 201 `{path, entry}`; cria pais; 409 `exists`, 400 `invalid_name` (caracteres de controle) |
| POST | `/api/files/rename` | U | `{path, newName}` → `{path, entry}`; 409 `exists`, 400 `invalid_name` |
| POST | `/api/files/delete` | U | `{paths: [], permanent?: bool}` → `{job}` (sync-or-job). Com a lixeira ativa e sem `permanent`, os itens vão para `<escopo>/.filezam-trash/<id>/<nome>` e ganham uma linha em `trash`; senão são apagados |
| POST | `/api/files/copy` | U | `{sources: [], destDir, onConflict: "rename"\|"overwrite"\|"skip"}` → `{job}`; o job falha com "disk quota exceeded" se a cópia estourar a cota |
| POST | `/api/files/move` | U | idem → `{job}`; rename atômico, fallback copiar+apagar entre dispositivos |

Validações de copy/move: `destDir` deve existir e ser pasta; cada origem deve existir; origem não pode conter o destino (400 `nested`); a raiz não pode ser origem (400 `root_op`). Até 10 000 caminhos por chamada (400 `too_many`).

Sync-or-job: o servidor aguarda até 300 ms; se o job terminou, `job.state` já vem `done`; senão vem `running` e o cliente consulta `/api/jobs/{id}`.

## Jobs

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/jobs/history?limit=` | U | Jobs persistidos do usuário (últimos 30 dias, mais recentes primeiro, `limit` ≤ 500): `{jobs: [{id, type, label, state, done, total, bytesDone, bytesTotal, error?, warnings, startedAt, finishedAt}]}`. Jobs em andamento aparecem com progresso de até 1 s atrás; os que um reinício interrompeu ficam `failed` com `error: "interrupted by server restart"` |
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

## Lixeira

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/trash` | U | `{items: [{id, name, path, type, size, deletedAt, by}], retention}`. `path` é o local original relativo ao escopo; usuário vê os próprios itens, admin vê todos os que estão dentro do seu escopo; `retention` em segundos |
| POST | `/api/trash/restore` | U | `{ids: []}` → `{restored: [{id, path}], failed: [{id, code}]}`. Recria a pasta original se preciso; se já existir algo com o nome, restaura como `nome (n)` |
| POST | `/api/trash/delete` | U | `{ids: []}` → `{deleted}` (permanente) |
| POST | `/api/trash/empty` | U | → `{deleted}`: apaga todos os itens visíveis |

Itens mais antigos que `FILEZAM_TRASH_RETENTION` são apagados pela varredura horária. Ids inválidos ou fora do escopo são ignorados em silêncio.

## Compartilhamentos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/shares` | U | `{shares, now}`; admin vê todos, usuário só os seus. `share.token` permite recopiar o link (`""` em links criados antes da migração 002) |
| POST | `/api/shares` | U | `{path, expiresIn /*s*/, name?, password?}` → 201 `{share, token, url}`. `path` pode ser pasta (`kind: dir`) ou arquivo (`kind: file`); `password` opcional com 4–256 caracteres (400 `weak_password`), guardada como Argon2id. `url` usa `FILEZAM_PUBLIC_URL` ou o host da requisição; a interface web monta o link a partir de `token` + origem do navegador. 400 `bad_expiry`, 409 `not_dir` |
| DELETE | `/api/shares/{id}` | U | Dono ou admin → `{ok}` |

Links de um usuário desativado respondem 404 enquanto ele estiver desativado. Em `?path=` (autenticado ou público) nomes `.filezam-*` são 400 `invalid_path` também na leitura.

## Público (sem sessão)

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/public/{token}` | P | `{name, kind, expiresAt, now, locked}` (+ `size, mtime, fileName` para arquivo); conta um acesso quando destravado. `locked: true` quando o link tem senha e o cookie de unlock não veio. 404 se inexistente, expirado, revogado, item sumiu ou dono desativado |
| POST | `/api/public/{token}/unlock` | P | `{password}` → `{ok}` e cookie `fz_s_<hash16>` (HMAC do token, 24 h). 401 `bad_credentials`; 5/min por (link, IP) e `loginSem` (Argon2). Exige `X-Filezam: 1` |
| GET | `/api/public/{token}/list?path=` | P | `{path, entries}`; 401 `share_locked` com senha pendente; 409 `not_dir` em link de arquivo |
| GET | `/api/public/{token}/content?path=&inline=` | P | Conteúdo (mesmas regras de inline). Em link de arquivo `path` é ignorado: serve sempre o arquivo compartilhado. 401 `share_locked` |
| GET | `/api/public/{token}/zip?path=...` | P | ZIP; sem `path` compacta a pasta inteira. 401 `share_locked`; 409 `not_dir` em link de arquivo |

Tudo responde 404 `not_found` para token inválido/expirado/revogado; 429 por rate limit.

## Administração

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/admin/users` | A | `{users: AdminUser[]}` |
| POST | `/api/admin/users` | A | `{username, password, role?, scope?, mustChangePassword?, quota?}` → 201 `{user}`; 400 `invalid_username`/`invalid_role`/`invalid_scope`/`weak_password`, 409 `exists` |
| PATCH | `/api/admin/users/{id}` | A | Qualquer de `{role, scope, disabled, password, mustChangePassword, quota}` (`quota` em bytes, 0 = sem limite; 400 `bad_quota`) → `{user}`; 409 `last_admin`. Mudanças de role/scope/senha/disabled revogam sessões; estreitar `scope` apaga os links públicos do usuário que ficaram fora dele (`sharesRevoked` no evento de auditoria) |
| POST | `/api/admin/users/{id}/totp/reset` | A | Remove o 2FA do usuário e derruba as sessões dele → `{ok}` |
| DELETE | `/api/admin/users/{id}` | A | → `{ok}`; 409 `self`/`last_admin`; remove partes de upload pendentes |
| GET | `/api/admin/dirs?path=` | A | `{path, dirs: [string]}` só diretórios reais da **raiz base**, para o seletor de escopo |
| GET | `/metrics` | token | Prometheus text format; exige `FILEZAM_METRICS_TOKEN` (`Authorization: Bearer`) ou sessão admin; 404 quando desativado |
| GET | `/api/admin/index` | A | `{enabled, ready, running, entries, lastFullAt, interval}` do índice de nomes |
| POST | `/api/admin/reindex` | A | Inicia uma varredura completa → `{started}` (`false` se já roda); 409 `unsupported` com índice desativado |
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
| 400 | `bad_json`, `invalid_path` (inclui tamanho de upload fora de `[0, 1 PiB]` e nomes `.filezam-*`), `invalid_name` (inclui caracteres de controle em nomes novos), `bad_query`, `nested`, `root_op`, `bad_index`, `bad_length`, `bad_conflict`, `bad_expiry`, `bad_id`, `bad_meta`, `bad_multipart`, `no_paths`, `too_many`, `too_many_files`, `weak_password`, `invalid_username`, `invalid_role`, `invalid_scope`, `bad_quota`, `unsupported` | Entrada inválida |
| 401 | `unauthorized`, `bad_credentials`, `share_locked`, `bad_totp`, `totp_expired` | Sem sessão / credenciais erradas / link com senha pendente / código 2FA inválido ou etapa expirada |
| 403 | `forbidden`, `csrf`, `password_change_required`, `totp_required`, `scope_unavailable`, `fs_permission` | Sem permissão |
| 404 | `not_found` | Caminho, job, share, usuário |
| 409 | `exists`, `is_dir`, `not_dir`, `conflict`, `cross_device`, `upload_in_progress`, `incomplete`, `last_admin`, `self`, `totp_setup_expired`, `totp_not_enabled`, `totp_already_enabled` | Conflito de estado |
| 411 | `length_required` | Chunk sem `Content-Length` |
| 413 | `too_large` | Corpo maior que o limite |
| 507 | `no_space`, `quota_exceeded` | Disco cheio / cota do usuário estourada |
| 429 | `rate_limited`, `busy` | Limite de taxa ou concorrência (`Retry-After: 1`) |
| 499 | `cancelled` | Cliente desistiu |
| 503 | `db`, `root` | Health |
| 507 | `no_space` | Disco cheio |
| 500 | `internal` | Erro inesperado (detalhes só no log) |
