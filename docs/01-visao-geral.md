# 01 · Visão geral e decisões

## Objetivo

Filezam é um gestor de arquivos web auto-hospedado. Ele expõe **uma pasta do servidor** (`FILEZAM_ROOT`) pelo navegador, com login por usuário e senha, para manipular arquivos com a velocidade de um gerenciador local e sem abrir brechas para fora dessa pasta.

Cenário de uso original: um HD externo montado num servidor Linux, compartilhado localmente via Samba e exposto na internet por Cloudflare Tunnel, acessado por administradores (raiz inteira) e clientes (apenas a subpasta deles).

## Requisitos

Funcionais:

- Login com usuário e senha; vários usuários; perfis `admin` e `user`.
- Cada usuário acessa a raiz inteira **ou** uma subpasta específica (escopo). Não existem pastas *home* automáticas.
- Primeiro início cria um admin padrão com troca de senha obrigatória.
- Navegar, criar pasta, renomear, copiar, recortar/colar (mover), excluir, baixar arquivo ou ZIP, favoritos, visualizar (imagem, vídeo, áudio, PDF, texto).
- Compartilhar uma **pasta ou um arquivo** por link público somente leitura com prazo de validade, senha opcional e revogação.
- Lixeira com retenção configurável e restauração ao local original.
- Pesquisa por nome em todas as subpastas do escopo, servida por um índice em SQLite atualizado por varreduras periódicas e pelas próprias operações do app.
- Interface em pt-BR e inglês.
- Upload de arquivos grandes, pastas inteiras e milhares de arquivos pequenos, sem travar a interface, com retomada.

Não funcionais (prioridades declaradas pelo dono do projeto):

1. **Segurança**: impossível ler ou escrever fora da raiz; nenhum conteúdo enviado pode executar código no navegador de outro usuário; credenciais e tokens nunca em texto puro no banco.
2. **Velocidade**: I/O em streaming sem buffers em memória, poucas requisições para muitos arquivos, operações longas em segundo plano com progresso.
3. Um único container, configurado por variáveis de ambiente, atrás de um proxy reverso que faz TLS.

## Stack

| Camada | Escolha | Versão usada |
|---|---|---|
| Backend | Go, `net/http` puro, `log/slog` | Go 1.26 |
| Sandbox de disco | `os.Root` (kernel `openat` + `O_NOFOLLOW` em cada componente) | stdlib |
| Banco | SQLite via `modernc.org/sqlite` (puro Go, sem CGO) | 1.58 |
| Senhas | Argon2id (`golang.org/x/crypto`) | — |
| Frontend | React 19, TypeScript, Vite 7, Tailwind CSS 4, react-router 7, TanStack Query 5, TanStack Virtual 3, zustand 5, react-markdown 10 + remark-gfm 4 (preview de Markdown, carregado sob demanda) | — |
| Empacotamento | Binário estático com UI embutida (`embed`); imagem `distroless/static` | — |

## Registro de decisões (ADR)

### ADR-1 · Go com `os.Root` para todo acesso a disco

Alternativas: validar caminhos por string e usar `os.*`; Node/Rust. `os.Root` transfere a garantia para o kernel: cada componente do caminho é aberto com `openat` sem seguir symlinks que escapem, o que elimina TOCTOU e o risco de esquecer uma validação. Consequência: nunca use `filepath.Join(root, p)` + `os.Open`; toda operação vive em `internal/vfs`.

### ADR-2 · Escopo por usuário como `Root` aninhado

O escopo é aberto com `base.OpenRoot(scope)` a cada requisição, e não com `os.OpenRoot(filepath.Join(base, scope))`. Assim um escopo cujo caminho passe por um symlink para fora da raiz é recusado. Custo: alguns `openat` por requisição, desprezível.

### ADR-3 · Caminhos na query string ou no JSON, nunca no path da URL

Proxies e o próprio `net/http` normalizam `%2F`, `..` e barras duplas no path da URL. Em `?path=` e no corpo JSON o valor chega intacto e passa por `vfs.Normalize`.

### ADR-4 · SQLite com dois pools

Um pool de escrita com uma conexão (`MaxOpenConns(1)`, `_txlock=immediate`) e um pool de leitura com 8. Elimina `SQLITE_BUSY` em escrita concorrente e mantém leituras paralelas. WAL, `synchronous=NORMAL`, `busy_timeout=5000`.

### ADR-5 · Três modos de upload escolhidos pelo cliente

Lote multipart para arquivos ≤ 1 MiB, `PUT` único até o tamanho do chunk e sessões chunked acima disso. O lote existe porque 10 mil arquivos pequenos a uma requisição por arquivo custam dezenas de segundos só de latência atrás de TLS + proxy. Detalhes em [05](05-uploads.md).

### ADR-6 · Chunks de tamanho fixo com bitset, não intervalos arbitrários

Simplifica o servidor (índice → offset), permite blocos paralelos e fora de ordem, e a retomada é "quais índices faltam". O tamanho vem de `FILEZAM_MAX_UPLOAD_CHUNK` e fica gravado por sessão, então mudar a configuração não quebra sessões em andamento.

### ADR-7 · Finalização atômica com `Link` + `Remove`

`os.Root` não expõe o descritor para `renameat2(RENAME_NOREPLACE)`. Para "não sobrescrever" usa-se `Link(part, final)` (falha com `EEXIST` se o destino surgiu) e depois `Remove(part)`. Em sistemas de arquivos sem hardlink cai-se para `Lstat` + `Rename` (janela de corrida mínima, aceita).

### ADR-8 · Jobs em memória com polling

Copiar/mover/excluir rodam em goroutines; o handler espera até 300 ms ("sync-or-job") e devolve o snapshot. O cliente consulta a cada 500 ms enquanto `running`. SSE foi descartado por exigir configuração de proxy (`proxy_buffering off`) e manter conexões abertas. Reiniciar o processo perde apenas a exibição de progresso.

### ADR-9 · Links públicos por caminho, token com hash

O share guarda o caminho base-relativo e o SHA-256 do token (256 bits). Excluir, mover ou renomear o item pelo app revoga o link (e os links de tudo que estiver dentro dele); para mudanças feitas por fora, o `dev`/`inode` gravado na criação faz o link parar de responder. Seguir o item para o novo caminho foi descartado: o link publicaria um lugar que quem o criou não escolheu. A resposta para token inexistente, expirado ou revogado é sempre o mesmo 404.

### ADR-10 · Container distroless sem root e sem `chown` no entrypoint

Não há shell na imagem; o healthcheck é o subcomando `healthcheck` do próprio binário. O app roda como `PUID:PGID` e testa a gravação em `/data` e `/config` na inicialização. Como o Docker cria bind mounts inexistentes como `root`, o compose traz um serviço `init` (Alpine, executa uma vez) que ajusta `/config` e, só se vazia, `/data`.

### ADR-11 · Banco fora da raiz de dados

`FILEZAM_DATA_DIR` não pode ficar dentro de `FILEZAM_ROOT` (verificado no `config.Load`). Do contrário o próprio gestor exibiria e permitiria baixar `filezam.db`, que contém hashes de senhas, sessões e tokens.

### ADR-12 · Interface em pt-BR num único arquivo de strings

Todos os textos ficam em `web/src/i18n/pt-BR.ts` (referência) e `web/src/i18n/en.ts`, incluindo a tradução dos códigos de erro da API; `strings.ts` expõe o objeto `S` do idioma ativo (preferência `auto`/`pt-BR`/`en`). Um idioma novo é um arquivo em `i18n/` com as mesmas chaves, sem tocar nos componentes.
