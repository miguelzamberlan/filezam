# Filezam

> Gestor de arquivos web auto-hospedado, rápido e seguro: exponha uma pasta do seu servidor pelo navegador, com usuários, uploads grandes com retomada e links públicos temporários. Um único binário Go com a interface embutida, feito para rodar num container atrás do seu proxy reverso.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![SQLite](https://img.shields.io/badge/SQLite-sem%20CGO-003B57?logo=sqlite&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-distroless-2496ED?logo=docker&logoColor=white)
![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green)

<p align="center">
  <img src="docs/img/desktop-light.png" alt="Filezam no desktop, tema claro" width="49%">
  <img src="docs/img/desktop-dark.png" alt="Filezam no desktop, tema escuro" width="49%">
</p>

## Sumário

- [Sobre o projeto](#sobre-o-projeto)
- [Funcionalidades](#funcionalidades)
- [Início rápido](#início-rápido)
- [Configuração](#configuração)
- [Como usar](#como-usar)
- [Segurança](#segurança)
- [Documentação](#documentação)
- [Desenvolvimento](#desenvolvimento)
- [Contribuindo](#contribuindo)
- [Roadmap e limitações](#roadmap-e-limitações)
- [Autor](#autor)
- [Licença](#licença)

## Sobre o projeto

O Filezam nasceu de uma necessidade concreta: um HD externo ligado a um servidor Linux em casa, compartilhado na rede local por Samba, que precisava ficar acessível pela internet (via Cloudflare Tunnel) para a família e para clientes, cada um vendo só a sua pasta. As alternativas prontas eram pesadas demais, exigiam banco de dados externo, ou tratavam a segurança do sistema de arquivos como detalhe.

Três prioridades guiam cada decisão, nesta ordem:

1. **Segurança.** É impossível ler ou escrever fora da pasta exposta: todo acesso ao disco passa pelo `os.Root` do Go, que valida cada componente do caminho no kernel, inclusive symlinks. Nenhum arquivo enviado por um usuário consegue executar script no navegador de outro. Senhas e sessões nunca ficam em texto puro no banco.
2. **Velocidade.** I/O em streaming sem carregar arquivos na memória, poucas requisições para muitos arquivos pequenos, uploads em blocos paralelos e operações longas em segundo plano com progresso.
3. **Simplicidade de operação.** Um container, configurado por variáveis de ambiente, sem banco externo, sem shell na imagem, rodando sem root.

É um projeto pessoal, gratuito e de código aberto. Serve bem para servidores domésticos, pequenos escritórios e quem quer entregar arquivos a clientes sem depender de serviços de terceiros.

## Funcionalidades

- **Usuários e escopos**: login com usuário e senha, perfis administrador/usuário, cada conta com acesso à raiz inteira ou a uma subpasta que ela enxerga como se fosse a raiz.
- **Gerenciamento completo**: navegar, criar pastas, renomear, copiar, recortar/colar (mover), excluir, baixar arquivo ou ZIP, favoritos, propriedades (tamanho calculado, quantidade de itens, links ativos) e espaço livre em disco.
- **Pesquisa** por nome em todas as subpastas às quais o usuário tem acesso.
- **Visualização** de imagens, vídeo, áudio, PDF e texto sem sair da página.
- **Uploads sérios**: arquivos de vários GB em blocos paralelos com retomada, pastas inteiras por arrastar e soltar, milhares de arquivos pequenos em lote, tudo com painel de progresso, pausa e repetição de falhas.
- **Links públicos**: compartilhe uma pasta por link somente leitura com prazo de validade, contagem de acessos e revogação imediata.
- **Interface**: em português, tema claro/escuro/sistema, cores personalizáveis, zoom da listagem, ícones por tipo de arquivo, atalhos de teclado e uso confortável no celular.
- **Administração**: gestão de usuários, auditoria de logins e alterações, bloqueio progressivo contra força bruta.
- **Operação**: binário estático, imagem `distroless` sem shell, rootfs somente leitura, SQLite embutido (sem CGO), migrações automáticas, healthcheck.

<p align="center">
  <img src="docs/img/mobile.png" alt="Filezam no celular" width="30%">
</p>

## Início rápido

Requisitos: Docker com Compose v2 e uma pasta no host que você queira expor.

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
cp .env.example .env
# edite .env: PUID/PGID (id -u / id -g), FILEZAM_HOST_ROOT (pasta a expor), FILEZAM_TRUSTED_PROXIES
docker compose up -d --build
```

Acesse `http://127.0.0.1:8080`. Login inicial: `admin` / `admin` (ou o que estiver em `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD`). A troca de senha é obrigatória no primeiro acesso.

Para atualizar depois de um `git pull`, reconstrua a imagem: `docker compose up -d --build`. Só `up -d` reinicia o container antigo.

## Configuração

### Pastas e permissões

| Volume | Conteúdo |
|---|---|
| `${FILEZAM_HOST_ROOT}` → `/data` | Pasta raiz exibida pelo gestor. Para expor várias pastas do host, monte-as como subpastas: `- /mnt/midia:/data/midia`. |
| `${FILEZAM_HOST_CONFIG}` → `/config` | Banco SQLite (usuários, sessões, links, uploads em andamento, auditoria). Nunca é servido e não pode ficar dentro de `/data`. |

O container roda como `PUID:PGID`, e as duas pastas precisam ser graváveis por esse usuário. Como o Docker cria pastas de *bind mount* inexistentes como `root`, o compose traz um serviço `init` (Alpine, executa uma vez) que faz `chown` em `/config` e, só se `/data` estiver vazia, também nela. Ele nunca altera o dono de uma pasta de dados que já tenha conteúdo. Se preferir gerenciar as permissões você mesmo, remova o serviço `init` e o bloco `depends_on`; se a pasta não for gravável, o app encerra na inicialização com a mensagem `chown` sugerida.

Discos NTFS/exFAT montados com `uid=`/`gid=` funcionam normalmente: basta que o `uid` do mount seja o mesmo `PUID`. Nunca use `PUID=0`.

### Variáveis de ambiente

| Variável | Padrão | Descrição |
|---|---|---|
| `FILEZAM_ROOT` | `/data` (imagem) / `./data` | Pasta raiz dos arquivos |
| `FILEZAM_DATA_DIR` | `/config` (imagem) / `./config` | Banco de dados. Não pode ficar dentro de `FILEZAM_ROOT` |
| `FILEZAM_LISTEN` | `:8080` | Endereço de escuta |
| `FILEZAM_TRUSTED_PROXIES` | vazio | IPs/CIDRs do proxy reverso, separados por vírgula |
| `FILEZAM_SECURE_COOKIES` | `auto` | `true` / `false` / `auto` (detecta via `X-Forwarded-Proto` de proxy confiável) |
| `FILEZAM_PUBLIC_URL` | vazio | Base dos links públicos (`https://...`); sem ela a interface usa o endereço do navegador |
| `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD` | `admin` / `admin` | Admin criado no primeiro início (troca forçada) |
| `FILEZAM_SESSION_TTL` | `168h` | Validade deslizante da sessão (teto absoluto: 30 dias) |
| `FILEZAM_MAX_UPLOAD_CHUNK` | `16MiB` | Tamanho do bloco de upload (1 MiB–1 GiB) |
| `FILEZAM_SHARE_MAX_TTL` | `720h` | Validade máxima de um link público |
| `FILEZAM_FSYNC` | `true` | `fsync` antes de finalizar cada upload |
| `FILEZAM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

No `.env` do compose há ainda `PUID`/`PGID`, `FILEZAM_HOST_ROOT`, `FILEZAM_HOST_CONFIG`, `FILEZAM_BIND` e `FILEZAM_PORT` (porta local à qual o proxy se conecta; por padrão só `127.0.0.1`).

### Atrás do proxy reverso

O app escuta HTTP puro e espera TLS no proxy. Configure:

- `FILEZAM_TRUSTED_PROXIES`: IPs/CIDRs do proxy. Só deles os cabeçalhos `X-Forwarded-For`/`X-Forwarded-Proto`/`X-Forwarded-Host` são aceitos (usados para IP real na auditoria/rate limit, cookie `Secure` e links de compartilhamento).
- `FILEZAM_SECURE_COOKIES=true` quando o acesso público é HTTPS (recomendado). `false` só para uso local sem TLS.
- `FILEZAM_PUBLIC_URL=https://arquivos.seudominio.com` se os links gerados pela API precisarem de um domínio diferente do que o navegador usa.

Uploads são enviados em blocos de `FILEZAM_MAX_UPLOAD_CHUNK` (16 MiB por padrão), então nenhuma requisição é enorme, mas o proxy não pode limitar nem bufferizar o corpo:

**nginx**
```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

**Traefik v3**: aumente `entryPoints.websecure.transport.respondingTimeouts.readTimeout` (padrão 60 s) para algo como `10m`. Veja os `labels` comentados em `docker-compose.yml` e remova o bloco `ports` se o proxy alcança o container por uma rede Docker compartilhada.

**Caddy**: funciona sem ajustes. **Cloudflare (proxy laranja)**: limite de 100 MB por requisição, então mantenha o chunk em até 64 MiB.

Receitas completas, backup, atualização e diagnóstico de problemas comuns estão em [`docs/08-operacao.md`](docs/08-operacao.md).

## Como usar

### Usuários e escopos

Em **Administração → Usuários** o admin cria contas e define o perfil (administrador ou usuário) e a **pasta de acesso**: a raiz inteira ou uma subpasta específica. O usuário restrito enxerga a subpasta como se fosse a raiz e não consegue alcançar nada fora dela. Alterar escopo ou senha invalida as sessões do usuário; estreitar o escopo também apaga os links públicos que ele tinha fora da nova pasta. Não é possível remover ou rebaixar o último administrador.

Contas são bloqueadas por 15 minutos (dobrando a cada repetição) após 10 falhas seguidas de login; há também limite por IP e por usuário. Tudo fica registrado em **Auditoria**.

### Links públicos

Selecione uma pasta, clique em **Compartilhar** e escolha a validade: o link (`/s/<token>`) é copiado na hora e pode ser copiado de novo em **Compartilhamentos** ou nas propriedades da pasta. Quem tiver o link pode listar, visualizar e baixar (arquivo ou ZIP), nada mais. O token tem 256 bits; a consulta pública é pelo hash, mas o token fica guardado no banco para permitir recopiar (quem tiver o arquivo do banco usa os links ativos). Revogar ou expirar invalida o link na hora; desativar o usuário ou tirar a pasta do escopo dele também. Mover ou renomear a pasta invalida o link (uma pasta nova no mesmo caminho o reativa, então revogue antes de recriar).

### Uploads

| Tamanho | Método |
|---|---|
| ≤ 1 MiB | Vários arquivos por requisição (`multipart`), até 200 arquivos / 32 MiB por lote |
| ≤ chunk | Um `PUT` direto |
| > chunk | Sessão em blocos: blocos paralelos, fora de ordem, idempotentes; `complete` só depois de todos |

Arquivos são gravados em `.filezam-upload-*.part` no diretório de destino e renomeados atomicamente ao final, então nunca aparece arquivo pela metade. Sessões abandonadas são apagadas após 24 h. Se o navegador for fechado no meio de um upload grande, ao voltar aparece um aviso: solte o mesmo arquivo na mesma pasta e só os blocos que faltam são enviados.

### Interface

- **Arquivos** (menu lateral) é a navegação em si. O filtro no alto da lista só peneira a pasta atual; **Pesquisar** procura pelo nome em todas as subpastas (a lupa ao lado do filtro já parte da pasta aberta). O resultado leva à pasta com o item selecionado.
- **Uploads** (menu lateral) não é uma tela: o envio acontece arrastando arquivos/pastas para a listagem ou pelos botões **Enviar arquivos**/**Enviar pasta**, e o progresso aparece num painel flutuante no canto inferior direito, com pausa, cancelamento e repetição de falhas. O item do menu mostra quantos envios estão em andamento e expande esse painel.
- **Zoom**: botões −/+ ao lado do filtro (ou em Configurações) ampliam a listagem sem mexer no zoom do navegador.
- **Tema e cores** (engrenagem no rodapé do menu): claro, escuro ou igual ao sistema; cores de destaque, seleção e foco com combinações prontas ou seletor livre. Tudo fica no navegador, por usuário.
- **Celular**: menu vira gaveta (☰), a barra de ações encolhe para o essencial mais **⋯**, um toque abre, toque longo abre o menu e seleciona (depois cada toque marca/desmarca).

### Atalhos de teclado

`↑ ↓ Home End PgUp PgDn` navegar · `Shift`/`Ctrl` seleção múltipla · `Ctrl+A` tudo · `Enter` abrir · `Backspace`/`Alt+↑` subir · `F2` renomear · `Del` excluir · `Ctrl+C` `Ctrl+X` `Ctrl+V` copiar/recortar/colar · `Ctrl+Shift+N` nova pasta · `Esc` limpar · digitar letras pula para o nome.

## Segurança

Resumo do que o projeto garante (modelo de ameaças, controles e limitações aceitas em [`docs/03-seguranca.md`](docs/03-seguranca.md); como reportar uma falha em [`SECURITY.md`](SECURITY.md)):

- **Sandbox de caminhos**: todo acesso ao disco passa por `os.Root`; `..`, symlinks para fora e nomes internos (`.filezam-*`) são recusados na entrada, inclusive em leitura. Nomes novos não aceitam caracteres de controle.
- **Conteúdo enviado por usuários nunca vira página**: HTML/JS/SVG-como-texto saem como `text/plain`, o resto vai como download; previews inline levam CSP `sandbox` e `nosniff`.
- **Sessões**: cookie `HttpOnly` + `SameSite=Strict`, só o hash no banco, validade deslizante com teto de 30 dias; senhas em Argon2id; bloqueio progressivo e rate limit no login **e na troca de senha**.
- **CSRF**: header `X-Filezam: 1` obrigatório em toda requisição mutante, mais `Sec-Fetch-Site`/`Origin`.
- **Cabeçalhos**: CSP estrita na SPA, `X-Frame-Options`, `Referrer-Policy: same-origin`, HSTS atrás de proxy HTTPS confiável; tokens de link nunca vão para os logs.
- **Container**: distroless sem shell, sem root, rootfs somente leitura, `cap_drop ALL`, `no-new-privileges`. Versão do Go fixada em `go.mod`/`Dockerfile`; `govulncheck` faz parte do checklist de versão.
- **Limitações aceitas**: sem cota de disco nem limite de zips/jobs por usuário autenticado; admin com escopo vê os links de todos; links são por caminho. Lista completa em [`docs/10-roadmap.md`](docs/10-roadmap.md).

## Documentação

A pasta [`docs/`](docs/README.md) é a fonte de verdade do projeto e é atualizada no mesmo commit que muda o comportamento:

| Documento | Conteúdo |
|---|---|
| [01 · Visão geral](docs/01-visao-geral.md) | Objetivo, requisitos, stack e decisões de arquitetura (ADRs) |
| [02 · Arquitetura](docs/02-arquitetura.md) | Pacotes, fluxo de uma requisição, ciclo de vida, limites |
| [03 · Segurança](docs/03-seguranca.md) | Modelo de ameaças, sandbox, autenticação, CSRF, cabeçalhos, links públicos, container |
| [04 · API HTTP](docs/04-api.md) | Todos os endpoints, payloads e códigos de erro |
| [05 · Uploads](docs/05-uploads.md) | Modos de envio, sessões em blocos, retomada, conflitos, limpeza |
| [06 · Banco de dados](docs/06-banco-de-dados.md) | Esquema SQLite, migrações, backup |
| [07 · Frontend](docs/07-frontend.md) | Estrutura React, estado, uploads, teclado, tema, celular |
| [08 · Operação](docs/08-operacao.md) | Deploy, variáveis, proxies, atualização, diagnóstico |
| [09 · Testes](docs/09-testes.md) | Suítes automatizadas e checklist manual |
| [10 · Roadmap](docs/10-roadmap.md) | Limitações conhecidas e próximos passos |

## Desenvolvimento

Requisitos: Go 1.26.8+ (a versão em `go.mod` é baixada automaticamente pelo `go`), Node 22+.

```bash
make run     # API em :8080 servindo ./data (cookies sem Secure)
make dev     # Vite em :5173 com proxy de /api para :8080
make web     # build do frontend em internal/server/webdist/dist
make build   # binário ./filezam com a UI embutida
make test    # go test ./... + tsc + vitest
```

Estrutura em duas partes: `cmd/` e `internal/` (Go: `vfs` é o núcleo de segurança, `server` os handlers HTTP, `store` o SQLite, `uploads` o protocolo de envio, `jobs` as operações em segundo plano) e `web/` (React 19 + TypeScript + Vite + Tailwind 4, embutido no binário via `go:embed`). Chamadas à API por script precisam do header `X-Filezam: 1`.

Subcomandos do binário: `serve` (padrão), `healthcheck` (usado pelo Docker), `reset-admin [senha]` (redefine a senha do admin e força troca no próximo login), `version`.

### Checklist antes de publicar uma versão

1. `docker compose up` limpo → login `admin/admin` → troca de senha obrigatória → senha antiga recusada.
2. Upload de um arquivo de vários GB: `docker stats` fica abaixo de 100 MB; recarregue a página no meio, solte o mesmo arquivo e veja a retomada; confira o `sha256sum`.
3. Upload de pasta com milhares de arquivos pequenos: interface responsiva, contagem correta.
4. Copiar/mover/excluir uma árvore grande com progresso e cancelamento.
5. ZIP de uma pasta grande abre com `unzip`.
6. Criar link de 1 hora, abrir em janela anônima, revogar → 404.
7. `curl '.../api/files?path=../../etc/passwd'` → 400; symlink em `/data` para `/config` → não abre.
8. Enviar `evil.svg` com `<script>` e `evil.html`: preview do SVG sem alerta, HTML só como texto/download.
9. Usuário com escopo não vê a pasta pai; admin muda o escopo → sessão dele cai.
10. Atrás do proxy real: cookie com `Secure`, IP verdadeiro na auditoria, upload de 200 MB completa.
11. Preview de PDF abre dentro da interface; no celular, toque abre e toque longo seleciona.
12. `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` e `cd web && npm audit --omit=dev` limpos.

## Contribuindo

Issues e pull requests são bem-vindos. Leia [`CONTRIBUTING.md`](CONTRIBUTING.md): ele resume as regras que não se negociam (todo acesso ao disco por `internal/vfs`, caminhos nunca em segmentos de URL, nunca servir HTML enviado por usuário, docs atualizados no mesmo commit, testes para todo endpoint que altera estado) e o fluxo de trabalho. Para vulnerabilidades, siga [`SECURITY.md`](SECURITY.md) em vez de abrir issue pública.

## Roadmap e limitações

O que já se sabe que falta (lixeira, cotas por usuário, busca indexada, 2FA, link de arquivo único, drag-and-drop interno) e o que foi descartado de propósito está em [`docs/10-roadmap.md`](docs/10-roadmap.md). Sugestões passam por issue antes de virar código.

## Autor

**Miguel Zamberlan** ([@miguelzamberlan](https://github.com/miguelzamberlan)) é o autor e responsável pelo projeto: define as prioridades, revisa as contribuições e publica as versões. O Filezam é desenvolvido no tempo livre, com o apoio de ferramentas de IA para revisão e implementação, e todo o código passa por testes automatizados e verificação manual antes de entrar na `main`. Dúvidas e sugestões: abra uma issue.

## Licença

[MIT](LICENSE) © 2026 Miguel Zamberlan. Use, modifique e distribua à vontade, mantendo o aviso de licença.
