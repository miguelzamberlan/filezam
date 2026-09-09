# 02 · Arquitetura

## Componentes

```
navegador ──HTTPS──▶ proxy reverso ──HTTP──▶ filezam (binário único)
                                              ├── net/http + SPA embutida
                                              ├── internal/vfs (os.Root) ──▶ FILEZAM_ROOT
                                              └── SQLite ──────────────────▶ FILEZAM_DATA_DIR/filezam.db
```

Um único processo: servidor HTTP, workers de jobs e tarefas de manutenção. Sem dependências externas em tempo de execução.

## Pacotes Go

| Pacote | Responsabilidade | Depende de |
|---|---|---|
| `cmd/filezam` | Subcomandos `serve`, `healthcheck`, `reset-admin`, `version`; sinal de parada, shutdown gracioso | config, server, store, vfs |
| `internal/config` | Parse e validação das variáveis de ambiente (`Config`) | — |
| `internal/server` | Roteamento, middlewares, handlers, sessões, SPA, auditoria | todos abaixo |
| `internal/vfs` | **Núcleo de segurança**: normalização de caminhos, `Root`, listagem, pesquisa (`Find`), cópia, movimento, remoção, zip, arquivos `.part` | `os.Root`, `x/sys/unix` |
| `internal/uploads` | Sessões chunked: bitset, escrita por offset, finalização, limpeza | store, vfs |
| `internal/jobs` | Registro em memória de jobs com progresso e cancelamento | — |
| `internal/auth` | Argon2id, tokens, hash de tokens, rate limiters e semáforos | `x/crypto` |
| `internal/store` | Acesso ao SQLite, migrações embutidas, repositórios por tabela | `modernc.org/sqlite` |
| `internal/server/webdist` | `//go:embed all:dist` com o build do Vite | — |

Regra de dependência: `vfs`, `auth`, `jobs` e `store` não conhecem HTTP. `uploads` conhece `store` e `vfs`. Só `server` conhece todos.

## Fluxo de uma requisição autenticada

1. `Handler()` monta a cadeia global: `recoverer` → `realIP` → `securityHeaders` → `logging` → `ServeMux`.
2. A rota escolhe a cadeia por tipo (`routes()` em `server.go`):
   - `authed`: `requireUser` (cookie → sessão → usuário; verifica CSRF em métodos mutantes).
   - `user`: `authed` + `requirePasswordOK` (bloqueia quem precisa trocar a senha).
   - `admin`: `user` + `requireAdmin`.
   - Públicas (`/api/public/*`, `/api/health`, login) não passam por `requireUser`.
3. O handler (`handlerFunc` que devolve `error`) abre o root do escopo com `s.userRoot(r)`, executa e fecha (`defer root.Close()`).
4. Erros voltam para `s.h()`, que chama `toAPIError` e responde `{error:{code,message}}` com status HTTP. Erros ≥ 500 vão para o log.

Objetos no contexto da requisição: usuário (`ctxUser`), sessão (`ctxSession`) e IP real (`ctxIP`).

## Caminhos: três referenciais

| Referencial | Onde aparece | Exemplo (escopo `Clientes/Empresa1`) |
|---|---|---|
| Absoluto no host | só dentro de `vfs.Open` | `/data/Clientes/Empresa1/Docs` |
| Base-relativo | banco (favoritos, shares, uploads), root base do admin | `Clientes/Empresa1/Docs` |
| Escopo-relativo | API, URL do navegador, UI | `Docs` |

Conversões: `vfs.Join(user.Scope, p)` para gravar; `scopeRel`/`relDir` para ler. Linhas fora do escopo atual são ocultadas, não convertidas.

## Ciclo de vida do processo (`serve`)

1. `config.Load()`; erro fatal se raiz for `/`, se `DataDir` estiver dentro da raiz ou se admin/senha iniciais estiverem vazios.
2. Cria `DataDir` e `Root` se não existirem; testa gravação nos dois (`CheckWritable`), falhando com sugestão de `chown`.
3. Abre o SQLite e aplica migrações pendentes (`store.Open`).
4. `vfs.Open(root)` mantém o `os.Root` base aberto pelo tempo de vida do processo.
5. `server.New` cria limitadores, o serviço de uploads, o gerenciador de jobs e o admin inicial se a tabela `users` estiver vazia.
6. `StartBackground()` executa imediatamente e depois a cada hora: limpeza de sessões de upload paradas há mais de 24 h, sessões de login expiradas e auditoria com mais de 180 dias.
7. `http.Server` com `ReadHeaderTimeout 10s`, `IdleTimeout 120s`, `MaxHeaderBytes 64 KiB` e **sem** `ReadTimeout`/`WriteTimeout` globais (matariam transferências grandes); prazos por handler via `ResponseController` (aborta após 60 s sem bytes).
8. `SIGINT`/`SIGTERM`: `Shutdown` do HTTP com 30 s, espera jobs até 30 s, cancela o contexto de fundo, fecha banco e root.

## Layout do repositório

```
cmd/filezam/main.go
internal/{config,server,vfs,uploads,jobs,auth,store}
internal/server/webdist/dist        # saída do Vite (gitignored, exceto .keep)
internal/store/migrations/NNN_*.sql # embutidas, aplicadas em ordem numérica
web/                                 # projeto Vite (ver docs/07)
docs/                                # esta documentação
Dockerfile docker-compose.yml .env.example Makefile README.md CLAUDE.md
```

## Concorrência e limites

| Recurso | Limite | Onde |
|---|---|---|
| Logins simultâneos (Argon2 usa 64 MiB cada) | 4 | `loginSem` |
| Tentativas de login por IP | 10/min, burst 10 | `loginIP` |
| Tentativas de login por usuário | 5/min, burst 5 | `loginUser` |
| Troca de senha (senha atual errada) | mesmos limites e semáforo do login | `handleChangePassword` |
| Uploads simultâneos por usuário (PUT, lote ou chunk) | 8 | `uploadSem` → 429 |
| Requisições públicas por IP | 120/min | `publicIP` |
| Downloads/zips públicos simultâneos por IP | 2 | `publicDL` |
| Corpo JSON | 1 MiB | `readJSON` |
| Corpo de chunk / PUT | `ChunkSize` | `MaxBytesReader` |
| Corpo de lote | `BatchMaxBytes` + 1 MiB | idem |
| Escrita no SQLite | 1 conexão | `store.DB.w` |
| Contagem em `/api/files/info` | 200 000 entradas ou 15 s | `infoScanLimit` |
| Tamanho de um upload chunked | 1 PiB (`uploads.MaxUploadSize`) e o espaço livre | `Service.Create` |
| Pesquisa recursiva | 2 simultâneas por usuário; 200 000 entradas, 500 resultados ou 10 s por requisição | `searchSem`, `handleSearch` |

Sem limite (aceito, ver [10](10-roadmap.md)): zips/downloads autenticados simultâneos, jobs por usuário, cota de disco.
