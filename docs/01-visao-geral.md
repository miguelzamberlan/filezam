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
- Compartilhar uma **pasta ou um arquivo** por link público somente leitura com prazo de validade, senha opcional e revogação. O endereço pode ser um apelido escolhido pelo usuário (`/s/orcamento-2026`), e nesse caso a senha é obrigatória.
- Receber arquivos de quem não tem conta, por um link público de envio apontado para uma pasta vazia e dedicada, com cota e vencimento obrigatórios; o dono é avisado na tela **Notificações** quando algo chega.
- Notificações por usuário (sino no menu), genéricas: hoje avisam de arquivos recebidos por link e de links revogados porque o item sumiu.
- O administrador liga, desliga e limita esses dois recursos numa tela de configurações globais.
- Lixeira com retenção configurável pelo administrador e restauração ao local original.
- Pesquisa por nome em todas as subpastas do escopo, sem diferenciar maiúsculas, acentos nem cedilha, servida por um índice em SQLite atualizado por varreduras periódicas e pelas próprias operações do app.
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

Copiar/mover/excluir/extrair/compactar rodam em goroutines do próprio processo; o handler espera até 300 ms ("sync-or-job") e devolve o snapshot. O cliente consulta a cada 500 ms enquanto `running`. SSE foi descartado por exigir configuração de proxy (`proxy_buffering off`) e manter conexões abertas.

**Não há servidor de filas, e não deve haver.** O produto é um binário estático com SQLite e nenhuma dependência externa; um Redis ou RabbitMQ seria um segundo container, um segundo ponto de falha e um segundo backup, para resolver problemas que não existem aqui: não há trabalho a distribuir entre máquinas (uma instância, um HD), não há produtor externo a desacoplar, e repetir sozinho uma operação de arquivo que falhou é mais perigoso que útil. Durabilidade, o único ganho real, o SQLite já daria.

Consequência assumida: **um reinício mata as operações em andamento**. O desligamento limpo espera até 30 s (`Shutdown` → `jobs.Wait`) e depois cancela; na subida seguinte o que ficou `running` vira `failed` com "interrupted by server restart". Retomar foi descartado de propósito; o que não se aceita é sobra com cara de arquivo legítimo. Cada arquivo copiado é gravado num temporário oculto `.filezam-copy-<job>-…` ao lado do destino e só ganha o nome no fim (hardlink sem sobrescrever, ou `rename` quando é para substituir — o original não é destruído antes de a cópia nova estar inteira). Antes de tocar o disco, o job grava em `job_cleanup` o que precisaria desfazer — os temporários com a sua marca sob cada destino de cópia ou movimento, a pasta de uma extração, o `.zip` temporário de uma compactação — e apaga as linhas ao terminar sozinho. A manutenção (na subida e de hora em hora) consome as linhas de jobs que não estão mais rodando: apaga o que foi registrado, mantém os arquivos que terminaram e troca o texto do histórico pelo que aconteceu (cópia de um arquivo: nada ficou no destino; de vários: cópia parcial, os completos ficaram). Um destino cuja pasta-mãe não existe (disco ainda não montado) espera a rodada seguinte. Mover registra só temporários, nunca a árvore: depois de um `rename`, o destino é o único lugar onde o item existe. Cancelar faz o mesmo na hora e deixa o aviso de cópia parcial no próprio job.

### ADR-9 · Links públicos por caminho, token com hash

O share guarda o caminho base-relativo e o SHA-256 do token (256 bits). Excluir, mover ou renomear o item pelo app revoga o link (e os links de tudo que estiver dentro dele); para mudanças feitas por fora, o `dev`/`inode` gravado na criação faz o link parar de responder. Seguir o item para o novo caminho foi descartado: o link publicaria um lugar que quem o criou não escolheu. A resposta para token inexistente, expirado ou revogado é sempre o mesmo 404.

### ADR-10 · Container distroless sem root e sem `chown` no entrypoint

Não há shell na imagem; o healthcheck é o subcomando `healthcheck` do próprio binário. O app roda como `PUID:PGID` e testa a gravação em `/data` e `/config` na inicialização. Como o Docker cria bind mounts inexistentes como `root`, o compose traz um serviço `init` (Alpine, executa uma vez) que ajusta `/config` e, só se vazia, `/data`.

### ADR-11 · Banco fora da raiz de dados

