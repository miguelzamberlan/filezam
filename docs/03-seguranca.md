# 03 · Segurança

## Modelo de ameaças

| Ator | Objetivo | Controle principal |
|---|---|---|
| Visitante anônimo na internet | Ler arquivos, adivinhar senha, derrubar o serviço | Só `/api/auth/login`, `/api/public/*` e `/api/health` são públicos; rate limit; tokens de 256 bits |
| Usuário autenticado com escopo | Sair da subpasta, ler o banco, ver uploads/links de outros | `os.Root` aninhado no escopo; banco fora da raiz; filtros por `user_id` |
| Admin comprometido | Ler o host além de `/data` | Raiz fixa por env, nunca alterável pela UI; container sem root, rootfs somente leitura, `cap_drop ALL` |
| Quem envia arquivos | Executar script no navegador de quem visualiza | Nunca servir `text/html`; CSP `sandbox` em previews; `nosniff` |
| Site malicioso aberto no mesmo navegador | CSRF | Cookie `SameSite=Strict` + header customizado + `Sec-Fetch-Site` |
| Quem obtém o banco | Reutilizar sessões e tokens | Só hashes SHA-256 de sessões/tokens; senhas em Argon2id |

Fora do escopo: proteção contra um administrador do host, ataques ao proxy reverso, e sigilo de arquivos frente a quem tem acesso legítimo ao escopo.

## Sandbox de caminhos (`internal/vfs`)

1. **Normalização** (`Normalize`): rejeita NUL, UTF-8 inválido, mais de 4096 bytes e segmentos com mais de 255 bytes. Percorre os segmentos mantendo uma pilha; `..` além do início é recusado com `ErrInvalidPath` (nunca silenciado). Resultado sem barra inicial/final; `""` é a raiz.
2. **Nomes reservados**: o prefixo `.filezam-` identifica arquivos internos (partes de upload). `NormalizeWritable` recusa qualquer segmento com esse prefixo; a listagem os oculta e os devolve à parte para limpeza.
3. **`os.Root`**: todo `Open`, `OpenFile`, `Mkdir`, `Rename`, `Remove`, `Link`, `Lstat`, `Stat`, `Chtimes` é chamado sobre o `*os.Root`. Symlinks são seguidos apenas se a resolução permanecer dentro do root; caso contrário o kernel devolve "path escapes from parent", mapeado para `ErrEscape` → HTTP 400.
4. **Escopos e shares**: `base.Sub(rel)` usa `Root.OpenRoot`, que valida o caminho do próprio escopo. `Sub("")` devolve uma visão compartilhada do base (não fecha o root pai).
5. **Symlinks na listagem**: entradas de link recebem `link: true`; o tipo é `dir`/`file` se a resolução fica dentro do root, senão `other` (não navegável). A UI nunca cria symlinks; cópia pula e reporta; exclusão remove só o link.
6. **Raiz do escopo** (`""`) nunca pode ser renomeada, movida ou excluída (`ErrRootOp`).
7. **Aninhamento**: mover/copiar `src` para dentro de si mesmo é recusado por comparação de strings normalizadas (`IsWithin`).

## Autenticação e sessões

- Senhas: Argon2id com `m=64 MiB, t=3, p=2`, salt de 16 bytes, formato PHC. Política mínima: 8 caracteres (máximo 256). Verificação em tempo constante; usuário inexistente também executa Argon2 (hash fictício) para igualar o tempo.
- Bloqueio: 10 falhas seguidas → 15 min, dobrando a cada bloqueio subsequente (teto 24 h). Sucesso zera contadores.
- Sessão: token de 32 bytes aleatórios em cookie `filezam_session` (`HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` conforme `FILEZAM_SECURE_COOKIES`). O banco guarda só o SHA-256. Validade deslizante `FILEZAM_SESSION_TTL` (renovada no máximo a cada 5 min), teto absoluto de 30 dias a partir da criação.
- Revogação: troca de senha (própria ou pelo admin), mudança de escopo, de perfil ou desativação apagam as demais sessões do usuário. Exclusão de usuário cascateia.
- Troca obrigatória: `must_change_password` bloqueia toda a API com `403 password_change_required`, exceto `me`, `password` e `logout`.

