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
| Quem rouba um cookie de sessão | Trocar a senha e tomar a conta | Troca de senha exige a senha atual, com os mesmos limites de taxa do login; ligar o 2FA exige a senha, desligar ou recadastrar exige senha e código |
| Quem descobre a senha | Entrar na conta | Verificação em duas etapas opcional (obrigatória para admins com `FILEZAM_REQUIRE_2FA_ADMINS`) |

Fora do escopo: proteção contra um administrador do host, ataques ao proxy reverso, e sigilo de arquivos frente a quem tem acesso legítimo ao escopo. **Recursos por usuário**: o admin pode definir uma cota de disco (`users.quota`, bytes no escopo; uploads, lote, sessões chunked e cópias são recusados com `507 quota_exceeded` quando estourariam), há no máximo 2 zips e 4 jobs em andamento por usuário (`429 busy`), e um upload chunked pré-aloca o tamanho declarado dentro da cota e do espaço livre; a soma do que as sessões abertas de um usuário reservam tem teto (`FILEZAM_UPLOAD_MAX_RESERVED`, 100 GiB por padrão, `413 upload_reserve_exceeded`), então nem um usuário sem cota ocupa o disco inteiro antes de enviar um byte. A sessão é exclusiva por **(usuário, destino)**: a sessão parada de um usuário não bloqueia outro do mesmo escopo, ninguém cancela a sessão alheia, e quando dois enviam o mesmo nome quem finaliza primeiro grava (o `Finalize` usa hardlink sem sobrescrever) e o outro recebe 409 `exists`. O uso é medido pelo índice de nomes (ou varredura limitada) com cache de 30 s ajustado a cada escrita aceita, então pequenas ultrapassagens transitórias são possíveis; lixeira e partes de upload não contam. O escopo de um admin é uma conveniência de navegação, **não uma fronteira de confidencialidade**: a tela Compartilhados e o diálogo de propriedades mostram a um admin todos os links (com token) de todos os usuários, inclusive de pastas fora do escopo dele.

## Sandbox de caminhos (`internal/vfs`)

1. **Normalização** (`Normalize`): rejeita NUL, UTF-8 inválido, mais de 4096 bytes, segmentos com mais de 255 bytes e **mais de `MaxDepth` (128) segmentos**. O limite de profundidade existe porque cada operação do `os.Root` resolve o caminho componente a componente: uma cadeia de profundidade *d* custa O(d²) chamadas de sistema, e sem o teto um usuário criaria (com `mkdir` e `move` aninhando cadeias) árvores que mantêm o indexador, a pesquisa, a medição de cota e os zips ocupados por horas. Percorre os segmentos mantendo uma pilha; `..` além do início é recusado com `ErrInvalidPath` (nunca silenciado). Resultado sem barra inicial/final; `""` é a raiz.
2. **Nomes reservados**: o prefixo `.filezam-` identifica arquivos internos (partes de upload, lixeira). `NormalizeWritable` recusa qualquer segmento com esse prefixo e é usada **tanto em escrita quanto em leitura** (`queryPath`, os `?path=` públicos, zip autenticado, criação de link, favoritos e o seletor de pastas do admin): uma parte de upload em andamento ou a lixeira de outro usuário nunca é endereçável, mesmo conhecendo o id. A listagem oculta essas partes e as devolve à parte para limpeza.
3. **Nomes novos** (`ValidName`, aplicado por `Mkdir`/`MkdirAll`, rename, PUT, lote e sessões de upload): sem `/`, NUL, `.`/`..`, prefixo reservado, UTF-8 inválido nem **caracteres de controle** (`< 0x20`, `0x7f`). Entradas já existentes com nomes assim continuam legíveis, renomeáveis e apagáveis.
4. **`os.Root`**: todo `Open`, `OpenFile`, `Mkdir`, `Rename`, `Remove`, `Link`, `Lstat`, `Stat`, `Chtimes` é chamado sobre o `*os.Root`. Symlinks são seguidos apenas se a resolução permanecer dentro do root; caso contrário o kernel devolve "path escapes from parent", mapeado para `ErrEscape` → HTTP 400.
5. **Escopos e shares**: `base.Sub(rel)` usa `Root.OpenRoot`, que valida o caminho do próprio escopo. `Sub("")` devolve uma visão compartilhada do base (não fecha o root pai).
6. **Symlinks na listagem**: entradas de link recebem `link: true`; o tipo é `dir`/`file` se a resolução fica dentro do root, senão `other` (não navegável). A UI nunca cria symlinks; cópia pula e reporta; exclusão remove só o link. Um **destino** alcançado por symlink que aponta para dentro da origem (criado fora do app) não gera recursão: a cópia anota (dispositivo, inode) de cada pasta que cria e nunca desce numa delas.
7. **Raiz do escopo** (`""`) nunca pode ser renomeada, movida ou excluída (`ErrRootOp`).
8. **Aninhamento**: mover/copiar `src` para dentro de si mesmo é recusado por comparação de strings normalizadas (`IsWithin`).
9. **Pesquisa** (`Root.Find`) e todas as varreduras (`walk`: índice, pesquisa, medição de cota, zip): percorrem com `Lstat`, nunca seguem symlinks, pulam nomes reservados, **não descem além de `WalkMaxDepth` (256)** (um `move` pode aninhar cadeias já existentes além de `MaxDepth`; o que estiver mais fundo fica invisível para essas tarefas em vez de custar horas) e **pulam pastas que não conseguem abrir** (permissões do host): uma pasta ilegível é reportada como entrada e ignorada, em vez de abortar o índice ou responder 403 à pesquisa inteira. A pesquisa para nos limites de entradas/resultados/tempo; roda sobre o root do escopo, então nunca vê nada fora dele. O índice de nomes (`internal/index`, `Root.WalkEntries`) segue as mesmas regras na varredura e guarda só nome/tipo/tamanho/mtime; cada acerto vindo do índice é conferido com `Stat` no root do escopo antes de ser devolvido, então uma linha velha nunca revela nada fora do escopo.
10. **Lixeira**: `.filezam-trash` no root do escopo tem o prefixo reservado, logo nunca aparece em listagem, pesquisa, cópia, zip ou contagem, e não é endereçável por `?path=`. `walk`/`copyDir` pulam qualquer nome reservado. Restaurar volta pelo root base com caminhos base-relativos e só de linhas visíveis (as próprias, ou todas dentro do escopo para admin). A linha no banco é gravada **antes** de mover o item: uma queda no meio deixa no máximo uma linha sem item (restaurar responde `not_found` e a apaga; a retenção também), nunca um item escondido na lixeira sem linha, fora da cota e invisível. Se o movimento falha e nada chegou à lixeira, a linha é desfeita.
11. **Banco fora da raiz**: `config.Load` resolve symlinks (`EvalSymlinks`) de `FILEZAM_ROOT` e `FILEZAM_DATA_DIR` antes de verificar que o banco não está dentro da raiz.

