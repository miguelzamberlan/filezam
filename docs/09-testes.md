# 09 · Testes

## Automatizados

| Suíte | Onde | Cobre | Comando |
|---|---|---|---|
| Normalização de caminhos | `internal/vfs/pathutil_test.go` | ~30 entradas maliciosas/válidas, `ValidName`, helpers | `go test ./internal/vfs/` |
| Sandbox | `internal/vfs/root_test.go` | symlinks para `/etc`, `../outside`, loop e irmão interno; canário fora do root; escopo aninhado; rename/move/copy/remove; cancelamento; partes e `Finalize`; `TestFindStaysInsideRoot` (pesquisa não segue symlinks, não vê o canário, pula partes, respeita limites) | idem |
| Integração HTTP | `internal/server/server_test.go` | fluxo completo: admin inicial → troca forçada → CSRF → usuário com escopo → traversal → mkdir/PUT/lote/chunked (fora de ordem, idempotente, incompleto) → rename/copy/move/delete jobs → zip → favoritos → shares públicos e revogação → mudança de escopo derruba sessão → desativar/excluir usuário → auditoria; expiração de share com relógio falso; rate limit de login; cabeçalhos da SPA; `TestSecurityHardening` (cabeçalhos do conteúdo inline, nomes com caracteres de controle, leitura de partes de upload recusada, links de usuário desativado/escopo estreitado, rate limit da troca de senha); `TestLogPath`; `TestSearch` (escopo, symlink, limite, erros) | `go test ./internal/server/` |
| Migrações | `internal/store/migrations_test.go` | banco só com `001` + linhas antigas → `Open` aplica `002` sem perder dados | `go test ./internal/store/` |
| Frontend puro | `web/src/**/*.test.ts` | `scheduler` (classify, pickBatch, backoff, chunkRange), `paths`, `naturalSort` | `cd web && npx vitest run` |
| Tipos | — | `tsc --noEmit` | `cd web && npm run typecheck` |

`make test` roda tudo. Um único teste Go: `go test ./internal/vfs/ -run TestEscapeAttemptsAreRefused -v`.

Os testes de integração criam raiz e banco temporários; nada toca o sistema real. `db.Now` é injetável para testar expirações.

## O que não está coberto automaticamente

- Interação real do navegador (drag-and-drop, teclado, virtualização). Verificado manualmente com Chrome headless via DevTools Protocol (scripts em `scratchpad` na sessão de desenvolvimento; não versionados).
- Comportamento atrás de proxies reais e no Cloudflare.
- Sistemas de arquivos específicos (foi validado em ext4 e ntfs3).

## Checklist manual antes de uma versão

1. `docker compose up` limpo → `admin/admin` → troca obrigatória → senha antiga recusada.
2. Upload de arquivo de vários GB: memória do container estável (`docker stats`), recarregar no meio e retomar soltando o mesmo arquivo, `sha256sum` igual.
3. Pasta com 10 mil arquivos pequenos: UI responsiva, contagem correta.
4. Copiar/mover/excluir árvore grande com progresso e cancelamento.
5. ZIP de pasta grande abre com `unzip`.
6. Link de 1 hora em janela anônima; revogar → 404.
7. `curl '.../api/files?path=../../etc/passwd'` → 400; symlink em `/data` para `/config` não abre.
8. `evil.svg` com `<script>` e `evil.html`: sem alerta, HTML só como texto/download.
9. Usuário com escopo não vê o pai; admin muda o escopo → sessão cai.
10. Atrás do proxy real: cookie `Secure`, IP real na auditoria, upload de 200 MB completa.
11. Preview de PDF abre dentro da interface (Chrome e Firefox).
12. `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` sem vulnerabilidades alcançáveis e `cd web && npm audit --omit=dev` limpo; se o Go tiver correção nova, subir `go.mod` e `Dockerfile`.
