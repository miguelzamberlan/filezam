# Contribuindo

Obrigado pelo interesse. Issues e pull requests são bem-vindos; o projeto é mantido no tempo livre, então a resposta pode demorar alguns dias.

## Antes de codar

1. Leia [`docs/01-visao-geral.md`](docs/01-visao-geral.md), [`docs/02-arquitetura.md`](docs/02-arquitetura.md) e [`docs/03-seguranca.md`](docs/03-seguranca.md). A pasta `docs/` é a fonte de verdade: cada documento descreve como a área funciona e **deve ser atualizado no mesmo commit** que muda o comportamento.
2. Para funcionalidades novas, abra uma issue primeiro descrevendo o problema que ela resolve. Correções de bug podem vir direto como PR.

## Regras que não se negociam

- Todo acesso ao disco passa por `internal/vfs` e `*os.Root`. Nunca `os.Open(filepath.Join(root, p))`.
- Caminhos vêm em `?path=` ou no JSON, nunca em segmentos da URL, e passam por `vfs.Normalize`/`vfs.NormalizeWritable`.
- Nunca servir `text/html` a partir de conteúdo enviado por usuários.
- Códigos de erro novos entram em `respond.go`, em `web/src/strings.ts` e em `docs/04-api.md`.
- Mudança de esquema é uma migração nova em `internal/store/migrations/`; nunca edite uma aplicada.
- Todo endpoint que altera estado precisa de teste em `internal/server/server_test.go`.

## Fluxo

```bash
make test          # go test ./... + tsc + vitest — precisa passar
make run           # API local; make dev para o Vite com hot reload
```

Textos da interface ficam em `web/src/strings.ts` (pt-BR). Commits e PRs podem ser em português ou inglês; descreva o *porquê* da mudança, não só o quê.

## Reportar problemas de segurança

Veja [`SECURITY.md`](SECURITY.md). Não abra issue pública para vulnerabilidades.