## Autenticação e sessões

- Senhas: Argon2id com `m=64 MiB, t=3, p=2`, salt de 16 bytes, formato PHC. Política mínima: 8 caracteres (máximo 256). Verificação em tempo constante; usuário inexistente também executa Argon2 (hash fictício) para igualar o tempo.
- Bloqueio (`auth.Lockout`, em memória): 10 falhas seguidas para o mesmo par **(usuário, IP)** → 15 min, dobrando a cada bloqueio até 24 h; sucesso zera. A resposta é sempre `401 bad_credentials`, idêntica à de senha errada e com o mesmo custo de Argon2 (hash fictício), então o bloqueio não revela que a conta existe; por ser por IP, um atacante não consegue manter o usuário legítimo bloqueado de outro endereço (quem divide o mesmo NAT com o atacante é afetado: aceito). O evento `login.locked` vai para a auditoria com o fim do bloqueio; `users.failed_logins` continua contando para o admin.
- Limites de login: 10/min por IP, 5/min por **(usuário, IP)**, 4 verificações Argon2 simultâneas. O limitador por conta leva o IP na chave pelo mesmo motivo do bloqueio: um limitador só por nome deixaria qualquer um trancar o admin de fora com cinco tentativas baratas por minuto. **A troca de senha (`POST /api/auth/password`) e a ativação do 2FA (`totp/enable`) passam pelos mesmos limites e semáforo**, com evento `password.ratelimited` na auditoria: quem roubou um cookie não consegue forçar a senha atual.
- Sessão: token de 32 bytes aleatórios em cookie `filezam_session` (`HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` conforme `FILEZAM_SECURE_COOKIES`). O banco guarda só o SHA-256. Validade deslizante `FILEZAM_SESSION_TTL` (renovada no máximo a cada 5 min; valores acima de 30 dias são reduzidos a 30 dias na carga da configuração), teto absoluto de 30 dias a partir da criação.
- Revogação: troca de senha (própria ou pelo admin), mudança de escopo, de perfil ou desativação apagam as demais sessões do usuário. Exclusão de usuário cascateia. As alterações de usuários pelo admin são serializadas por um mutex no processo, para que a checagem de "último admin" não corra com outra requisição.
- Troca obrigatória: `must_change_password` bloqueia toda a API com `403 password_change_required`, exceto `me`, `password`, `logout`, `config` e os endpoints de 2FA; a exigência de 2FA para admins usa o mesmo mecanismo (`403 totp_required`).

