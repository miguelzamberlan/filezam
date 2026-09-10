# 03 · Segurança

## Modelo de ameaças

| Ator | Objetivo | Controle principal |
|---|---|---|
| Visitante anônimo na internet | Ler arquivos, adivinhar senha, derrubar o serviço | Só `/api/auth/login`, `/api/public/*` e `/api/health` são públicos; rate limit; tokens de 256 bits |
| Usuário autenticado com escopo | Sair da subpasta, ler o banco, ver uploads/links de outros | `os.Root` aninhado no escopo; banco fora da raiz; filtros por `user_id` |
| Admin comprometido | Ler o host além de `/data` | Raiz fixa por env, nunca alterável pela UI; container sem root, rootfs somente leitura, `cap_drop ALL` |
| Quem envia arquivos | Executar script no navegador de quem visualiza | Nunca servir `text/html`; CSP `sandbox` em previews; `nosniff` |
| Site malicioso aberto no mesmo navegador | CSRF | Cookie `SameSite=Strict` + header customizado + `Sec-Fetch-Site` |
| Quem obtém o banco | Reutilizar sessões e tokens | Sessões só como hash SHA-256; senhas em Argon2id; tokens de link público ficam em claro (ver "Links públicos") |
| Quem rouba um cookie de sessão | Trocar a senha e tomar a conta | Troca de senha exige a senha atual, com os mesmos limites de taxa do login |

Fora do escopo: proteção contra um administrador do host, ataques ao proxy reverso, e sigilo de arquivos frente a quem tem acesso legítimo ao escopo. **Usuários autenticados são semi-confiáveis quanto a recursos**: não há cota de disco nem limite de zips/jobs simultâneos por usuário (um upload chunked pré-aloca o tamanho declarado, até o espaço livre); veja [10](10-roadmap.md). O escopo de um admin é uma conveniência de navegação, **não uma fronteira de confidencialidade**: a tela Compartilhados e o diálogo de propriedades mostram a um admin todos os links (com token) de todos os usuários, inclusive de pastas fora do escopo dele.

## Sandbox de caminhos (`internal/vfs`)

1. **Normalização** (`Normalize`): rejeita NUL, UTF-8 inválido, mais de 4096 bytes e segmentos com mais de 255 bytes. Percorre os segmentos mantendo uma pilha; `..` além do início é recusado com `ErrInvalidPath` (nunca silenciado). Resultado sem barra inicial/final; `""` é a raiz.
2. **Nomes reservados**: o prefixo `.filezam-` identifica arquivos internos (partes de upload). `NormalizeWritable` recusa qualquer segmento com esse prefixo e é usada **tanto em escrita quanto em leitura** (`queryPath` e os `?path=` públicos): uma parte de upload em andamento nunca é endereçável, mesmo conhecendo o id. A listagem oculta essas partes e as devolve à parte para limpeza.
3. **Nomes novos** (`ValidName`, aplicado por `Mkdir`/`MkdirAll`, rename, PUT, lote e sessões de upload): sem `/`, NUL, `.`/`..`, prefixo reservado, UTF-8 inválido nem **caracteres de controle** (`< 0x20`, `0x7f`). Entradas já existentes com nomes assim continuam legíveis, renomeáveis e apagáveis.
4. **`os.Root`**: todo `Open`, `OpenFile`, `Mkdir`, `Rename`, `Remove`, `Link`, `Lstat`, `Stat`, `Chtimes` é chamado sobre o `*os.Root`. Symlinks são seguidos apenas se a resolução permanecer dentro do root; caso contrário o kernel devolve "path escapes from parent", mapeado para `ErrEscape` → HTTP 400.
5. **Escopos e shares**: `base.Sub(rel)` usa `Root.OpenRoot`, que valida o caminho do próprio escopo. `Sub("")` devolve uma visão compartilhada do base (não fecha o root pai).
6. **Symlinks na listagem**: entradas de link recebem `link: true`; o tipo é `dir`/`file` se a resolução fica dentro do root, senão `other` (não navegável). A UI nunca cria symlinks; cópia pula e reporta; exclusão remove só o link.
7. **Raiz do escopo** (`""`) nunca pode ser renomeada, movida ou excluída (`ErrRootOp`).
8. **Aninhamento**: mover/copiar `src` para dentro de si mesmo é recusado por comparação de strings normalizadas (`IsWithin`).
9. **Pesquisa** (`Root.Find`): percorre com `Lstat`, nunca segue symlinks, pula nomes reservados e para nos limites de entradas/resultados/tempo; roda sobre o root do escopo, então nunca vê nada fora dele. O índice de nomes (`internal/index`, `Root.WalkEntries`) segue as mesmas regras na varredura e guarda só nome/tipo/tamanho/mtime; cada acerto vindo do índice é conferido com `Stat` no root do escopo antes de ser devolvido, então uma linha velha nunca revela nada fora do escopo.
10. **Lixeira**: `.filezam-trash` no root do escopo tem o prefixo reservado, logo nunca aparece em listagem, pesquisa, cópia, zip ou contagem, e não é endereçável por `?path=`. `walk`/`copyDir` pulam qualquer nome reservado. Restaurar volta pelo root base com caminhos base-relativos e só de linhas visíveis (as próprias, ou todas dentro do escopo para admin).
10. **Banco fora da raiz**: `config.Load` resolve symlinks (`EvalSymlinks`) de `FILEZAM_ROOT` e `FILEZAM_DATA_DIR` antes de verificar que o banco não está dentro da raiz.

