# Filezam

Gestor de arquivos web, rápido e seguro, para expor uma pasta do seu servidor pelo navegador.

- Login com usuário e senha; vários usuários, cada um com acesso à raiz inteira ou a uma subpasta.
- Navegar, criar pastas, renomear, copiar, recortar/colar (mover), excluir, baixar (arquivo ou ZIP), favoritos.
- Compartilhar uma pasta por link público **somente leitura** com prazo de validade.
- Upload de arquivos grandes em blocos paralelos com retomada, upload de pastas inteiras (arraste e solte) e de milhares de arquivos pequenos em lote.
- Um único binário Go com a interface embutida; imagem Docker `distroless` sem shell, rodando sem root com rootfs somente leitura.

## Início rápido (Docker Compose)

```bash
git clone <seu-repositorio> filezam && cd filezam
cp .env.example .env
# edite .env: PUID/PGID (id -u / id -g), FILEZAM_HOST_ROOT (pasta a expor), FILEZAM_TRUSTED_PROXIES
mkdir -p data config
docker compose up -d --build
```

Acesse `http://127.0.0.1:8080` (ou pelo seu proxy reverso). Login inicial: `admin` / `admin` (ou o que estiver em `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD`). A troca de senha é obrigatória no primeiro acesso.

### Pastas e permissões

| Volume | Conteúdo |
|---|---|
| `${FILEZAM_HOST_ROOT}` → `/data` | Pasta raiz exibida pelo gestor. Para expor várias pastas do host, monte-as como subpastas: `- /mnt/midia:/data/midia`. |
| `${FILEZAM_HOST_CONFIG}` → `/config` | Banco SQLite (usuários, sessões, links, uploads em andamento, auditoria). Nunca é servido. |

O container roda como `PUID:PGID`. As duas pastas precisam ser graváveis por esse usuário; se não forem, o app encerra na inicialização com a mensagem `chown` sugerida. Use *bind mounts* (como no compose) e não volumes nomeados, que nascem pertencendo ao root.

O app nunca consegue sair de `/data`: todo acesso ao disco passa por `os.Root` do Go, que valida cada componente do caminho no kernel, inclusive links simbólicos. Um symlink dentro de `/data` apontando para fora é listado, mas não pode ser aberto.

### Atrás do proxy reverso

O app escuta HTTP puro e espera TLS no proxy. Configure:

- `FILEZAM_TRUSTED_PROXIES`: IPs/CIDRs do proxy. Só deles os cabeçalhos `X-Forwarded-For`/`X-Forwarded-Proto`/`X-Forwarded-Host` são aceitos (usados para IP real na auditoria/rate limit, cookie `Secure` e links de compartilhamento).
- `FILEZAM_SECURE_COOKIES=true` quando o acesso público é HTTPS (padrão recomendado). `false` só para uso local sem TLS.
- `FILEZAM_PUBLIC_URL=https://arquivos.seudominio.com` para montar os links públicos com o domínio certo.

Uploads são enviados em blocos de `FILEZAM_MAX_UPLOAD_CHUNK` (16 MiB por padrão), então nenhuma requisição é enorme, mas o proxy não pode limitar/bufferizar o corpo:

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

## Variáveis de ambiente