## Verificação em duas etapas (TOTP)

- Algoritmo: TOTP (RFC 6238) sobre HOTP SHA-1, 6 dígitos, 30 s, tolerância de ±1 intervalo; tudo com a biblioteca padrão (`internal/auth/totp.go`). O último intervalo aceito fica em `users.totp_counter` e um código nunca é aceito duas vezes: a gravação é um `UPDATE … WHERE totp_counter < ?` cujo `RowsAffected` decide, então de duas requisições simultâneas com o mesmo código só uma passa. Códigos de recuperação são consumidos por compare-and-swap da lista (`ConsumeTOTPRecovery`), para que dois usos simultâneos não ressuscitem um código gasto.
- Segredo: 20 bytes aleatórios em base32, gravados **cifrados** (AES-256-GCM, `auth.Seal`) com a chave do servidor: `FILEZAM_SECRET_KEY` (64 hex) ou, na ausência, `<FILEZAM_DATA_DIR>/secret.key` gerado no primeiro início com modo 0600. Um dump do banco sozinho não entrega os segredos; a chave e o banco precisam ser copiados juntos no backup.
- Cadastro: `setup` gera um segredo pendente em memória (10 min) e devolve a URI `otpauth://`; `enable` exige **a senha atual** e um código válido, só então grava o segredo, gera 10 códigos de recuperação (mostrados uma vez, guardados como SHA-256) e derruba as outras sessões. Com o 2FA já ativo, `setup` e `enable` respondem `409 totp_already_enabled`: recadastrar equivale a desligar + ligar, e desligar pede senha e código, então um cookie roubado não troca o segredo nem tranca o dono fora.
- Login: senha certa em conta com 2FA devolve `{totpRequired, token}` (token de 24 bytes, 5 min, preso ao IP, uso único) em vez da sessão; `POST /api/auth/totp` recebe token + código (6 dígitos ou código de recuperação) e cria a sessão. Erros passam pelo limitador por usuário e por um bloqueio próprio por (usuário, IP) (`totp|user|ip`), sempre `401 bad_totp`.
- Dispositivo confiável: com `trust: true` o servidor grava `fz_trust` = `uid.exp.HMAC-SHA256(chave, uid|exp|totp_enabled_at)`, `HttpOnly`, `SameSite=Strict`, 30 dias, sem tabela. Desligar ou recadastrar o 2FA muda `totp_enabled_at` e invalida todos os dispositivos de uma vez; não há revogação individual (limitação aceita).
- Desativar ou gerar novos códigos exige senha atual e um código. O admin pode zerar o 2FA de qualquer usuário (`totp.reset`, sessões derrubadas). `FILEZAM_REQUIRE_2FA_ADMINS=true` bloqueia a API para admins sem 2FA (`403 totp_required`) até o cadastro, como a troca de senha obrigatória.
- Auditoria: `login.totp_pending`, `totp.fail`, `totp.recovery`, `totp.enable`, `totp.disable`, `totp.recovery_reset`, `totp.reset`.

## CSRF

Requisições mutantes em `/api` exigem:

1. Header `X-Filezam: 1` (headers customizados forçam preflight CORS, que nunca é liberado porque o servidor não emite cabeçalhos CORS).
2. Se `Sec-Fetch-Site` existir, deve ser `same-origin` ou `none`; senão, o host de `Origin`/`Referer`, quando presente, deve ser igual a `Host`.

Isso vale inclusive para o login. Scripts e `curl` precisam enviar o header. A checagem fica dentro de `requireUser`, então cobre todo endpoint autenticado (inclusive `PUT` de chunks e logout).

## Extração de arquivos compactados

Extrair é a primeira operação que interpreta **estrutura** de conteúdo de terceiros — e, com os
links públicos de recebimento, esse conteúdo pode ter sido depositado por alguém sem conta. O que
sustenta que isso seja seguro:

- **Pasta nova e um `os.Root` aninhado.** O destino é sempre uma pasta criada na hora (`UniqueName`
  se o nome estiver ocupado), e a extração roda dentro de `base.Sub(destino)`. São três barreiras de
  kernel empilhadas: escopo, destino, e a validação do caminho.
- **O nome da entrada é normalizado sozinho, antes de ser juntado ao destino.** Essa ordem não é
  intercambiável: normalizar o caminho já concatenado faria `destino/../x` colapsar em silêncio para
  um irmão do destino, dentro do escopo. Sozinho, o `..` estoura em `Normalize` porque a pilha
  começa vazia. Depois disso cada segmento passa por `ValidName`.
