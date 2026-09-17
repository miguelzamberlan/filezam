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
Share    { id, token, slug, mode: "read"|"drop", kind: "dir"|"file", hasPassword, revoked, path, name, createdBy, mine, createdAt, expiresAt, expired, accessCount, lastAccessAt,
           quotaBytes, usedBytes, fileCount, maxFileBytes, maxFiles /*só mode "drop"*/, brokenSince?, revokeAt? }
Settings { slugsEnabled, dropEnabled, dropMaxQuota, dropMaxTtl, dropFileMax, dropMaxFiles, dropMaxLinks, dropStaleAge,
           extractEnabled, extractMaxBytes, extractMaxEntries, extractMaxArchive,
           thumbsEnabled, thumbsMaxPixels, thumbsMaxFile, thumbsCacheMax,
           trashRetention /*s, 0..365 d; 0 = lixeira desligada*/, indexInterval /*s, 15 min..7 d*/ }
AdminUser{ id, username, role, scope, mustChangePassword, disabled, lockedUntil, createdAt, updatedAt }
Audit    { id, ts, userId, username, ip, action, detail /*JSON string*/ }
```

Timestamps: `mtime` em milissegundos; os demais em segundos Unix.

## Sistema

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/health` | - | `{ok:true, version}`; 503 `db`/`root` se banco ou raiz indisponíveis |
| GET | `/api/config` | S | `{chunkSize, batchMaxFiles, batchMaxBytes, batchFileMax, maxParallel, shareMaxTtl, publicUrl, trashRetention, require2fa, previewMaxText, version, slugsEnabled, dropEnabled, dropMaxTtl, dropMaxQuota, dropFileMax, dropMaxFiles}` (`trashRetention` em segundos; `0` = lixeira desativada) (`publicUrl` = `FILEZAM_PUBLIC_URL`, `""` quando não definido; `require2fa` = `FILEZAM_REQUIRE_2FA_ADMINS`) |

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
| GET | `/api/files/search?path=&q=&limit=` | U | Pesquisa por nome (substring comparada por `vfs.Fold`: sem diferenciar maiúsculas, acentos e cedilha, com NFD/NFC e ligaduras igualados — `acao` acha `Ação.pdf` e `AÇÃO` acha `acao.txt`) a partir de `path` (pasta; 409 `not_dir`), dentro do escopo: `{path, q, results: [{dir, entry}], partial, source: "index"\|"walk", indexedAt}`. Com o índice de nomes pronto (`source: index`) a resposta vem do SQLite e cada acerto é conferido no disco (fantasmas somem); senão percorre o disco (`walk`). `dir` é a pasta do item relativa ao escopo. Para em 500 resultados (`limit` ≤ 500), 200 000 entradas visitadas ou 10 s → `partial: true`. Symlinks não são seguidos; nomes `.filezam-*` nunca aparecem. 400 `bad_query` sem `q` (máx. 255 bytes) ou com `q` que dobra para vazio (só marcas de acento); 429 `busy` acima de 2 pesquisas simultâneas por usuário |
| GET | `/api/files/disk` | U | `{total, free, used}` em bytes do sistema de arquivos da raiz do escopo (`statfs`; zeros se desconhecido); com cota, também `{quota, quotaUsed}` |
| GET | `/api/files/thumb?path=&v=` | U | JPEG de no máximo 256 px do lado maior, gerado na primeira vez e guardado em cache. `v` é o `mtime` do arquivo: ele versiona a URL, e a resposta vai com `private, max-age=300` + `ETag` — a única resposta de conteúdo que o navegador guarda, e por pouco tempo de propósito (ver `docs/03-seguranca.md`). 404 `no_thumb` quando o formato não tem decodificador, a imagem passa dos limites ou o recurso está desligado (o cliente cai no ícone); 429 `busy` só depois de esperar por um slot |
| GET | `/api/files/content?path=&inline=0\|1` | U | Conteúdo com suporte a `Range`; `inline=1` só para tipos permitidos ([03](03-seguranca.md)) |
| PUT | `/api/files/content?path=&mtime=&overwrite=0\|1&ifMtime=` | U | Upload pequeno (corpo bruto ≤ `chunkSize`) → 201 `{path, entry}`, com `path` o caminho gravado de fato (substituir um nome equivalente fica com o nome que já estava lá; pastas equivalentes são reaproveitadas); 409 `exists`, 413 `too_large`, 507 `no_space`. `ifMtime` (ms) é conferido imediatamente antes de publicar e recusa com 409 `modified` se o arquivo mudou desde então — é o que o editor usa para não sobrescrever em silêncio quem salvou primeiro; resolução de 1 ms |
| POST | `/api/files/batch?dir=&overwrite=` | U | Multipart (ver [05](05-uploads.md#lote)) → `{dir, results:[{path, ok, code?, error?, entry?}]}` |
| GET | `/api/files/zip?path=a&path=b&name=&from=&to=` | U | ZIP streaming (método Store) das entradas, em ordem de pasta e nome; `name` opcional para o arquivo. Com `from`/`to` (nomes de entrada, de `zip/plan`) só as entradas em `[from, to)`: uma parte de um download dividido, que é um zip completo. Máximo 2 simultâneos por usuário (429 `busy`) |
| GET | `/api/files/zip/plan?path=a&path=b` | U | `{partSize, parts: [{from, to, files, bytes}], files, bytes}`: como o zip se divide em partes de até 2 GiB de conteúdo (um arquivo maior vai sozinho). Uma parte só = baixar direto. As fronteiras são nomes, então uma mudança na pasta entre o plano e as partes não duplica arquivo. Varredura com até 60 s (503 `timeout`) |
| POST | `/api/files/mkdir` | U | `{path}` → 201 `{path, entry}`; cria pais; 409 `exists`, 400 `invalid_name` (caracteres de controle) |
| POST | `/api/files/rename` | U | `{path, newName}` → `{path, entry}`; 409 `exists`, 400 `invalid_name`. Revoga os links públicos do item e do que estiver dentro dele |
| POST | `/api/files/delete` | U | `{paths: [], permanent?: bool}` → `{job}` (sync-or-job). Com a lixeira ativa e sem `permanent`, os itens vão para `<escopo>/.filezam-trash/<id>/<nome>` e ganham uma linha em `trash`; senão são apagados. Os links públicos de cada item (e do que estiver dentro) são revogados antes; restaurar da lixeira não os traz de volta |
| POST | `/api/files/copy` | U | `{sources: [], destDir, onConflict: "rename"\|"overwrite"\|"skip"}` → `{job}`; o job falha com "disk quota exceeded" se a cópia estourar a cota |
| POST | `/api/files/extract` | U | `{path}` → `{job}`. Extrai um `.zip` para uma pasta nova ao lado, com o nome do arquivo (`UniqueName` se já existir); nada preexistente é sobrescrito. 400 `bad_archive` (extensão ou conteúdo), 413 `archive_too_large` (arquivo acima de `extractMaxArchive`, índice acima de 64 MiB ou de `extractMaxEntries` entradas, ou total declarado acima de `extractMaxBytes` — tudo antes de criar o job), 400 `archive_encrypted` (todo arquivo do zip protegido por senha — o extrator não decifra; recusado antes de criar a pasta), 409 `is_dir`, 429 `busy`, 403 `feature_disabled`. Entradas recusadas (travessia, symlink, cifradas num zip que tem outras legíveis, duplicadas) viram aviso do job; estourar os tetos falha o job e apaga a pasta |
| POST | `/api/files/archive` | U | `{paths, name?}` → `{job}`. Gera um `.zip` na pasta das origens; o nome ganha `.zip` e desvia com `UniqueName` se estiver ocupado |
| POST | `/api/files/move` | U | idem → `{job}`; rename atômico, fallback copiar+apagar entre dispositivos. Revoga os links públicos de cada item movido |

Validações de copy/move: `destDir` deve existir e ser pasta; cada origem deve existir; origem não pode conter o destino (400 `nested`); a raiz não pode ser origem (400 `root_op`). Até 10 000 caminhos por chamada (400 `too_many`).

Sync-or-job: o servidor aguarda até 300 ms; se o job terminou, `job.state` já vem `done`; senão vem `running` e o cliente consulta `/api/jobs/{id}`.

## Jobs

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/jobs/history?limit=` | U | Jobs persistidos do usuário (últimos 30 dias, mais recentes primeiro, `limit` ≤ 500): `{jobs: [{id, type, label, state, done, total, bytesDone, bytesTotal, error?, warnings, startedAt, finishedAt}]}`. Jobs em andamento aparecem com progresso de até 1 s atrás; os que um reinício interrompeu ficam `failed` com `error: "interrupted by server restart"`, e depois da limpeza ganham o que aconteceu (`…; partial copy of N files: …`, `…; the unfinished copy was removed`, `…; the partial extraction was removed`, `…; the unfinished .zip was removed`; textos estáveis, traduzidos pela interface) |
| GET | `/api/notifications?limit=` | U | `{notifications: [{id, kind, data, createdAt, updatedAt, readAt}], unread}` do próprio usuário, mais recentes primeiro (`limit` ≤ 200, padrão 50). `kind`: `drop.received` (`data: {shareId, link, path, files, bytes, names[≤5]}`, somado enquanto não lida) ou `share.revoked` (`data: {shareId, link, path, slug, mode, reason: "missing"}`). `path` sai relativo ao escopo, ou ausente se ficou fora dele |
| GET | `/api/notifications/unread` | U | `{unread}` |
| POST | `/api/notifications/read` | U | `{ids: [..]}` ou `{all: true}` → `{unread}`; só as do próprio usuário. 400 `bad_id`/`too_many` |
| DELETE | `/api/notifications/{id}` | U | `{ok}`; 404 se não é do usuário |
| GET | `/api/jobs` | U | `{jobs: Job[]}` do usuário (últimos 50) |
| GET | `/api/jobs/{id}` | U | `{job}`; 404 se de outro usuário |
| DELETE | `/api/jobs/{id}` | U | Cancela → `{ok}` |

## Uploads chunked

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| POST | `/api/uploads` | U | `{dir, name, size, mtime, overwrite}` → 201 `Upload` (`dir`/`name` já resolvidos para o que existe no disco); 409 `exists` (destino, ou nome equivalente, existe e `overwrite=false`), 409 `upload_in_progress` (o mesmo usuário já tem sessão para o destino, com o mesmo arquivo ou recebendo blocos agora; uma sessão parada de **outro** arquivo é substituída pela nova, e sessões de outros usuários não conflitam — ver [05](05-uploads.md#criar-sessão)), 413 `upload_reserve_exceeded` (as sessões abertas do usuário já reservam `FILEZAM_UPLOAD_MAX_RESERVED`), 507 `no_space` |
| GET | `/api/uploads` | U | `{uploads: Upload[]}` pendentes do usuário dentro do escopo |
| GET | `/api/uploads/{id}` | U | `Upload` |
| PUT | `/api/uploads/{id}?index=N` | U | Corpo bruto do bloco N com `Content-Length` exato → `Upload`; 400 `bad_index`/`bad_length`, 411 `length_required`, 413 `too_large` |
| POST | `/api/uploads/{id}/complete` | U | → `{entry}`; 409 `incomplete` com `missing: [int]`; 409 `exists` se o destino surgiu (inclusive por outro usuário que finalizou antes) e `overwrite=false` |
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

Itens mais antigos que a retenção (`trashRetention` das configurações; sem valor gravado, `FILEZAM_TRASH_RETENTION`) são apagados pela varredura horária. Ids inválidos ou fora do escopo são ignorados em silêncio.

## Compartilhamentos

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/shares` | U | `{shares, now}`; `brokenSince`/`revokeAt` aparecem quando a manutenção não acha o item (o link é revogado em `revokeAt` se ele não voltar); admin vê todos, usuário só os seus. `share.token` permite recopiar o link (`""` em links criados antes da migração 002). Links revogados só aparecem quando têm apelido, com `revoked: true`, para o dono ver o endereço que segue reservado a ele |
| POST | `/api/shares` | U | `{path, expiresIn /*s*/, name?, password?, slug?, mode?, quotaBytes?, maxFileBytes?, maxFiles?}` → 201 `{share, token, url}`. `path` pode ser pasta (`kind: dir`) ou arquivo (`kind: file`); `password` opcional com 8–256 caracteres (400 `weak_password`), guardada como Argon2id. `url` usa `FILEZAM_PUBLIC_URL` ou o host da requisição; a interface web monta o link a partir de `slug \|\| token` + origem do navegador. 400 `bad_expiry`, 409 `not_dir` |
| POST | `/api/shares/affected` | U | `{paths: [escopo-relativos]}` → `{count, links: [{path, mode, slug?}]}`: links vivos (não revogados nem vencidos), de qualquer dono, do item ou de algo abaixo dele, que excluir, mover ou renomear esses caminhos revogaria. `links` traz no máximo 20; `count` é exato. Sem token nem autor. 400 `no_paths`/`too_many`/`invalid_path`/`root_op` |
| DELETE | `/api/shares/{id}?purge=` | U | Dono ou admin → `{ok}`. Link com apelido é **revogado**, não apagado: o endereço continua reservado a quem o criou. `?purge=1` apaga a linha e libera o apelido |

Links de um usuário desativado respondem 404 enquanto ele estiver desativado. Em `?path=` (autenticado ou público) nomes `.filezam-*` são 400 `invalid_path` também na leitura.

### Apelido (`slug`)

`slug` troca o token por um endereço legível (`/s/orcamento-2026`). São 3–63 caracteres em
`[a-z0-9-]`, sem hífen nas pontas, únicos em toda a instalação e fora da lista reservada (`admin`,
`login`, `filezam`, `s`, `api`) — 400 `invalid_slug`, 409 `slug_taken`. Como o endereço é
adivinhável, **senha é obrigatória** (400 `password_required`); o sigilo do link passa a morar nela.
Depende de `slugs_enabled` nas configurações (403 `feature_disabled`).

### Link de envio (`mode: "drop"`)

Depende de `drop_enabled` (403 `feature_disabled`). Sempre `kind: dir`, e:

- `path` **não pode ser a raiz do escopo** (400 `root_op`) e a pasta é criada no ato; se já existir,
  precisa estar vazia — inclusive de partes de upload (409 `not_empty`).
- `expiresIn` é obrigatório e vale no máximo `min(drop_max_ttl, 30 dias, FILEZAM_SHARE_MAX_TTL)`
  (400 `bad_expiry`).
- `quotaBytes` é obrigatório, limitado por `drop_max_quota` e pela cota restante do dono
  (400 `bad_quota`). `maxFileBytes` e `maxFiles` caem no padrão das configurações.
- 409 `drop_links_exceeded` quando o usuário já tem `drop_max_links` links de envio ativos.

## Público (sem sessão)

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/public/{token}` | P | Travado, devolve só `{kind, mode, expiresAt, now, locked: true}` — nem o nome, que com apelido seria um oráculo de enumeração. Destravado, acrescenta `name` (+ `size, mtime, fileName` para arquivo; + os campos de envio abaixo para `mode: drop`) e conta um acesso. 404 se inexistente, expirado, revogado, item sumiu ou dono desativado |
| POST | `/api/public/{token}/unlock` | P | `{password}` → `{ok}` e cookie `fz_s_<hash16>` (HMAC do token, 24 h). 401 `bad_credentials`; 5/min por (link, IP) e `loginSem` (Argon2). Exige `X-Filezam: 1` |
| GET | `/api/public/{token}/list?path=` | P | `{path, entries}`; 401 `share_locked` com senha pendente; 409 `not_dir` em link de arquivo; **404 em link de envio** |
| GET | `/api/public/{token}/content?path=&inline=` | P | Conteúdo (mesmas regras de inline). Em link de arquivo `path` é ignorado: serve sempre o arquivo compartilhado. 401 `share_locked`. Com 2 downloads/zips do mesmo IP em andamento, espera um slot até 30 s e só então responde 429 `busy` |
| GET | `/api/public/{token}/zip/plan?path=...` | P | O plano de partes do zip público, com as mesmas regras de `files/zip/plan`; ocupa um dos 2 slots de download do IP enquanto mede |
| GET | `/api/public/{token}/zip?path=...&name=&from=&to=` | P | ZIP; sem `path` compacta a pasta inteira; `from`/`to` e `name` como em `files/zip`. 401 `share_locked`; 409 `not_dir` em link de arquivo; divide com `content` os 2 slots por IP e tem teto de 4 zips públicos simultâneos no servidor (mesma espera de até 30 s, depois 429 `busy`) |

`{token}` aceita o token de 43 caracteres ou o apelido. Um apelido errado consome um balde próprio
(30/min por IP) antes do 404, então varrer endereços trava rápido e quem tem o link certo nunca paga
por isso.

Tudo responde 404 `not_found` para token inválido/expirado/revogado; 429 por rate limit. Um link de
envio responde 404 nas rotas de leitura e um link de leitura responde 404 nas de escrita: a
invariante de "mesma resposta para tudo que não existe" vale nos dois sentidos.

## Envio anônimo (link `mode: drop`)

Todas exigem `X-Filezam: 1` e origem do mesmo site, como `unlock`, e 401 `share_locked` enquanto a
senha do link não foi destravada.

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| PUT | `/api/public/{token}/content?name=&mtime=` | P | Corpo bruto, até `chunkSize` → 201 `{name, size}`. O `name` devolvido é sempre **o que foi pedido**, mesmo quando o servidor precisou gravar como `nome (1).ext` para não sobrescrever: revelar o desvio faria do endpoint um teste de existência de nome na pasta. 413 `too_large` acima do bloco |
| POST | `/api/public/{token}/uploads` | P | `{name, size, mtime}` → 201 `Upload` (sem `dir`: o link é plano) |
| PUT | `/api/public/{token}/uploads/{id}?index=` | P | Um bloco, `Content-Length` exato; 411 `length_required`, 400 `bad_index`/`bad_length` |
| POST | `/api/public/{token}/uploads/{id}/complete` | P | → `{name, size}` (o nome pedido, como no PUT); 409 `incomplete {missing}` |
| DELETE | `/api/public/{token}/uploads/{id}` | P | Cancela e devolve o espaço reservado |

O `GET /api/public/{token}` destravado de um link de envio traz o que o cliente precisa sem depender
de `/api/config` (que exige sessão): `{quotaBytes, usedBytes, fileCount, maxFileBytes, maxFiles,
chunkSize, maxParallel, mine: [{name, size, at}]}`. `mine` lista **apenas os envios do próprio
visitante**, identificado pelo cookie `fz_d_<hash16>` assinado pelo servidor; adulterá-lo só faz o
servidor emitir uma identidade nova. Os nomes em `mine` são os que o visitante pediu, não os que
estão no disco.

Um link cujo recurso o administrador desligou responde **404 em todas as rotas**, inclusive as que
já existiam antes de ser desligado.

Limites: 507 `drop_full` (cota do link **ou** do dono — indistinguíveis de propósito), 413
`drop_file_limit`, 409 `drop_count_exceeded` (teto de arquivos do link, ou 8 sessões abertas do mesmo
visitante). Sessões abertas contam na cota enquanto existem e são descartadas após `drop_stale_age`.
A escrita usa um balde de requisições próprio (o de leitura, 120/min, mataria um envio legítimo de
algumas dezenas de arquivos) e 2 envios simultâneos por IP, com a mesma espera de 30 s dos downloads
antes de 429 `busy`.

## Administração

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| GET | `/api/admin/users` | A | `{users: AdminUser[]}` |
| POST | `/api/admin/users` | A | `{username, password, role?, scope?, mustChangePassword?, quota?}` → 201 `{user}`; 400 `invalid_username`/`invalid_role`/`invalid_scope`/`weak_password`, 409 `exists` |
| PATCH | `/api/admin/users/{id}` | A | Qualquer de `{role, scope, disabled, password, mustChangePassword, quota}` (`quota` em bytes, 0 = sem limite; 400 `bad_quota`) → `{user}`; 409 `last_admin`. Mudanças de role/scope/senha/disabled revogam sessões; estreitar `scope` apaga os links públicos do usuário que ficaram fora dele (`sharesRevoked` no evento de auditoria) |
| POST | `/api/admin/users/{id}/totp/reset` | A | Remove o 2FA do usuário e derruba as sessões dele → `{ok}` |
| DELETE | `/api/admin/users/{id}` | A | → `{ok}`; 409 `self`/`last_admin`; remove partes de upload pendentes |
| GET | `/api/admin/settings` | A | `{settings, dropTtlHardMax}` |
| PATCH | `/api/admin/settings` | A | Campos parciais de `Settings` → `{settings, dropTtlHardMax}`. Fora de faixa → 400 `bad_quota`; `dropMaxTtl` nunca passa de 30 dias, mesmo com o banco editado à mão. `trashRetention` e `indexInterval` sem valor gravado saem com o das variáveis de ambiente; mudar `indexInterval` remarca a próxima varredura na hora (a partir do fim da anterior). Auditado como `settings.update` |
| GET | `/api/admin/dirs?path=` | A | `{path, dirs: [string]}` só diretórios reais da **raiz base**, para o seletor de escopo |
| GET | `/metrics` | token | Prometheus text format; exige `FILEZAM_METRICS_TOKEN` (`Authorization: Bearer`) ou sessão admin; 404 quando desativado |
| GET | `/api/admin/index` | A | `{enabled, ready, running, entries, lastFullAt, interval, lastMs}` do índice de nomes (`lastMs`: duração da última varredura completa deste processo, 0 antes da primeira) |
| POST | `/api/admin/reindex` | A | Inicia uma varredura completa → `{started}` (`false` se já roda); 409 `unsupported` com índice desativado. Ao terminar, a próxima varredura periódica passa a contar a partir dela (botão **Varrer agora** em Configurações do sistema e **Reconstruir índice** na Pesquisa) |
| GET | `/api/admin/audit?before=&limit=` | A | `{entries}` mais recentes primeiro; `before` = id para paginar; `limit` ≤ 500 |
| GET | `/api/admin/dashboard` | A | Retrato do servidor para o **Painel**, montado na hora: `{now, activeUsers, users, sessions, activity, uploads, jobs, shareRefs, shares, logins24h, drops7d, disk, trash, index}`. `users`: `{id, username, role, scope, disabled, locked, totp, quota, used, sessions, lastSeenAt, shares, dropShares}`, com `used` `null` enquanto o índice de nomes não está pronto (medir varrendo o disco a cada atualização pesaria demais). `sessions`: sessões não vencidas `{id, userId, ip, userAgent, createdAt, lastSeenAt, current}`; `activeUsers` conta quem apareceu nos últimos 10 min (o último acesso só é gravado a cada 5). `activity`: envios em andamento, só em memória, `{userId, shareId?, files, bytes, since, lastAt, inflight}` — uma rodada por usuário ou link de recebimento, que soma lote, PUT único e blocos até 2 min sem envio. A gravação do editor não conta. `uploads`: sessões em blocos abertas, inclusive de links de recebimento, `{userId, shareId?, path, size, received, createdAt, updatedAt}`, com `path` relativo ao escopo do admin quando cabe nele (mesmo critério de `/api/shares`). `jobs`: operações em andamento de todos `{id, userId, type, label, done, total, bytesDone, bytesTotal, startedAt}`. `shareRefs`: `{id, name, mode, ownerId}` dos links citados em `activity`/`uploads`. `shares`: `{read, drop, broken, expiring, created7d}` (no ar; `expiring` = vence em 3 dias). `logins24h`: `{ok, failed, locked}` da auditoria (`failed` soma `login.fail` e `totp.fail`). `drops7d`: `{files, bytes}` recebidos por links |
| DELETE | `/api/admin/sessions/{id}` | A | Encerra a sessão `id` (o de `dashboard.sessions`) → `{ok}`; 404 `not_found`; 409 `self` para a própria sessão (use o logout). Auditado como `session.revoke` com o dono e o IP |

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
| 400 | `bad_json`, `incomplete_body` (a transferência acabou antes dos bytes prometidos), `invalid_path` (inclui tamanho de upload fora de `[0, 1 PiB]` e nomes `.filezam-*`), `invalid_name` (inclui caracteres de controle e os controles bidi U+202A–U+202E/U+2066–U+2069 em nomes novos), `bad_query`, `nested`, `root_op`, `bad_index`, `bad_length`, `bad_conflict`, `bad_expiry`, `bad_id`, `bad_meta`, `bad_multipart`, `no_paths`, `too_many`, `too_many_files`, `weak_password`, `bad_archive`, `archive_encrypted`, `invalid_slug`, `password_required`, `bad_mode`, `invalid_username`, `invalid_role`, `invalid_scope`, `bad_quota`, `unsupported` | Entrada inválida |
| 401 | `unauthorized`, `bad_credentials`, `share_locked`, `bad_totp`, `totp_expired` | Sem sessão / credenciais erradas / link com senha pendente / código 2FA inválido ou etapa expirada |
| 403 | `forbidden`, `csrf`, `feature_disabled`, `password_change_required`, `totp_required`, `scope_unavailable`, `fs_permission` | Sem permissão |
| 404 | `not_found`, `no_thumb` | Caminho, job, share, usuário; `no_thumb` não é erro para o usuário, é o sinal para usar o ícone |
| 409 | `exists`, `is_dir`, `not_dir`, `not_empty`, `modified`, `slug_taken`, `drop_count_exceeded`, `drop_links_exceeded`, `conflict`, `cross_device`, `upload_in_progress`, `incomplete`, `last_admin`, `self`, `totp_setup_expired`, `totp_not_enabled`, `totp_already_enabled` | Conflito de estado |
| 408 | `timeout` | A transferência estancou e foi descartada (rede do cliente, ou proxy) |
| 411 | `length_required` | Chunk sem `Content-Length` |
| 413 | `too_large`, `upload_reserve_exceeded`, `drop_file_limit`, `archive_too_large` | Corpo maior que o limite / uploads inacabados do usuário já reservam o teto de espaço / arquivo acima do teto por arquivo do link de envio |
| 507 | `no_space`, `quota_exceeded`, `drop_full` | Disco cheio / cota do usuário estourada / link de envio sem espaço (a cota do link ou a do dono: um visitante anônimo não distingue as duas) |
| 429 | `rate_limited`, `busy` | Limite de taxa ou concorrência (`Retry-After: 1`) |
| 499 | `cancelled` | Cliente desistiu |
| 503 | `db`, `root` | Health |
| 507 | `no_space` | Disco cheio |
| 500 | `internal` | Erro inesperado (detalhes só no log) |

**Nomes equivalentes**: para todo 409 `exists`, um nome que só difere na caixa ou na forma Unicode de um existente conta como existente (ver [03](03-seguranca.md#sandbox-de-caminhos-internalvfs)). Quando é isso, o erro traz `existing` com o nome que está no disco (`{"error": {"code": "exists", "existing": "C8347.mp4"}}`), e no lote o resultado do arquivo traz o mesmo campo.