## CSRF

Requisições mutantes em `/api` exigem:

1. Header `X-Filezam: 1` (headers customizados forçam preflight CORS, que nunca é liberado porque o servidor não emite cabeçalhos CORS).
2. Se `Sec-Fetch-Site` existir, deve ser `same-origin` ou `none`; senão, o host de `Origin`/`Referer`, quando presente, deve ser igual a `Host`.

Isso vale inclusive para o login. Scripts e `curl` precisam enviar o header.

## Cabeçalhos HTTP

Em todas as respostas: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, `Permissions-Policy` restritiva, `Cross-Origin-Opener-Policy: same-origin`, e `Strict-Transport-Security` quando a conexão é HTTPS (direta ou por proxy confiável).

Na SPA: `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; font-src 'self' data:; connect-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`. O build do Vite não emite scripts inline. `frame-src 'self'` existe para o `iframe sandbox` do PDF.

## Conteúdo enviado por usuários

`serveFile` decide entre `attachment` e `inline` (`detectType` em `handlers_files.go`):

- Inline permitido apenas para: imagens (`png, jpeg, gif, webp, avif, bmp, svg+xml, x-icon`), `video/*`, `audio/*`, `application/pdf` e texto.
- Tudo que é texto (inclusive `.html`, `.js`, `.svg` fora do modo imagem, JSON, XML) é servido como `text/plain; charset=utf-8` quando inline.
- Respostas inline levam `Content-Security-Policy: sandbox; default-src 'none'; style-src 'unsafe-inline'`.
- Qualquer outro tipo é `application/octet-stream` + `attachment`.
- `Content-Disposition` usa `mime.FormatMediaType` (RFC 2231 para nomes não-ASCII).

## Links públicos

- Token: 32 bytes `base64url`; só o SHA-256 é gravado. Consulta por hash (sem vazamento de tempo por comparação de token).
- Expiração no relógio do servidor; validade máxima `FILEZAM_SHARE_MAX_TTL`. Revogação apaga a linha.
- Público só pode: `info`, `list`, `content`, `zip`, sempre dentro de `base.Sub(share.Path)`. Não há escrita.
- Rate limit por IP e no máximo 2 downloads/zips simultâneos por IP.
- Mesma resposta 404 para inexistente, expirado, revogado ou pasta removida.
- O link é construído com `FILEZAM_PUBLIC_URL` ou, na ausência, com esquema/host da requisição (`X-Forwarded-Proto`/`X-Forwarded-Host` só de proxies confiáveis).

## IP real e proxies

`X-Forwarded-For` é considerado apenas quando `RemoteAddr` está em `FILEZAM_TRUSTED_PROXIES`; toma-se o salto mais à direita que não seja um proxy confiável. O IP é usado para rate limit, auditoria e cookie `Secure=auto`.

## Auditoria

Tabela `audit_log` com `login.ok`, `login.fail`, `login.locked`, `login.ratelimited`, `logout`, `password.change`, `password.fail`, `user.create`, `user.update`, `user.delete`, `share.create`, `share.revoke`. Retenção: 180 dias. Visível em Administração → Auditoria.

## Container

Imagem `gcr.io/distroless/static-debian12:nonroot` (sem shell, sem gerenciador de pacotes). Compose: `user: PUID:PGID`, `read_only: true`, `tmpfs /tmp`, `cap_drop: [ALL]`, `no-new-privileges`. O binário não precisa gravar em lugar algum além de `/config` e `/data`.

## Verificação contínua

Os testes em `internal/vfs/root_test.go` mantêm um arquivo-canário **fora** do root e afirmam que listagem, download, cópia, movimento, exclusão e zip nunca o alcançam nem o alteram, com symlinks para `/etc`, para `../outside`, loop e para um irmão interno. `internal/server/server_test.go` repete os ataques pela API (`..`, `%2e%2e`, symlink dentro do escopo, `evil.html` inline). Qualquer mudança em `vfs` ou `handlers_files.go` exige esses testes verdes.