- **Profundidade do caminho final**, não da entrada: `Depth(destino) + Depth(entrada) > MaxDepth`
  recusa. Senão daria para criar um arquivo que existe, ocupa cota e que a interface não consegue
  nem abrir nem apagar, porque `queryPath` recusaria o caminho.
- **Só arquivo regular e diretório.** Nenhum symlink é criado, em hipótese alguma: ele permitiria
  que a entrada seguinte escrevesse através dele para fora.
- **O modo declarado é ignorado** (0644 e 0755 fixos): `setuid`, `setgid` e `sticky` vindos do
  arquivo não existem no resultado.
- **Nunca sobrescreve**: cada arquivo é criado com `O_CREATE|O_EXCL`, que também falha se o nome for
  um symlink. Entrada duplicada dentro do mesmo arquivo vira aviso.
- **Entrada cifrada é pulada explicitamente.** O `archive/zip` do Go não valida esse bit: sem a
  checagem, ele copiaria o texto cifrado para o disco com nome legítimo e só acusaria erro de
  checksum no fim. Qualquer erro no meio da cópia também apaga o arquivo parcial.
- **Bomba de descompressão**: o tamanho descomprimido do cabeçalho é escolhido por quem monta o
  arquivo e mente, então ele nunca é usado como limite — contam os bytes que realmente passam pelo
  disco (`extract_max_bytes`). O número de entradas tem teto próprio (`extract_max_entries`), porque
  a cota limita bytes e não inodes. E o tamanho do **arquivo de origem** (`extract_max_archive`) é o
  único teto que morde antes de o `zip.NewReader` carregar o diretório central inteiro na memória.
- **Recursos**: uma extração por usuário e duas no servidor inteiro, job cancelável, contexto
  conferido a cada entrada. Ao falhar ou ser cancelada, a pasta de destino é removida com `Remove` —
  e não com a varredura, que no cancelamento não apagaria nada por já estar com o contexto morto.
- **Não é recursivo**: um `.zip` dentro do `.zip` sai como arquivo.
- **Nada é executado.** Extrair grava bytes; não há shell, `exec` nem interpretação do conteúdo. O
  pior caso realista é consumo de disco e CPU, ambos limitados.
- **Nomes legados**: um zip sem o bit UTF-8 traz os nomes na code page do sistema que compactou; eles
  são convertidos de CP437 antes de validar, senão todo arquivo com acento seria recusado.
- Limitações aceitas: o diretório central é carregado inteiro na memória pelo `archive/zip`
  (mitigado pelo teto de tamanho do arquivo e pelos semáforos); `.tar.gz` ainda não é suportado; e
  trocar o arquivo por fora (Samba) durante a leitura faz a extração falhar, sem escapar do sandbox.

## Miniaturas

Gerar miniatura é decodificar imagem vinda de terceiros, e é a única resposta de conteúdo que o
produto manda o navegador guardar. As duas coisas têm tratamento próprio:

- **Recusa pelo cabeçalho, antes de decodificar.** `image.DecodeConfig` lê só as dimensões; uma
  imagem declarada como 50000×50000 é recusada por `thumbs_max_pixels` sem nunca alocar o bitmap.
  Há também um teto de tamanho de arquivo (`thumbs_max_file`).
- **Só os decodificadores da biblioteca padrão e de `golang.org/x/image`** (JPEG, PNG, GIF, WebP,
  BMP, TIFF), todos em Go, sem CGO. HEIC, AVIF, RAW, PDF e vídeo exigiriam bibliotecas em C na
  imagem distroless e ficam de fora: caem no ícone por tipo.
- **O cache vive no `DataDir`**, nunca na árvore do usuário: não aparece em listagem, zip, cópia ou
  backup de conteúdo, e segue o precedente do banco e do `secret.key`. A chave é o SHA-256 de
  `(dev, ino, mtime, tamanho, versão)`, então qualquer alteração do arquivo gera uma chave nova e a
  invalidação é automática. As entradas velhas ficam para trás e são recolhidas pela varredura
  horária quando o cache passa de `thumbs_cache_max`.
- **Cabeçalhos de cache**: esta é a exceção ao `no-store` que vale para todo o resto do conteúdo. A
  resposta vai com `private, max-age=7d, immutable` **porque a URL carrega o `mtime`** (`?v=`) e
  muda junto com o arquivo. `private` impede proxy compartilhado; a miniatura fica no cache do
  navegador de quem tem acesso, como a imagem original já ficaria se fosse aberta. É o que faz a
  segunda visita a uma pasta de fotos não gerar requisição nenhuma.
- **Concorrência**: no máximo 4 gerações por usuário, e a requisição **espera** por um slot (até
  20 s) em vez de receber 429. Um `<img>` que recebe erro não tenta de novo, então recusar deixaria
  o ícone congelado na tela — a mesma razão pela qual o download público espera.