## Autenticação e sessões

- Senhas: Argon2id com `m=64 MiB, t=3, p=2`, salt de 16 bytes, formato PHC. Política mínima: 8 caracteres (máximo 256). Verificação em tempo constante; usuário inexistente também executa Argon2 (hash fictício) para igualar o tempo.
- Bloqueio: 10 falhas seguidas → 15 min, dobrando a cada bloqueio subsequente enquanto o degrau anterior for menor que 24 h (último degrau: 32 h). Sucesso zera contadores. Consequência aceita: a resposta `423 locked` revela que a conta existe, e 10 tentativas por janela mantêm um usuário conhecido bloqueado.
- Limites de login: 10/min por IP, 5/min por usuário, 4 verificações Argon2 simultâneas. **A troca de senha (`POST /api/auth/password`) passa pelos mesmos limites e semáforo**, com evento `password.ratelimited` na auditoria: quem roubou um cookie não consegue forçar a senha atual.
- Sessão: token de 32 bytes aleatórios em cookie `filezam_session` (`HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` conforme `FILEZAM_SECURE_COOKIES`). O banco guarda só o SHA-256. Validade deslizante `FILEZAM_SESSION_TTL` (renovada no máximo a cada 5 min; valores acima de 30 dias são reduzidos a 30 dias na carga da configuração), teto absoluto de 30 dias a partir da criação.
- Revogação: troca de senha (própria ou pelo admin), mudança de escopo, de perfil ou desativação apagam as demais sessões do usuário. Exclusão de usuário cascateia. As alterações de usuários pelo admin são serializadas por um mutex no processo, para que a checagem de "último admin" não corra com outra requisição.
- Troca obrigatória: `must_change_password` bloqueia toda a API com `403 password_change_required`, exceto `me`, `password`, `logout` e `config`.

## CSRF

Requisições mutantes em `/api` exigem:

1. Header `X-Filezam: 1` (headers customizados forçam preflight CORS, que nunca é liberado porque o servidor não emite cabeçalhos CORS).
2. Se `Sec-Fetch-Site` existir, deve ser `same-origin` ou `none`; senão, o host de `Origin`/`Referer`, quando presente, deve ser igual a `Host`.

Isso vale inclusive para o login. Scripts e `curl` precisam enviar o header. A checagem fica dentro de `requireUser`, então cobre todo endpoint autenticado (inclusive `PUT` de chunks e logout).

## Cabeçalhos HTTP

Em todas as respostas: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, `Permissions-Policy` restritiva, `Cross-Origin-Opener-Policy: same-origin`, e `Strict-Transport-Security` quando a conexão é HTTPS (direta ou por proxy confiável).

Na SPA: `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; font-src 'self' data:; connect-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`. O build do Vite não emite scripts inline. `frame-src 'self'` existe para o iframe do preview de PDF.

**Exceção para conteúdo inline** (`serveFile` com `inline=1`): a resposta troca o `X-Frame-Options` por `SAMEORIGIN` e acrescenta `frame-ancestors 'self'` à CSP, senão o próprio iframe de PDF da SPA seria bloqueado. O iframe **não** leva o atributo `sandbox`: o Chrome recusa o visualizador de PDF em qualquer frame com esse atributo (mesmo com `allow-scripts`); o isolamento vem do cabeçalho `Content-Security-Policy: sandbox` que o servidor envia junto com o arquivo, que vale também quando a URL é aberta diretamente.

## Conteúdo enviado por usuários

`serveFile` decide entre `attachment` e `inline` (`detectType` em `handlers_files.go`):

- Inline permitido apenas para: imagens (`png, jpeg, gif, webp, avif, bmp, svg+xml, x-icon`), `video/*`, `audio/*`, `application/pdf` e texto.
- Tudo que é texto (inclusive `.html`, `.js`, `.svg` fora do modo imagem, JSON, XML) é servido como `text/plain; charset=utf-8` quando inline. `.xhtml`, `.mht`, `.wasm` e afins caem em `application/octet-stream` + `attachment`.
- Respostas inline levam `Content-Security-Policy: sandbox; default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'`.
- Qualquer outro tipo é `application/octet-stream` + `attachment`.
- `Content-Disposition` usa `mime.FormatMediaType` (RFC 2231 para nomes não-ASCII ou com caracteres especiais; o `net/http` ainda substitui CR/LF em cabeçalhos).

## Links públicos

