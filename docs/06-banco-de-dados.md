# 06 · Banco de dados

SQLite em `FILEZAM_DATA_DIR/filezam.db` (mais `-wal` e `-shm`). Driver `modernc.org/sqlite`, sem CGO.

## Abertura (`internal/store/db.go`)

- DSN: `file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_txlock=immediate`.
- Dois pools: escrita (`MaxOpenConns(1)`) e leitura (`MaxOpenConns(8)`). Métodos de repositório usam `db.w` para `INSERT/UPDATE/DELETE` e `db.r` para `SELECT`.
- `db.Now` é injetável (testes de expiração).

## Migrações

Arquivos em `internal/store/migrations/NNN_nome.sql`, embutidos com `embed`, aplicados em ordem numérica dentro de uma transação cada, registrados em `schema_migrations(version, applied_at)`. Para evoluir o esquema: crie `002_algo.sql` (nunca edite um arquivo já aplicado em produção). SQLite tem `ALTER TABLE` limitado; para mudanças estruturais use o padrão criar-nova → copiar → renomear.

## Esquema (`001_init.sql` + migrações)

```sql
users(id, username UNIQUE NOCASE, password_hash /*PHC argon2id*/, role CHECK IN ('admin','user'),
      scope TEXT DEFAULT '' /*base-relativo; '' = raiz*/, must_change_password, disabled,
      failed_logins, lockouts, locked_until, created_at, updated_at)

sessions(id /*hex sha256 do token*/, user_id → users ON DELETE CASCADE, created_at, expires_at,
         last_seen_at, ip, user_agent)            -- índices: user_id, expires_at

favorites(id, user_id → users CASCADE, path /*base-relativo*/, name, created_at, UNIQUE(user_id,path))

shares(id, token_hash UNIQUE, token /*em claro, 002; '' nos links anteriores*/,
       path /*base-relativo*/, name, created_by → users CASCADE,
       created_at, expires_at, revoked_at, access_count, last_access_at)   -- índice: expires_at

uploads(id /*hex 16 bytes*/, user_id → users CASCADE, dir /*base-relativo*/, name, size, mtime,
        chunk_size, received BLOB /*bitset ceil(chunks/8)*/, overwrite, created_at, updated_at)
        -- UNIQUE(dir,name); índice updated_at

audit_log(id, ts, user_id, username, ip, action, detail /*JSON*/)   -- índice: ts

schema_migrations(version, applied_at)
```

Migrações aplicadas depois de `001_init.sql`:

| Versão | Arquivo | O que faz |
|---|---|---|
| 002 | `002_share_token.sql` | `shares.token` (texto, default `''`): guarda o token do link público para poder copiá-lo de novo |

Todos os timestamps são segundos Unix, exceto `uploads.mtime` (ms, vindo do cliente).

## Convenções

- Caminhos gravados são **base-relativos** (`vfs.Join(user.Scope, p)`), para sobreviverem a mudanças de escopo do usuário. A leitura converte para escopo-relativo e descarta o que ficou fora.
- Nunca grave senhas ou tokens de sessão em claro (`auth.HashToken`). A única exceção é `shares.token`, gravado em claro de propósito para permitir recopiar o link ([03](03-seguranca.md#links-públicos)); a consulta pública continua sendo por `token_hash`.
- `RecordLoginFailure` aplica o bloqueio progressivo; `RecordLoginSuccess` zera.
- `ListStaleUploads`, `PurgeExpiredSessions` e `PruneAudit` são chamados pela tarefa de fundo.

## Backup

Com o container rodando, o modo WAL permite copiar os três arquivos juntos, mas o mais seguro é `docker compose stop filezam`, copiar `filezam.db*` e subir de novo. O banco é pequeno (KB a poucos MB). Restaurar = substituir os arquivos com o serviço parado.

## Reset do admin

`filezam reset-admin [senha]` (usa `FILEZAM_ADMIN_PASSWORD` se não houver argumento): recria ou redefine `FILEZAM_ADMIN_USER` como admin ativo com troca obrigatória, apaga as sessões dele e zera bloqueios. Com Docker: `docker compose run --rm filezam reset-admin 'NovaSenha123'`.