- **Só no navegador autenticado.** Links públicos não têm miniatura: o teto de 2 downloads
  simultâneos por IP seria atingido por uma grade inteira.
- Desligável pelo administrador, e cada usuário ainda pode desligar nas próprias preferências.

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
- **Markdown formatado** é renderizado pela SPA, não pelo servidor (que continua mandando `text/plain`): `react-markdown` gera elementos React, sem `dangerouslySetInnerHTML`; HTML bruto do arquivo é descartado (`skipHtml`), `defaultUrlTransform` zera protocolos fora de `http(s)`/`mailto`/etc. (`javascript:`, `data:`), e imagens externas continuam bloqueadas pelo `img-src 'self'` da CSP da SPA (evita rastreamento por pixel). Relativos só alcançam a pasta do arquivo e passam pelas mesmas rotas de conteúdo (`vfs`, escopo, share).
- `Content-Disposition` usa `mime.FormatMediaType` (RFC 2231 para nomes não-ASCII ou com caracteres especiais; o `net/http` ainda substitui CR/LF em cabeçalhos).

## Links públicos

- Token: 32 bytes `base64url`. A consulta pública continua sendo por SHA-256 (`token_hash`, sem vazamento de tempo por comparação de token), mas o token também é gravado em claro (`shares.token`, migração 002) para que a tela **Compartilhados** possa copiar o link de novo. Consequência aceita: quem obtiver o arquivo do banco consegue usar os links ainda ativos. Com o link de envio (migração 011) isso deixou de ser só leitura: um banco vazado permite **escrever** nos links de envio vivos — restrito a pastas dedicadas e vazias na criação, com cota e no máximo 30 dias de validade, e sem nenhuma forma de ler o que está lá. Links criados antes da migração 002 ficam sem token e não podem ser recopiados.
- **Apelido (`slug`, migração 011)**: endereço legível escolhido pelo usuário (`/s/orcamento-2026`), único em toda a instalação, em `[a-z0-9-]` com 3–63 caracteres e fora de uma lista curta de nomes reservados (anti-phishing; não há colisão de rota porque `/s/` é uma rota só da SPA). **O apelido não é segredo**: ele é adivinhável por construção, então **senha é obrigatória** e o sigilo do link mora inteiramente nela, com o rate limit de 5/min por (link, IP) que já protegia a senha. Duas defesas a mais contra varredura: um link travado responde só `{kind, mode, expiresAt, now, locked}` — sem o nome, que seria um oráculo do que existe do outro lado — e cada tentativa **malsucedida** com forma de apelido consome um balde de 30/min por IP, de modo que quem tem o endereço certo nunca é penalizado. Ligado/desligado por `slugs_enabled`.
- **Apelido revogado não volta ao pool**: revogar (ou mover/apagar a pasta) marca `revoked_at` em vez de apagar a linha quando há apelido, e o índice único mantém o endereço preso a quem o criou. Sem isso, bastaria esperar um link cair para reivindicar `/s/contabilidade` e passar a receber o que era destinado a outra pessoa. O dono libera o endereço de propósito com `DELETE /api/shares/{id}?purge=1`. Exceção: excluir o usuário leva os apelidos dele junto (CASCADE).
- Expiração no relógio do servidor; validade máxima `FILEZAM_SHARE_MAX_TTL`. Revogação apaga a linha.
- **O link segue o dono**: usuário desativado → todos os links dele respondem 404 (voltam se ele for reativado); escopo estreitado pelo admin → links de pastas que ficaram fora do novo escopo são apagados na hora (`sharesRevoked` no evento `user.update`); usuário excluído → links apagados em cascata.
- Um link de leitura (`mode = read`) só permite `info`, `list`, `content` e `zip`, sempre dentro de `base.Sub(share.Path)`. Um link de envio (`mode = drop`) permite **só escrita** e responde 404 nas quatro rotas de leitura; o inverso também vale. Os dois sentidos respondem 404, e não 403, para manter a invariante de que tudo que "não existe ali" responde igual.
- **Link de arquivo** (`kind = file`): o root aberto é a pasta pai e `content` serve sempre `Base(path)`, ignorando `?path=`; `list`/`zip` respondem 409. Um link de arquivo nunca alcança irmãos.
- **Senha opcional**: Argon2id em `shares.password_hash`. `POST /api/public/{token}/unlock` verifica (5/min por **(link, IP)**, semáforo do Argon2, auditoria `share.unlock.fail`) e grava o cookie `fz_s_<16 hex do hash do token>` = `<exp>.<HMAC-SHA256(chave = hash da senha, mensagem = hash do token | exp)>`, `HttpOnly`, `SameSite=Strict`, 24 h. A validade fica assinada dentro do valor, então um cookie vazado morre em 24 h mesmo que o cliente ignore o `Max-Age`. Sem cookie válido, `info` devolve só `locked: true` e nome; `list`/`content`/`zip` respondem 401 `share_locked`. Revogar o link ou criar outro invalida o cookie porque a chave muda.
- **Link de envio (`mode = drop`, migração 011)** — escrita anônima, com camadas:
  - Só existe quando o administrador liga `drop_enabled` (padrão **desligado**; 403 `feature_disabled` na criação), o que também vale para o próprio admin. O interruptor é conferido **a cada requisição**, na resolução do link (`ifEnabled`), não só na criação: desligá-lo precisa parar na hora os links que já existem, que é exatamente o que se faz ao perceber abuso. Aí a resposta é 404, para não distinguir de um link inexistente. O mesmo vale para `slugs_enabled`: apelidos param de resolver, e o token do mesmo link continua valendo.
  - A pasta é **criada na hora** e precisa estar vazia, inclusive de partes de upload (`Root.IsEmpty`): nada que já existia fica exposto a quem envia, e um nome que chega da internet nunca disputa espaço com um arquivo do usuário.
  - **Nunca sobrescreve**: `overwrite` é sempre falso e uma colisão vira `nome (1).ext`. A busca pelo nome livre olha o disco **e** os nomes já reservados pelas sessões em voo do link (`ShareOpenNames`), senão dois remetentes escolheriam o mesmo nome final e o segundo bateria no índice único de `uploads`.
  - **O desvio de nome é invisível para quem envia**: o recibo guarda os dois nomes (`name`, o que está no disco e que o dono vê; `sent_name`, o que o remetente pediu) e só o segundo volta na resposta e na lista de envios. Contar que "seu arquivo virou `nome (1).ext`" transformaria o endpoint de escrita num oráculo de existência: bastaria enviar 1 byte com um nome chutado para descobrir o que já está na pasta, de outro remetente ou do próprio dono — exatamente o que o link write-only existe para impedir. O que o remetente continua vendo é a cota e a contagem agregadas, necessárias para ele saber se ainda cabe.
  - **Plano, sem pastas**: o protocolo não tem `dir`; o nome passa por `ValidName` e não existe superfície de traversal.
  - **Cota obrigatória na criação**, limitada por `drop_max_quota` e pela cota restante do dono, com `max_file_bytes`, `max_files`, no máximo 8 sessões abertas por remetente e `drop_max_links` links ativos por usuário. As sessões em blocos abertas contam na cota enquanto existem, porque pré-alocam o tamanho declarado (`fallocate`) — é isso, e não a cota do usuário, que limita a reserva: `checkQuota` mede por `IndexSumSize`/`ScanLimited`, e ambos pulam `.filezam-*`. Sessões abandonadas caem em `drop_stale_age` (2 h por padrão, contra 24 h dos uploads autenticados). `FILEZAM_UPLOAD_MAX_RESERVED` continua como rede final, já que a sessão é gravada com o `user_id` do dono.
  - **Admissão serializada por link**: contar, somar e inserir acontecem sob o mesmo mutex, senão visitantes simultâneos leriam o mesmo estado e passariam todos.
  - **507 `drop_full` é único** para a cota do link e a do dono: distinguir as duas contaria a um anônimo como anda a conta de quem criou o link.
  - **Identidade do remetente**: cookie `fz_d_<16 hex do hash do token>` = `<id>.<HMAC-SHA256(chave do servidor, hash do token | id)>`, `HttpOnly`, `SameSite=Strict`, válido até o link expirar. Serve só para o visitante reencontrar os próprios envios; sem a assinatura, `sender` seria texto escolhido pelo cliente e a lista viraria uma sonda para ver o que outros mandaram. Cookie adulterado → o servidor emite uma identidade nova.
  - **As sessões anônimas são invisíveis no caminho autenticado** (`ListUploads`/`GetUpload` filtram `share_id = 0`): a interface do dono aborta toda pendência que enxerga, então listá-las faria abrir o navegador de arquivos cancelar o envio de terceiros. A autorização de uma sessão pública é o par (link, remetente), nunca o usuário.
  - **Balde de requisições próprio** para escrita (900/min por IP) mais 2 envios simultâneos por IP: o balde de leitura (120/min) mataria um envio legítimo de algumas dezenas de arquivos, já que cada arquivo em blocos custa três requisições. O custo real fica limitado por bytes, cota e slots, não por contagem.
  - Auditoria `share.drop.upload` com o **id do link** — o segmento de token é mascarado nos logs, e sem o id não daria para saber qual link está sendo martelado.