`FILEZAM_DATA_DIR` não pode ficar dentro de `FILEZAM_ROOT` (verificado no `config.Load`). Do contrário o próprio gestor exibiria e permitiria baixar `filezam.db`, que contém hashes de senhas, sessões e tokens.

### ADR-12 · Interface em pt-BR num único arquivo de strings

Todos os textos ficam em `web/src/i18n/pt-BR.ts` (referência), `web/src/i18n/en.ts` e `web/src/i18n/es.ts`, incluindo a tradução dos códigos de erro da API; `strings.ts` expõe o objeto `S` do idioma ativo (preferência `auto`/`pt-BR`/`en`/`es`). Um idioma novo é um arquivo em `i18n/` com as mesmas chaves, sem tocar nos componentes.

### ADR-13 · Apelido de link com senha obrigatória, e que não volta ao pool

Um endereço escolhido pelo usuário é adivinhável, ao contrário do token de 256 bits, então o sigilo passa a morar na senha, que vira obrigatória — em vez de embutir um sufixo aleatório, que devolveria um endereço que ninguém consegue ditar por telefone, que era o motivo do recurso. Contra varredura: link travado não revela sequer o nome do item, e só a tentativa **errada** consome o balde de 30/min por IP. Revogar um link com apelido marca `revoked_at` em vez de apagar a linha: liberar o endereço deixaria outra pessoa assumir um link já divulgado e passar a receber o que era destinado a quem o criou. O dono libera de propósito com `?purge=1`.

### ADR-14 · Link de envio como um modo de `shares`, não uma entidade nova

O link de recebimento (`mode = drop`) reaproveita dez pontos já prontos e testados: resolução de token com rate limit e filtros de expirado/revogado/dono desativado, o sandbox `base.Sub` com conferência de `dev`/`inode`, a senha Argon2 com cookie HMAC, a revogação ao mover/apagar/renomear, a revogação ao estreitar escopo, o CASCADE do usuário, a tela **Compartilhamentos**, a montagem da URL, o mascaramento de token no log e a rota `/s/:token`. Uma tabela paralela duplicaria os dez. Só os recibos ganham tabela própria (`share_uploads`), por terem cardinalidade e ciclo de vida diferentes — e são a fonte única da cota, em vez de contadores denormalizados que precisariam ser mantidos em sincronia sob corrida.

Consequência assumida: escrita anônima passa a existir no produto. Ela nasce desligada, exige cota e vencimento de no máximo 30 dias, aponta só para pasta vazia criada no ato, nunca sobrescreve e não permite leitura nenhuma.

### ADR-15 · Fila de envio própria na página pública

A página pública usa `upload/dropUploader.ts`, e não o `UploadManager`. O gerenciador autenticado carrega lote multipart, diálogo de conflito, caminhada de pastas, retomada de sessões pendentes e chamadas fixas a `/api/uploads` — tudo o que o link público **não deve** oferecer — e é um singleton configurado dentro de `<Protected>`. Injetar endpoints nele manteria 100% desse código e acrescentaria indireção; a fila própria tem cerca de 150 linhas e é testável isoladamente.

### ADR-16 · Configurações globais no banco, não em variável de ambiente

Os interruptores e os tetos dos links de envio ficam na tabela `settings`, editáveis pelo admin sem reiniciar o serviço — é ele quem decide, na operação, se a instalação aceita escrita anônima e com que limites. Isso não amplia o que um admin comprometido consegue fazer, já que ele podia alterar escopo e cota de qualquer conta. O que não é configurável é o teto de 30 dias do vencimento: está no código e o painel só encurta.

### ADR-17 · Extrair para pasta nova, sem nunca confiar no arquivo

A pasta de destino é criada na hora e a extração roda num `os.Root` aninhado nela: um erro de lógica
no tratamento de caminho não alcança nem o resto do escopo. Nada declarado pelo arquivo é honrado —
nem o modo (que traria setuid), nem o tamanho descomprimido (que é a bomba), nem o tipo quando não é
arquivo regular ou diretório (que traria symlinks). O nome da entrada é normalizado sozinho antes de
ser juntado ao destino, porque a ordem inversa colapsaria `..` contra a pasta de destino em silêncio.

Extrair "na pasta atual" foi descartado: obrigaria a resolver colisão com o que o usuário já tem, e é
justamente a classe de problema que a pasta nova elimina de saída.