- Token: 32 bytes `base64url`. A consulta pública continua sendo por SHA-256 (`token_hash`, sem vazamento de tempo por comparação de token), mas o token também é gravado em claro (`shares.token`, migração 002) para que a tela **Compartilhados** possa copiar o link de novo. Consequência aceita: quem obtiver o arquivo do banco consegue usar os links ainda ativos — os links são somente leitura, expiram e podem ser revogados. Links criados antes da migração 002 ficam sem token e não podem ser recopiados.
- Expiração no relógio do servidor; validade máxima `FILEZAM_SHARE_MAX_TTL`. Revogação apaga a linha.
- **O link segue o dono**: usuário desativado → todos os links dele respondem 404 (voltam se ele for reativado); escopo estreitado pelo admin → links de pastas que ficaram fora do novo escopo são apagados na hora (`sharesRevoked` no evento `user.update`); usuário excluído → links apagados em cascata.
- Público só pode: `info`, `list`, `content`, `zip`, sempre dentro de `base.Sub(share.Path)`. Não há escrita.
- **Link de arquivo** (`kind = file`): o root aberto é a pasta pai e `content` serve sempre `Base(path)`, ignorando `?path=`; `list`/`zip` respondem 409. Um link de arquivo nunca alcança irmãos.
- **Senha opcional**: Argon2id em `shares.password_hash`. `POST /api/public/{token}/unlock` verifica (5/min por link, semáforo do Argon2, auditoria `share.unlock.fail`) e grava o cookie `fz_s_<16 hex do hash do token>` = HMAC-SHA256(chave = hash da senha, mensagem = hash do token), `HttpOnly`, `SameSite=Strict`, 24 h. Sem cookie válido, `info` devolve só `locked: true` e nome; `list`/`content`/`zip` respondem 401 `share_locked`. Revogar o link ou criar outro invalida o cookie porque a chave muda.
- Rate limit por IP e no máximo 2 downloads/zips simultâneos por IP.
- Mesma resposta 404 para inexistente, expirado, revogado, dono desativado ou pasta removida.
- **Shares são por caminho**: mover ou renomear a pasta invalida o link, mas se outra pasta ocupar o mesmo caminho antes de o link expirar ele volta a funcionar apontando para ela. Revogue links de pastas que você recria.
- Tokens nunca vão para os logs: o middleware de log e o `recoverer` mascaram o segmento de token em `/api/public/…` e `/s/…`.
- A API devolve `url` construída com `FILEZAM_PUBLIC_URL` ou, na ausência, com esquema/host da requisição (`X-Forwarded-Proto`/`X-Forwarded-Host` só de proxies confiáveis). A interface web ignora essa `url` quando `FILEZAM_PUBLIC_URL` não está definido e monta o link com a origem do próprio navegador (`window.location.origin`), que é sempre o endereço que o usuário está usando.

## IP real e proxies

`X-Forwarded-For` é considerado apenas quando `RemoteAddr` está em `FILEZAM_TRUSTED_PROXIES`; toma-se o salto mais à direita que não seja um proxy confiável. O IP é usado para rate limit, auditoria e cookie `Secure=auto`.

## Auditoria

Tabela `audit_log` com `login.ok`, `login.fail`, `login.locked`, `login.ratelimited`, `logout`, `password.change`, `password.fail`, `password.ratelimited`, `user.create`, `user.update`, `user.delete`, `share.create`, `share.revoke`, `share.unlock.fail`, `trash.restore`, `trash.delete`, `trash.empty`, `index.rebuild`. Retenção: 180 dias. Visível em Administração → Auditoria.

## Container e dependências

Imagem `gcr.io/distroless/static-debian12:nonroot` (sem shell, sem gerenciador de pacotes). Compose: `user: PUID:PGID`, `read_only: true`, `tmpfs /tmp`, `cap_drop: [ALL]`, `no-new-privileges`. O binário não precisa gravar em lugar algum além de `/config` e `/data`. Não use `PUID=0`: nada impede, mas anula a garantia de processo sem root. O serviço `init` roda como root e faz `chown -R` em `/config`: confira `FILEZAM_HOST_CONFIG` antes do primeiro `up`.

A versão do Go fica fixada em `go.mod` (`go 1.26.x`) e no `Dockerfile` (`golang:1.26.x-alpine`): as correções de segurança da biblioteca padrão (`net/http`, `crypto/tls`, `os`) só entram quando esse número sobe. Antes de cada versão rode `govulncheck ./...` e `cd web && npm audit --omit=dev` (ver [09](09-testes.md)).

## Verificação contínua

Os testes em `internal/vfs/root_test.go` mantêm um arquivo-canário **fora** do root e afirmam que listagem, download, cópia, movimento, exclusão e zip nunca o alcançam nem o alteram, com symlinks para `/etc`, para `../outside`, loop e para um irmão interno. `internal/server/server_test.go` repete os ataques pela API (`..`, `%2e%2e`, symlink dentro do escopo, `evil.html` inline) e, em `TestSecurityHardening`, cobre os cabeçalhos do conteúdo inline, nomes com caracteres de controle, leitura de partes de upload, links de usuário desativado/escopo estreitado e o rate limit da troca de senha. Qualquer mudança em `vfs`, `handlers_files.go` ou `handlers_shares.go` exige esses testes verdes.