- **O que sai de um arquivo compactado é conteúdo de terceiros** pelas mesmas razões: ao pré-visualizar depois, valem a allow-list de `detectType` e a CSP `sandbox`, como em qualquer arquivo enviado.
- **Conteúdo que agora vem da internet**: um arquivo num link de envio foi enviado por alguém sem conta, e o dono vai pré-visualizá-lo depois pela interface autenticada. As defesas contra XSS armazenada são as de sempre (allow-list de `detectType`, tudo que é texto servido como `text/plain`, CSP `sandbox` no inline, `nosniff`), mas a origem do conteúdo mudou de "usuário confiável" para "qualquer um com o link" — ao avaliar mudanças nessas defesas, é esse o modelo a considerar.
- Rate limit por IP, no máximo 2 downloads/zips simultâneos por IP e **4 zips públicos simultâneos no servidor inteiro** (um zip de pasta grande custa CPU e leitura de disco por minutos, e visitantes anônimos trocam de IP à vontade; não há teto de bytes por zip). Acima disso a requisição **espera** um slot (até 30 s; desiste se o visitante cancelar) em vez de responder 429 na hora: um preview de Markdown pede todas as imagens de uma vez e um `<img>` recusado fica quebrado. Quem espera não ocupa disco nem banda; o número de esperas por IP é limitado pelo rate limit de 120 requisições/min.
- Mesma resposta 404 para inexistente, expirado, revogado, dono desativado ou pasta removida.
- Excluir, mover ou apagar a pasta de um link de envio também **descarta as sessões em voo** (parte no disco e linha no banco), em vez de deixá-las até a varredura horária num caminho que talvez nem exista mais.
- **Excluir, mover ou renomear pelo app revoga o link** do item e de tudo abaixo dele, de qualquer dono (`DeleteSharesUnder`, comparação por bytes do caminho, então `pub2` não cai junto com `pub`). A exclusão revoga antes de apagar; restaurar da lixeira devolve o item, não o link. Isso vale em qualquer sistema de arquivos, inclusive nos que reutilizam números de inode.
- **Mudanças feitas por fora (Samba, SSH) caem na identidade**: o link guarda `dev`/`inode` do item na criação (migração 006) e a cada acesso o item encontrado no caminho precisa ter a mesma identidade; movido por fora, o link responde 404, e um item novo criado no mesmo caminho **não** o reativa. Mover por fora e de volta preserva o inode e o link volta a funcionar. Em sistemas de arquivos que não informam inode (`Identity` devolve zeros) vale só o caminho, como antes; links criados antes da migração 006 também. A proteção contra "apagar e recriar no mesmo caminho" depende de o sistema de arquivos não reutilizar o número de inode imediatamente: ext4/tmpfs em geral não reutilizam, overlayfs pode reutilizar (o `TestShareBoundToInode` falha nele). Para mudanças por fora é uma defesa em profundidade, não uma garantia.
- **Configurações globais (migração 011)**: `slugs_enabled` e `drop_enabled` (mais os tetos dos links de envio) vivem na tabela `settings` e são editáveis em **Administração → Configurações do sistema**, com auditoria `settings.update` registrando antes e depois. Chave ausente cai no padrão de fábrica. O teto de 30 dias do vencimento é constante no código, não configuração: um valor maior no banco é reduzido na leitura (`clampSettings`), assim como todo limite fora de faixa. Isso não amplia o poder de um admin comprometido, que já podia alterar escopo e cota de qualquer conta.
- Tokens nunca vão para os logs: o middleware de log, o `recoverer` e o registro de erros 5xx mascaram o segmento de token em `/api/public/…` e `/s/…`.
- A API devolve `url` construída com `FILEZAM_PUBLIC_URL` ou, na ausência, com esquema/host da requisição (`X-Forwarded-Proto`/`X-Forwarded-Host` só de proxies confiáveis). A interface web ignora essa `url` quando `FILEZAM_PUBLIC_URL` não está definido e monta o link com a origem do próprio navegador (`window.location.origin`), que é sempre o endereço que o usuário está usando.

