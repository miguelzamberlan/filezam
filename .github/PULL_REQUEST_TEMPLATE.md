## O que muda e por quê

<!-- Uma ou duas frases sobre o problema e a abordagem. Referencie a issue: "Fecha #12". -->

## Checklist

- [ ] `make test` passa (Go + `tsc` + vitest).
- [ ] Todo acesso a disco continua passando por `internal/vfs` / `*os.Root`; caminhos vêm em `?path=` ou JSON, nunca em segmentos de URL.
- [ ] Endpoint novo ou alterado que muda estado tem teste em `internal/server/server_test.go`.
- [ ] Código de erro novo está em `respond.go`, em `web/src/i18n/pt-BR.ts` **e** `en.ts` (`errorCodes`) e em `docs/04-api.md`.
- [ ] Texto novo da interface existe em `pt-BR.ts` e em `en.ts`.
- [ ] Mudança de esquema veio como migração nova em `internal/store/migrations/`.
- [ ] O documento de `docs/` da área afetada foi atualizado neste mesmo PR (e o README, se a mudança for visível ao usuário).