| Variável | Padrão | Descrição |
|---|---|---|
| `FILEZAM_ROOT` | `/data` (imagem) / `./data` | Pasta raiz dos arquivos |
| `FILEZAM_DATA_DIR` | `/config` (imagem) / `./config` | Banco de dados. Não pode ficar dentro de `FILEZAM_ROOT` |
| `FILEZAM_LISTEN` | `:8080` | Endereço de escuta |
| `FILEZAM_TRUSTED_PROXIES` | vazio | Lista de IPs/CIDRs separados por vírgula |
| `FILEZAM_SECURE_COOKIES` | `auto` | `true` / `false` / `auto` (detecta via `X-Forwarded-Proto` de proxy confiável) |
| `FILEZAM_PUBLIC_URL` | vazio | Base dos links públicos (`https://...`) |
| `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD` | `admin` / `admin` | Admin criado no primeiro início (troca forçada) |
| `FILEZAM_SESSION_TTL` | `168h` | Validade deslizante da sessão (teto absoluto: 30 dias) |
| `FILEZAM_MAX_UPLOAD_CHUNK` | `16MiB` | Tamanho do bloco de upload (1 MiB–1 GiB) |
| `FILEZAM_SHARE_MAX_TTL` | `720h` | Validade máxima de um link público |
| `FILEZAM_FSYNC` | `true` | `fsync` antes de finalizar cada upload |
| `FILEZAM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Usuários e escopos

Em **Administração → Usuários** o admin cria contas e define o perfil (administrador ou usuário) e a **pasta de acesso**: a raiz inteira ou uma subpasta específica. O usuário restrito enxerga a subpasta como se fosse a raiz e não consegue alcançar nada fora dela. Alterar escopo ou senha invalida as sessões do usuário. Não é possível remover ou rebaixar o último administrador.

Contas são bloqueadas por 15 minutos (dobrando a cada repetição) após 10 falhas seguidas de login; há também limite por IP e por usuário. Tudo fica registrado em **Auditoria**.

## Links públicos

Selecione uma pasta e clique em **Compartilhar**, escolha a validade e copie o link (`/s/<token>`). Quem tiver o link pode listar, visualizar e baixar (arquivo ou ZIP), nada mais. O token tem 256 bits e só o hash é guardado; revogar ou expirar torna o link inválido na hora. Mover ou renomear a pasta compartilhada invalida o link.

## Uploads

| Tamanho | Método |
|---|---|
| ≤ 1 MiB | Vários arquivos por requisição (`multipart`), até 200 arquivos / 32 MiB por lote |
| ≤ chunk | Um `PUT` direto |
| > chunk | Sessão em blocos: blocos paralelos, fora de ordem, idempotentes; `complete` só depois de todos |

Arquivos são gravados em `.filezam-upload-*.part` no diretório de destino e renomeados atomicamente ao final, então nunca aparece arquivo pela metade. Sessões abandonadas são apagadas após 24 h. Se o navegador for fechado no meio de um upload grande, ao voltar aparece um aviso: solte o mesmo arquivo na mesma pasta e só os blocos que faltam são enviados.

## Atalhos de teclado

`↑ ↓ Home End PgUp PgDn` navegar · `Shift`/`Ctrl` seleção múltipla · `Ctrl+A` tudo · `Enter` abrir · `Backspace`/`Alt+↑` subir · `F2` renomear · `Del` excluir · `Ctrl+C` `Ctrl+X` `Ctrl+V` copiar/recortar/colar · `Ctrl+Shift+N` nova pasta · `Esc` limpar · digitar letras pula para o nome.

## Desenvolvimento

Requisitos: Go 1.26+, Node 22+.

```bash
make run     # API em :8080 servindo ./data (cookies sem Secure)
make dev     # Vite em :5173 com proxy de /api para :8080
make web     # build do frontend em internal/server/webdist/dist
make build   # binário ./filezam com a UI embutida
make test    # go test ./... + tsc + vitest
```

Subcomandos do binário: `serve` (padrão), `healthcheck` (usado pelo Docker), `reset-admin [senha]` (redefine a senha do admin e força troca no próximo login), `version`.

## Checklist manual de validação

1. `docker compose up` limpo → login `admin/admin` → troca de senha obrigatória → senha antiga recusada.
2. Upload de um arquivo de vários GB: `docker stats` fica abaixo de 100 MB; recarregue a página no meio, solte o mesmo arquivo e veja a retomada; confira o `sha256sum`.
3. Upload de pasta com milhares de arquivos pequenos (`seq 1 10000 | xargs -I{} touch f{}`): interface responsiva, contagem correta.
4. Copiar/mover/excluir uma árvore grande com progresso e cancelamento.
5. ZIP de uma pasta grande abre com `unzip`.
6. Criar link de 1 hora, abrir em janela anônima, revogar → 404.
7. `curl '.../api/files?path=../../etc/passwd'` → 400; symlink em `/data` para `/config` → não abre.
8. Enviar `evil.svg` com `<script>` e `evil.html`: preview do SVG sem alerta, HTML só como texto/download.
9. Usuário com escopo não vê a pasta pai; admin muda o escopo → sessão dele cai.
10. Atrás do proxy real: cookie com `Secure`, IP verdadeiro na auditoria, upload de 200 MB completa.

## Licença

MIT