## IP real e proxies

`X-Forwarded-For` é considerado apenas quando `RemoteAddr` está em `FILEZAM_TRUSTED_PROXIES`; todas as linhas do cabeçalho são concatenadas (`Header.Values`, não `Get`: proxies como o HAProxy acrescentam uma linha nova em vez de anexar à existente, e a primeira linha pode ter vindo do cliente) e toma-se o salto mais à direita que não seja um proxy confiável. `X-Forwarded-Proto` e `X-Forwarded-Host` usam o último valor da última linha pelo mesmo motivo. O IP é usado para rate limit, auditoria e cookie `Secure=auto`.

Quem conecta de um endereço confiável escolhe o IP que o app vê: consegue escapar dos limites e do bloqueio por IP (a força bruta fica limitada só pelo Argon2 e pelo 2FA) e forjar esquema/host, mas não entra sem senha, não sai do escopo nem passa pelo CSRF. Por isso a lista deve conter só o proxy. Com Docker, uma conexão feita pelo próprio servidor numa porta publicada chega pelo gateway da rede (não por `127.0.0.1`), e o `docker-compose.yml` fixa a sub-rede (`172.31.250.0/24`) para que o `.env.example` confie só no gateway `172.31.250.1`; conexões da rede local chegam com o IP real e não são confiáveis. Gravidade por configuração e receitas em [08](08-operacao.md#porta-local-e-proxies-confiáveis).

## Métricas

`GET /metrics` (formato Prometheus, sem dependência externa) só existe com `FILEZAM_METRICS_TOKEN` definido e aceita `Authorization: Bearer <token>` (comparação em tempo constante) ou uma sessão de admin. Expõe contadores (requisições por método/classe de status, soma e contagem de duração, logins ok/falha, bytes recebidos em chunks, jobs concluídos por tipo/estado) e medidores no momento da coleta (usuários, sessões, links ativos, itens/bytes na lixeira, entradas e última varredura do índice, jobs por estado, disco). Nenhum nome de arquivo, usuário ou token aparece nas métricas.

## Auditoria

Tabela `audit_log` com `login.ok`, `login.fail`, `login.locked`, `login.ratelimited`, `logout`, `password.change`, `password.fail`, `password.ratelimited`, `user.create`, `user.update`, `user.delete`, `share.create`, `share.revoke`, `share.unlock.fail`, `trash.restore`, `trash.delete`, `trash.empty`, `index.rebuild`, `login.totp_pending`, `totp.fail`, `totp.recovery`, `totp.enable`, `totp.disable`, `totp.recovery_reset`, `totp.reset`. Retenção: 180 dias. Visível em Administração → Auditoria.

## Container e dependências

Imagem `gcr.io/distroless/static-debian12:nonroot` (sem shell, sem gerenciador de pacotes). Compose: `user: PUID:PGID`, `read_only: true`, `tmpfs /tmp`, `cap_drop: [ALL]`, `no-new-privileges`. O binário não precisa gravar em lugar algum além de `/config` e `/data`. Não use `PUID=0`: nada impede, mas anula a garantia de processo sem root. O serviço `init` roda como root, mas só troca o dono de `/config` em si e dos arquivos `filezam.db*`/`secret.key` nela (nunca `-R`), e aborta se `/config` tiver subpastas: um `FILEZAM_HOST_CONFIG` apontando para `/` ou `$HOME` por engano não reescreve o dono de uma árvore inteira.

As imagens base do `Dockerfile` (e o `alpine` do `init`) são fixadas por digest do índice multi-arquitetura, para que o mesmo commit gere a mesma imagem; o Dependabot (ecossistemas `docker` e `docker-compose`) abre PR quando a tag ganha um digest novo. A versão do Go fica fixada em `go.mod` (`go 1.26.x`) e no `Dockerfile` (`golang:1.26.x-alpine`): as correções de segurança da biblioteca padrão (`net/http`, `crypto/tls`, `os`) só entram quando esse número sobe. A CI (`.github/workflows/ci.yml`) roda `govulncheck` e `npm audit --omit=dev` em cada PR; `make vuln` faz o mesmo localmente. O Dependabot abre PRs mensais para módulos Go, npm, imagens base e actions.

## Verificação contínua

Os testes em `internal/vfs/root_test.go` mantêm um arquivo-canário **fora** do root e afirmam que listagem, download, cópia, movimento, exclusão e zip nunca o alcançam nem o alteram, com symlinks para `/etc`, para `../outside`, loop e para um irmão interno. `internal/server/server_test.go` repete os ataques pela API (`..`, `%2e%2e`, symlink dentro do escopo, `evil.html` inline); `TestSecurityHardening` cobre os cabeçalhos do conteúdo inline, nomes com caracteres de controle, leitura de partes de upload, links de usuário desativado/escopo estreitado e o rate limit da troca de senha; `TestPublicationHardening` cobre caminhos reservados em zip/link/favorito, `X-Forwarded-For` em várias linhas, o limitador por (usuário, IP) e a validade assinada do cookie de senha do link; `TestTOTPFlow` cobre senha na ativação e a recusa de recadastro com 2FA ativo. Qualquer mudança em `vfs`, `handlers_files.go` ou `handlers_shares.go` exige esses testes verdes.
