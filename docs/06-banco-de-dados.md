# 06 · Banco de dados

SQLite em `FILEZAM_DATA_DIR/filezam.db` (mais `-wal` e `-shm`). Driver `modernc.org/sqlite`, sem CGO.

## Abertura (`internal/store/db.go`)

- DSN: `file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=case_sensitive_like(ON)&_txlock=immediate`. `case_sensitive_like` porque os `LIKE` de prefixo de caminho no índice (`path LIKE 'Docs/%'`) não podem casar `docs/…`: senão estreitar o escopo de um usuário para `Docs` somaria na cota dele os bytes de `docs`, e reindexar uma pasta apagaria linhas da outra. A pesquisa por nome continua sem diferenciar maiúsculas porque usa a coluna `name_lc` com o termo já em minúsculas.
- Dois pools: escrita (`MaxOpenConns(1)`) e leitura (`MaxOpenConns(8)`). Métodos de repositório usam `db.w` para `INSERT/UPDATE/DELETE` e `db.r` para `SELECT`.
- `db.Now` é injetável (testes de expiração).

## Migrações

Arquivos em `internal/store/migrations/NNN_nome.sql`, embutidos com `embed`, aplicados em ordem numérica dentro de uma transação cada, registrados em `schema_migrations(version, applied_at)`. Para evoluir o esquema: crie `010_algo.sql` (nunca edite um arquivo já aplicado em produção). SQLite tem `ALTER TABLE` limitado; para mudanças estruturais use o padrão criar-nova → copiar → renomear.

## Esquema (`001_init.sql` + migrações)

```sql
users(id, username UNIQUE NOCASE, password_hash /*PHC argon2id*/, role CHECK IN ('admin','user'), quota /*bytes, 007*/, totp_secret /*AES-GCM, 009*/, totp_enabled_at, totp_counter, totp_recovery /*JSON, 009*/,
      scope TEXT DEFAULT '' /*base-relativo; '' = raiz*/, must_change_password, disabled,
      failed_logins, lockouts, locked_until, created_at, updated_at)

sessions(id /*hex sha256 do token*/, user_id → users ON DELETE CASCADE, created_at, expires_at,
         last_seen_at, ip, user_agent)            -- índices: user_id, expires_at

favorites(id, user_id → users CASCADE, path /*base-relativo*/, name, created_at, UNIQUE(user_id,path))

shares(id, token_hash UNIQUE, token /*em claro, 002; '' nos links anteriores*/, kind /*'dir'|'file', 003*/, password_hash /*argon2id ou '', 003*/, dev, ino /*identidade do item, 006*/,
       path /*base-relativo*/, name, created_by → users CASCADE,
       created_at, expires_at, revoked_at, access_count, last_access_at)   -- índice: expires_at

uploads(id /*hex 16 bytes*/, user_id → users CASCADE, dir /*base-relativo*/, name, size, mtime,
        chunk_size, received BLOB /*bitset ceil(chunks/8)*/, overwrite, created_at, updated_at)
        -- UNIQUE(dir,name); índice updated_at

audit_log(id, ts, user_id, username, ip, action, detail /*JSON*/)   -- índice: ts
trash(id /*hex 16 bytes*/, user_id → users CASCADE, trash_dir /*base-relativo: <escopo>/.filezam-trash*/, name, path /*original, base-relativo*/,
      type, size, deleted_at)   -- índices: deleted_at, path   (004)
file_index(path PK /*base-relativo*/, parent, name, name_lc, type, size, mtime, gen)   -- índices: name_lc, parent   (005)
index_state(id=1, last_full_at, entries, gen)   (005)
jobs(id, user_id → users CASCADE, type, label, state, done, total, bytes_done, bytes_total, error, warnings, started_at, finished_at)   -- índice: (user_id, started_at)   (008)
schema_migrations(version, applied_at)
```

Migrações aplicadas depois de `001_init.sql`:

| Versão | Arquivo | O que faz |
|---|---|---|
| 002 | `002_share_token.sql` | `shares.token` (texto, default `''`): guarda o token do link público para poder copiá-lo de novo |
| 003 | `003_share_file_password.sql` | `shares.kind` (`dir`/`file`) e `shares.password_hash` (Argon2id, `''` = sem senha) |
| 004 | `004_trash.sql` | Tabela `trash(id, user_id → users CASCADE, trash_dir, name, path, type, size, deleted_at)`: itens da lixeira com o caminho original base-relativo |
| 006 | `006_share_inode.sql` | `shares.dev`, `shares.ino`: identidade do item compartilhado (0 = desconhecida) |
| 007 | `007_user_quota.sql` | `users.quota` (bytes, 0 = sem limite) |
| 009 | `009_totp.sql` | `users.totp_secret` (cifrado), `totp_enabled_at`, `totp_counter`, `totp_recovery` (JSON de hashes) |
| 008 | `008_jobs.sql` | Tabela `jobs(id, user_id → users CASCADE, type, label, state, done, total, bytes_done, bytes_total, error, warnings, started_at, finished_at)`: histórico de operações |
| 005 | `005_file_index.sql` | `file_index(path PK, parent, name, name_lc, type, size, mtime, gen)` com índices em `name_lc` e `parent`, e `index_state(id=1, last_full_at, entries, gen)` |

Todos os timestamps são segundos Unix, exceto `uploads.mtime` (ms, vindo do cliente).

## Convenções

- Caminhos gravados são **base-relativos** (`vfs.Join(user.Scope, p)`), para sobreviverem a mudanças de escopo do usuário. A leitura converte para escopo-relativo e descarta o que ficou fora.
- Nunca grave senhas ou tokens de sessão em claro (`auth.HashToken`). Segredos TOTP vão cifrados com `auth.Seal` e a chave de `<DataDir>/secret.key` (ou `FILEZAM_SECRET_KEY`): **o backup precisa levar o `secret.key` junto com o banco**, senão o 2FA de todos deixa de validar. A única exceção é `shares.token`, gravado em claro de propósito para permitir recopiar o link ([03](03-seguranca.md#links-públicos)); a consulta pública continua sendo por `token_hash`.
- `RecordLoginFailure` aplica o bloqueio progressivo; `RecordLoginSuccess` zera.
- `SetTOTPCounter` (`WHERE totp_counter < ?`) e `ConsumeTOTPRecovery` (`WHERE totp_recovery = ?`) devolvem se a linha mudou: são o anti-replay do 2FA, por isso o resultado nunca é ignorado.
- `ListStaleUploads`, `PurgeExpiredSessions` e `PruneAudit` são chamados pela tarefa de fundo.

## Backup

Com o container rodando, o modo WAL permite copiar os três arquivos juntos, mas o mais seguro é `docker compose stop filezam`, copiar `filezam.db*` e subir de novo. O banco é pequeno (KB a poucos MB). Restaurar = substituir os arquivos com o serviço parado.

## Reset do admin

`filezam reset-admin [senha]` (usa `FILEZAM_ADMIN_PASSWORD` se não houver argumento): recria ou redefine `FILEZAM_ADMIN_USER` como admin ativo com troca obrigatória, apaga as sessões dele e zera bloqueios. Com Docker: `docker compose run --rm filezam reset-admin 'NovaSenha123'`.