### ADR-18 · Miniaturas sob demanda, com cache em disco

Servir a imagem original reduzida por CSS baixaria uma pasta inteira de fotos (200 × 5 MB = 1 GB)
para mostrar oito quadradinhos. Gerar tudo antecipadamente no indexador gastaria CPU com o que
ninguém vai olhar e transformaria o primeiro scan de um HD cheio de fotos numa operação de horas —
o walk de hoje é `lstat`-only e barato, e abrir e decodificar arquivos ali mudaria essa natureza.

Sobra gerar sob demanda: só o que a virtualização da listagem mostra, guardado em disco no
`DataDir` com a chave derivada de `(dev, ino, mtime, tamanho)`, que dá invalidação automática. A
resposta é a única do produto que o navegador guarda, e isso só é seguro porque a URL carrega o
`mtime`. O recurso é desligável pelo administrador e por cada usuário, porque velocidade é a
prioridade declarada do projeto e nem toda instalação quer pagar essa CPU.

### ADR-19 · Metadados de mídia lidos do contêiner, sem ffmpeg

Saber a duração de um vídeo, a resolução de uma foto ou quantos arquivos em 4K há numa pasta é
pergunta de quem trabalha com imagem, e a resposta óbvia seria chamar o `ffprobe`. Isso quebraria as
duas premissas do produto: o binário único sem CGO e a imagem distroless, que não tem shell nem
gerenciador de pacotes para hospedar um executável de 70 MB e a superfície dele.

A alternativa adotada é ler o **cabeçalho do contêiner** em Go puro (`internal/media`): ISO base
media (MP4, MOV, M4A, 3GP, HEIC, AVIF), Matroska/WebM, RIFF (AVI, WAV) e TIFF/EXIF (JPEG e os RAW
que são TIFF por baixo). Nenhum quadro é decodificado e nenhum pixel é reconstruído — só se
percorrem as caixas, os elementos e os IFDs que declaram resolução, duração, codec, taxa de quadros
e dados da câmera. Os números foram conferidos contra `ffprobe` e `exiftool` num acervo real.

O preço é o que esse caminho não alcança e fica declarado: **qualidade percebida** (VMAF, PSNR) não
sai de cabeçalho nenhum, e formatos cujo metadado só existe no fluxo — MTS/M2TS, MXF, R3D, BRAW —
ficam de fora, contados como "não foi possível ler" em vez de sumirem da conta. A resolução
informada é a **codificada**, com a rotação declarada já aplicada: é o número que um reprodutor
mostra, e não a dimensão de exibição anamórfica do `tkhd`.

Ler o cabeçalho é barato por arquivo e caro por pasta (uma abertura e alguns saltos de disco por
item). Por isso cada leitura fica numa tabela de cache chaveada por caminho + tamanho + `mtime`, e
uma análise com muita leitura nova vira job em segundo plano em vez de segurar a requisição.

### ADR: licença AGPL-3.0-only com opção comercial

Até a 1.2.0 o Filezam foi MIT. A partir da versão seguinte é **AGPL-3.0-only**, em licença dual com uma licença comercial negociada pelo autor. A AGPL fecha a brecha que a GPL deixa para software usado pela rede: quem modifica o Filezam e o oferece como serviço precisa abrir o código das modificações a quem o usa, então ninguém fecha uma versão melhorada do projeto para vender como SaaS. A variante *only* deixa com o titular a decisão de adotar uma AGPL futura. A licença dual depende de o autor poder distribuir todo o código nos dois regimes, por isso o `CONTRIBUTING.md` pede, a cada PR, uma licença para relicenciar a contribuição.

Os termos adicionais da seção 7 (`NOTICE.md`) são o que a AGPL permite acrescentar: preservar avisos legais e a atribuição de autoria (7(b)), marcar versões modificadas (7(c)) e não usar o nome para sugerir endosso (7(e)). Por isso o crédito da interface é apresentado como *Avisos Legais Apropriados* (versão, autor, licença, link do código-fonte) e não como propaganda: uma exigência fora da seção 7 seria uma "restrição adicional" que quem recebe o código pode simplesmente remover. Não há verificação técnica que impeça tirar o crédito num fork (qualquer uma seria removível no próprio código); a proteção é jurídica, e o link **Código-fonte** também é o meio de cumprir a seção 13.
