# Contribuindo com o Filezam

Obrigado pelo interesse. Issues e pull requests são bem-vindos, em português ou inglês. O projeto é mantido no tempo livre, então a resposta pode levar alguns dias.

## Por onde começar

1. Leia o [README](README.md) para entender o que o projeto faz e o que ele **não** pretende fazer.
2. Leia [`docs/01-visao-geral.md`](docs/01-visao-geral.md) (decisões de arquitetura), [`docs/02-arquitetura.md`](docs/02-arquitetura.md) (pacotes e fluxo de uma requisição) e [`docs/03-seguranca.md`](docs/03-seguranca.md) (modelo de ameaças). A pasta `docs/` é a fonte de verdade: cada documento descreve como a área funciona e **é atualizado no mesmo commit** que muda o comportamento.
3. Para funcionalidades novas, abra uma issue antes de codar, descrevendo o problema que ela resolve. Correções de bug e melhorias de documentação podem vir direto como PR.
4. Confira [`docs/10-roadmap.md`](docs/10-roadmap.md): ali estão as limitações conhecidas, os próximos passos sugeridos e as ideias já descartadas (com o motivo).

## Ambiente de desenvolvimento

Requisitos: Go (a versão em `go.mod`; o `go` baixa a toolchain sozinho), Node 22+ e, opcionalmente, Docker.

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
cd web && npm ci && cd ..
make run          # API em http://127.0.0.1:8080 servindo ./data, banco em ./config, cookies sem Secure
make dev          # em outro terminal: Vite em http://localhost:5173 com hot reload e proxy de /api
make test         # go test ./... + tsc --noEmit + vitest; precisa passar antes de abrir o PR
```

Se a porta 8080 estiver ocupada: `FILEZAM_LISTEN=127.0.0.1:8765 make run` (e ajuste o `proxy` em `web/vite.config.ts`). Sem o Go instalado, a suíte Go roda em Docker:

```bash
docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.26-alpine go test ./...
```

Login inicial em desenvolvimento: `admin` / `admin` (troca obrigatória). Chamadas à API por `curl` precisam do header `X-Filezam: 1`.

## Regras que não se negociam

Elas existem porque o Filezam expõe um disco na internet; um PR que as viole não será aceito, por melhor que seja o resto.

- **Todo acesso ao disco passa por `internal/vfs` e `*os.Root`.** Nunca `os.Open(filepath.Join(root, p))`. Operação nova de arquivo = método em `vfs.Root`, mapeamento em `vfs.MapError` e caso em `root_test.go` com o arquivo-canário fora da raiz.
- **Caminhos vêm em `?path=` ou no JSON, nunca em segmentos da URL**, e passam por `vfs.Normalize` (leitura) ou `vfs.NormalizeWritable` (escrita).
- **Nunca servir `text/html` a partir de conteúdo enviado por usuários.** O allow-list de tipos inline (`detectType`) e a CSP `sandbox` das respostas inline ficam como estão.
- **Códigos de erro são estáveis**: um código novo entra em `internal/server/respond.go`, em `web/src/i18n/pt-BR.ts` **e** `web/src/i18n/en.ts` (`errorCodes`) e em `docs/04-api.md`.
- **Mudança de esquema é uma migração nova** em `internal/store/migrations/NNN_*.sql`; nunca edite uma já aplicada.
- **Todo endpoint que altera estado tem teste** em `internal/server/server_test.go`.
- **Interface bilíngue**: todo texto novo existe em `pt-BR.ts` (referência) e em `en.ts`; o `tsc` falha se faltar uma chave.

## Fluxo de um PR

1. Crie um branch a partir de `main`.
2. Faça a mudança com o documento de `docs/` correspondente atualizado. Mudanças visíveis ao usuário também entram no README.
3. `make test` verde. A CI do GitHub roda o mesmo conjunto mais `govulncheck`, `npm audit` e o build da imagem Docker.
4. Abra o PR descrevendo **o porquê** da mudança, não só o quê; o template traz o checklist.
5. Commits podem ser em português ou inglês. Um commit por assunto facilita a revisão.

## Onde as coisas ficam

| Área | Caminho |
|---|---|
| Rotas e cadeias de middleware (`authed`/`user`/`admin`) | `internal/server/server.go` |
| Handlers HTTP | `internal/server/handlers_*.go` |
| Sandbox de caminhos, cópia, zip, lixeira | `internal/vfs/` |
| Protocolo de upload (servidor / cliente) | `internal/uploads/service.go` / `web/src/upload/manager.ts` |
| Ações e teclado da listagem | `web/src/pages/Browser.tsx` |
| Diálogos imperativos | `web/src/components/dialogs.tsx` |
| Textos da interface | `web/src/i18n/pt-BR.ts`, `web/src/i18n/en.ts` |
| Estilos compostos (Tailwind 4 `@utility`) | `web/src/index.css` |

Um passo a passo para adicionar uma funcionalidade de ponta a ponta está em [`docs/07-frontend.md`](docs/07-frontend.md#como-adicionar-uma-funcionalidade).

## Reportar problemas de segurança

Veja [`SECURITY.md`](SECURITY.md). **Não abra issue pública** para vulnerabilidades.

## Conduta

Seja respeitoso e objetivo; veja [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
