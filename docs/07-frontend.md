# 07 · Frontend

Projeto Vite em `web/`, React 19 + TypeScript estrito + Tailwind CSS 4. Saída do build em `internal/server/webdist/dist`, embutida no binário.

## Estrutura

```
web/src/
  main.tsx            QueryClientProvider + BrowserRouter
  App.tsx             Rotas e guarda de autenticação (Protected)
  hooks.ts            useAuth, useConfig, useListing, useInvalidateDirs, useFavorites
  strings.ts          Objeto S com os textos do idioma ativo; i18n/pt-BR.ts (referência), i18n/en.ts e i18n/es.ts
  index.css           Tailwind + utilitários próprios (@utility btn, input, card, menu…)
  api/client.ts       fetch tipado (X-Filezam, 401 → /login), ApiError, objeto Api
  api/types.ts        Tipos espelhando docs/04
  lib/                paths (join/dirname/encode/uniqueName), naturalSort, format,
                      clipboard (copyText com fallback), share (shareLink), fold (comparação sem
                      acentos), about (nome, versão, autor, licença e URL do código-fonte),
                      updates (versão do servidor vista no header + travas do reload automático)
  store/ui.ts         zustand: seleção, âncora, foco, clipboard, ordenação, visão, filtro
  store/jobs.ts       zustand: jobs acompanhados pelos toasts
  upload/manager.ts   UploadManager (sem React) — fila, modos, slots, retries, conflitos, retomada
  upload/dropUploader.ts  Fila do link público de recebimento — sem pastas, lote, sobrescrita nem retomada
  upload/scheduler.ts funções puras (classify, pickBatch, backoffMs, chunkRange) — testadas
  upload/walk.ts      travessia de DataTransfer/FileList
  upload/xhr.ts       XMLHttpRequest com progresso → Promise
  components/         Shell, FileList, Breadcrumb, Toolbar (dentro de Browser), ContextMenu,
                      Preview, Markdown (lazy), ShareDialog, MoveDialog (seletor de pasta de destino),
                      InfoDialog (propriedades), MediaStats (análise de foto e vídeo), Preferences, DiskBar,
                      UploadPanel, JobToasts, dialogs (prompt/confirm/conflict/toast), Icons
  pages/              Login, ChangePassword, Browser, Shares, AdminUsers, AdminAudit, AdminSettings, PublicShare, PublicDrop
  components/Editor.tsx   Editor de texto e markdown (fora do Preview, ver abaixo)
```

## Rotas

| Rota | Página | Guarda |
|---|---|---|
| `/login` | Login | — |
| `/change-password` | ChangePassword (troca obrigatória) | sessão |
| `/setup-2fa` | Setup2FA (cadastro obrigatório para admins com `require2fa`; fora do Shell) | sessão |
| `/b/*` | Browser (`*` = caminho escopo-relativo, cada segmento `encodeURIComponent`) | sessão + senha em dia |
| `/search?path=&q=` | Search (pesquisa recursiva por nome a partir de `path`; resultados levam a `/b/<pasta>?sel=<nome>`, que seleciona o item) | sessão + senha em dia |
| `/trash` | Trash (itens excluídos: restaurar, excluir de vez, esvaziar) | idem |
| `/account` | Account, "Minha conta": tudo o que é da pessoa, em duas abas com a escolhida em `?aba=` — **Preferências** (`components/Preferences.tsx`: geral, idioma, tema, visualização, zoom, cores) e **Senha e segurança** (trocar senha; 2FA: ativar com `TotpSetup`, desativar, novos códigos). Quem precisa ativar o 2FA abre direto na segunda. Entrada pelo cartão do usuário no rodapé do menu, ao lado do botão Sair | idem |
| `/notifications` | Notifications (lista de `/api/notifications` com o texto montado por `kind` + `data` no idioma ativo; "Abrir pasta" navega e marca como lida, a página em si não marca nada; contagem de não lidas em `useUnreadNotifications`, polling de 30 s só com a aba visível, que alimenta o contador no menu, o ponto no `MenuButton` e um toast quando a contagem sobe) | idem |
| `/jobs` | Jobs ("Operações": em andamento com progresso e cancelamento + histórico de 30 dias vindo de `/api/jobs/history`; polling de 1 s só enquanto há job rodando) | idem |
| `/shares`, `/admin/dashboard`, `/admin/users`, `/admin/audit` | idem | idem (admin para `/admin/*`; a API também valida) |
| `/s/:token/*` | PublicShare (ramifica para PublicDrop quando `mode: drop`) | — |

`Protected` redireciona para `/login` sem sessão, para `/change-password` quando `mustChangePassword` e para `/setup-2fa` quando `totpRequired`. Login em duas etapas: `Api.login` pode devolver `{totpRequired, token}`; a tela troca para o campo de código (app ou recuperação) com "Confiar neste dispositivo por 30 dias" e chama `Api.loginTOTP`. `TotpSetup` gera o QR com a biblioteca `qrcode` como `data:` URL numa `<img>` (nada de SVG injetado).

## Estado

- **Servidor** (TanStack Query): `['me']`, `['config']` (staleTime ∞), `['list', path, sortKey, sortDir, showHidden]` (`useInfiniteQuery`, páginas de `LIST_PAGE` = 2000 entradas via `GET /api/files?limit=&offset=&sort=&dir=&hidden=`; `useInvalidateDirs` invalida pelo prefixo `['list', path]`), `['favorites']`, `['shares']`, `['dirs', path]` (seletor de destino do Mover), `['admin-users']`, `['admin-dirs', path]`, `['settings']`, `['job', id]` (refetch 500 ms enquanto `running`), `['public', token, ...]`. Mutações invalidam as chaves afetadas; `useInvalidateDirs(dirs)` é o ponto único para listagens.
- **UI** (zustand `useUI`): `selection: Set<string>` de nomes na pasta atual, `anchor`/`focused` para Shift/teclado, `clipboard {op: copy|cut, dir, names}`, `sort`, `view`, `filter` e `prefs` (persistidos em `localStorage`: `sort`, `view`, `prefs`).
- **Preferências** (`prefs`, editadas na aba Preferências de Minha conta): `showHidden` (arquivos iniciados por ponto; o servidor sempre os lista, o filtro é no cliente e o rodapé mostra "N ocultos"), `showHints` (dicas de atalhos), `confirmDelete`, `zoom` (fator da listagem, passos em `ZOOM_STEPS`; botões −/%/+ na barra do Browser e seleção em Minha conta). `lang` e `theme` (`system`/`light`/`dark`; `Preferences` usa subcomponentes definidos fora dele, senão o `<input type="color">` seria remontado a cada mudança e o seletor nativo fecharia no meio do arraste: `lib/theme.ts` põe a classe `.dark` no `<html>` e `color-scheme`; o Tailwind usa `@custom-variant dark (&:where(.dark, .dark *))`, nunca a media query direto) e `accent`/`selection`/`focus` (hex; viram as variáveis `--accent`/`--selection`/`--focus` no `<html>`, expostas como `bg-accent`, `text-accent`, `ring-focus` etc. via `@theme inline`; `row-selected`, `nav-active` e `btn-primary` derivam tons com `color-mix`, então uma cor serve para os dois temas). `uploadPanelOpen` e `sidebarOpen` (não persistidos) controlam a lista do painel de envios e o menu lateral em telas estreitas; o item **Uploads** do menu lateral expande o painel ou, sem envios, explica como enviar. Novas preferências: adicionar em `Prefs`/`defaultPrefs` em `store/ui.ts`, uma linha em `components/Preferences.tsx` e o texto em `i18n/pt-BR.ts` e `i18n/en.ts`.
- **Uploads**: fora do React; `useUploads()` lê o snapshot via `useSyncExternalStore`.

## Página Browser (`pages/Browser.tsx`)

Concentra as ações. Convenções:

- Seleção: clique = única; Ctrl/Cmd = alternar; Shift = intervalo desde a âncora; clique no fundo limpa.
- Abrir: pasta navega; arquivo com preview abre o modal; senão baixa por `<a download>` invisível.
- Baixar pasta ou seleção em zip passa por `downloadZip` (`components/ZipParts.tsx`): pede `zip/plan`; com uma parte só, dispara o zip direto; com mais, abre um diálogo com uma linha por parte (`<a download>` com `from`/`to` e o nome "pasta (parte i de n)"), marcando as já clicadas. Toast "Preparando o download…" se o plano passar de 700 ms; `timeout` do plano cai no zip único. O mesmo helper serve ao `PublicShare`.
- Colar: se algum nome já existe no destino, pergunta uma vez (`dialogs.conflict`) e envia a política à API; `cut` limpa o clipboard após o job.
- Soltar arquivos: no fundo → pasta atual; sobre uma linha de pasta → dentro dela (destaque azul). Tipos sem `Files` são ignorados.
- Arrastar e soltar interno: linhas são `draggable`; o `dataTransfer` leva `application/x-filezam` (JSON com os nomes; a seleção inteira se o item arrastado estiver nela). Pastas da listagem e os ancestrais da trilha aceitam o drop e chamam `moveInto(dest, names)`, que lista o destino, pergunta uma vez em caso de conflito (`dialogs.conflict`) e dispara `Api.move`. Soltar sobre um item da própria seleção ou dentro dele mesmo é recusado. No toque não há arraste: use recortar/colar.
- **Mover para…** (`components/MoveDialog.tsx`): a seleção escolhe o destino num seletor que navega só por pastas (`GET /api/files/dirs`, uma consulta `['dirs', dir]` por pasta visitada), começando na pasta de origem — o destino mais comum é uma irmã, a um clique de "subir". As pastas que estão sendo movidas aparecem desabilitadas: entrar nelas só levaria a um destino que o servidor recusaria (`nested`). "Nova pasta" é um campo embutido no próprio diálogo, não um `dialogs.prompt` por cima (dois modais empilhados dividiriam o Esc); com ele aberto, Esc e o clique fora cancelam só o campo. Escolhido o destino, o mesmo `moveInto(dest, names)` do arrastar e soltar toma conta do resto: conflito de nome, aviso de link que cai e o job de move. É o caminho do celular e do teclado para o que o arraste faz com o mouse.
- Listagem paginada: o servidor filtra ocultos e ordena; a página seguinte é pedida quando a rolagem virtual chega a 20 linhas do fim (`onEndReached`). Enquanto faltam páginas a ordem do servidor é mantida (sem `sortEntries` no cliente) e o rodapé mostra "N de total carregados"; o filtro da pasta só peneira o que já foi carregado. O filtro e a digitação que salta para o prefixo comparam por `lib/fold.ts` (espelho de `vfs.Fold`: sem maiúsculas, acentos e cedilha), como a pesquisa do servidor.
- Excluir: com `config.trashRetention > 0` o item vai para a lixeira (confirmação leve, respeitando `confirmDelete`); `Shift+Del` ou "Excluir de vez" no menu apagam permanentemente (confirmação sempre, com a caixa `requireCheck` de `dialogs.confirm`, que começa desmarcada e libera o botão; o mesmo vale para excluir de vez/esvaziar na Lixeira). Compartilhar aceita pasta ou arquivo (`ShareDialog` recebe `kind`) e senha opcional.
- Erros da API viram toast com `errorMessage(code)`.

Atalhos: `↑ ↓ Home End PgUp PgDn` (com Shift estende), `→ ←` na grade, `Enter`, `Backspace`/`Alt+↑`, `Esc`, `F2`, `Delete`, `Espaço` alterna, `Ctrl+A/C/X/V`, `Ctrl+Shift+N`, digitação salta para o prefixo (buffer de 700 ms). O handler ignora eventos vindos de inputs e quando há menu/preview/diálogo aberto. Ele mora no `onKeyDown` da lista, que só ouve com o foco dentro dela; como fechar o preview pelo X, confirmar um diálogo ou um PDF que tomou o foco no iframe removem o elemento focado e deixam o foco no `<body>`, um listener no `window` cobre esse caso: tecla com o foco no `body` e nada aberto por cima (nem `[aria-modal]`) devolve o foco à lista e é tratada ali. Sem isso, o item continuava selecionado na tela e `Delete` não fazia nada. Excluir, renomear, colar um recorte e arrastar para mover consultam `Api.sharesAffected` antes de agir: com link público no item (ou abaixo dele), a confirmação lista os caminhos afetados (agrupados, com o tipo do link) e avisa que o link não volta — na exclusão o aviso entra no próprio diálogo e aparece mesmo com a confirmação da lixeira desligada; nos outros, é um diálogo só quando há link. `Ctrl+A` com páginas ainda por vir seleciona o que já chegou e mostra um aviso de 10 s com `n de total` e o caminho para o resto (rolar até o fim, ou Esc + **Baixar** para a pasta inteira) — carregar todas as páginas de uma vez travaria pastas enormes.

## Idiomas (`strings.ts`, `i18n/`)

`i18n/pt-BR.ts` é a referência; `i18n/en.ts` e `i18n/es.ts` são tipados como `Strings = typeof ptBR`, então uma chave faltando quebra o `tsc`. `S` é um objeto mutável: `applyLocale()` (chamado em `main.tsx` antes do primeiro render, com `resolveLocale(prefs.lang)`: `auto` segue `navigator.language`, `pt*` → pt-BR, `es*` → es, senão en) copia o idioma para dentro dele e ajusta `<html lang>`. Componentes leem `S.chave` no render; constantes de módulo (ex.: `OPTIONS` do `ShareDialog`) só mudam com recarga, por isso trocar o idioma em Configurações recarrega a página. Datas relativas usam `S.relAgo`/`S.relIn`; `formatDate` usa o locale do navegador. Para adicionar um idioma: novo arquivo em `i18n/`, entrada em `LOCALES`/`LOCALE_NAMES` e no tipo `Locale`.

## Créditos e versão (`components/Credits.tsx`, `lib/about.ts`)

`Credits` mostra "Powered by Filezam vX.Y.Z · Desenvolvido originalmente por Miguel Zamberlan", a licença (AGPL-3.0, link para o texto da GNU) e **Código-fonte** (link para `APP.sourceUrl`). São os Avisos Legais Apropriados da AGPLv3 e a atribuição exigida pelo `NOTICE.md`: não se removem, e uma versão modificada troca só `sourceUrl` pelo endereço do próprio código (seção 13). Aparece em três lugares: no pé do menu lateral, abaixo do cartão da conta (`stacked`, uma informação por linha para caber nos 240 px), e numa faixa fixa embaixo das telas sem menu — login, troca de senha, 2FA e links públicos (leitura e recebimento) —, pelo `WithCredits` em `App.tsx`. Texto de 11 px em cinza, sem cor de destaque.

A versão da interface tem uma fonte só, `web/package.json`: o `vite.config.ts` lê o número e o injeta como `__APP_VERSION__` (`define`, declarado em `globals.d.ts`), que `lib/about.ts` expõe como `APP.version`. O binário recebe a tag por `-X main.version` e a release recusa a tag se o `package.json` disser outro número, então os dois nunca divergem numa versão publicada. O mesmo `vite.config.ts` põe no topo do JS de entrada o comentário `/*! Filezam vX.Y.Z | (C) 2026 Miguel Zamberlan | AGPL-3.0-only | <repositório> */`, no `generateBundle` — o `output.banner` do Rollup passa antes do minificador, que o descartava.

## Atualização do servidor com a aba aberta

`index.html` sai com `Cache-Control: no-store` e os assets têm hash no nome (`immutable`), então **recarregar já troca o bundle inteiro** — não há cache a furar, e o que fica no `localStorage` são as preferências, válidas em qualquer versão. Falta só a aba saber que precisa recarregar.

Toda resposta traz `X-Filezam-Version` (ver [04](04-api.md#versão-e-atualização)). O `api()` em `api/client.ts` passa o valor para `useUpdate.note()` (`lib/updates.ts`), antes do tratamento de erro — depois de uma atualização é justamente numa resposta de erro que a aba velha tende a esbarrar. Diferente da versão do próprio bundle, `components/UpdateBanner.tsx` mostra a faixa "O Filezam foi atualizado para a versão X.Y.Z · Recarregar", empilhada com os toasts no mesmo contêiner (`ToastHost`) para nunca se cobrirem. Nenhuma requisição existe só para isso: a consulta de não lidas (30 s com a aba visível) faz até uma aba parada descobrir sozinha. `dev` de um dos lados desliga a comparação — no `make dev` o binário se diz `dev` e o bundle traz o número do `package.json`.

**Recarregar sozinho só quando nada se perde.** A faixa reconfere a cada 15 s e em cada `visibilitychange`, e só chama `location.reload()` com a aba **escondida** e sem nenhuma trava: quem está olhando decide a hora, quem saiu volta já na versão nova. As travas são `useReloadHold(active)` (`lib/updates.ts`, um contador simples), usadas em:

| Onde | Enquanto |
|---|---|
| `UploadPanel` | há envio na fila ou em andamento, ou sobraram itens que falharam na lista |
| `PublicDrop` | há envio na fila ou em andamento |
| `Editor` | estiver aberto, mesmo sem alterações |
| `DialogHost` | houver diálogo na pilha |
| `Browser` | houver recortar/copiar esperando colar |

É de propósito mais abrangente que o `beforeunload`, que só cobre perda de dados: fechar um editor limpo não perde nada, mas ninguém quer voltar à aba e encontrá-lo fechado. Uma tela nova com trabalho em andamento só precisa chamar `useReloadHold`.

Uma tentativa por versão anunciada, registrada em `sessionStorage` (`filezam.autoReloadedTo`, que sobrevive à recarga e vale só naquela aba): se recarregar não resolver — um binário publicado com número diferente do bundle que ele embute, um proxy servindo assets de outra versão —, sem isso a aba escondida recarregaria de 15 em 15 segundos para sempre. A faixa fica na tela para a recarga manual, e uma atualização posterior, com outro número, volta a valer. Sem `sessionStorage` disponível não há recarga automática.

## Celular e toque

- Menu lateral vira gaveta abaixo de `lg`; cada página coloca `<MenuButton />` (de `Shell.tsx`) na sua primeira linha em vez de gastar uma linha só com o botão.
- Browser: a barra de ações completa só aparece a partir de `sm`; abaixo disso há uma linha compacta (nova pasta, enviar, filtro, colar quando há área de transferência e **⋯** que abre o mesmo menu de contexto da pasta ou da seleção). Zoom só na barra ≥ `sm` e em Configurações.
- Toque (`pointerType === 'touch'` no `click`): um toque **abre** pasta/arquivo; toque longo (~600 ms sem mover, implementado em `FileList` com pointer events, para o iOS que não dispara `contextmenu`; no Android o evento nativo cancela o timer) abre o menu de contexto e seleciona o item; com uma seleção ativa, toques alternam itens e a linha compacta mostra "N selecionados ✕" para limpar. Mouse mantém clique = selecionar, duplo clique = abrir.
- Tabelas (Pesquisar, Compartilhamentos, Usuários, Auditoria) escondem colunas secundárias abaixo de `sm`/`md` e os botões ficam só com ícone.
- `height: 100dvh` quando suportado e `touch-action: manipulation` nos botões.

## FileList

Colunas da visão em lista: ícone, Nome, Tipo, Tamanho e Modificado; Tipo e Modificado se escondem abaixo de `sm`. Todas ordenam pelo cabeçalho (`Sort` em `store/ui.ts`, persistido). A coluna Tipo é a extensão em maiúsculas (`typeLabel` em `lib/format.ts`: `PDF`, `JPG`; pasta e arquivo sem extensão ganham palavra), que é exatamente o que a ordenação por tipo agrupa — no cliente (`sortEntries`) e no servidor (`vfs.SortEntries`, `sort=type`), sempre com as pastas primeiro e o desempate por nome em ordem crescente.

Virtualizada com `@tanstack/react-virtual` (34 px por linha; grade com colunas calculadas por `ResizeObserver`). A prop `zoom` aplica CSS `zoom` ao contêiner de rolagem: o virtualizador trabalha nas coordenadas já ampliadas (`scrollTop`/`clientHeight` do próprio elemento), então os tamanhos estimados não mudam; validado no Chrome rolando até a última linha a 150 %. Recebe entradas já ordenadas (`sortEntries`: pastas primeiro, `Intl.Collator` numérico) e filtradas. É a mesma para o browser autenticado e a página pública (`readOnly`). `focused` é rolado para a vista.

## Ícones (`components/Icons.tsx`)

`iconFor(name, type)` escolhe por extensão em `EXT_ICONS`: imagem, vídeo, áudio, arquivo compactado, PDF/DOC/XLS/PPT (contorno de arquivo com a sigla e cor por família), texto e código; qualquer outra extensão cai no ícone genérico. Para acrescentar um tipo, adicione a extensão à lista certa ou crie um `IDoc('SIGLA')`.

## Página Search (`pages/Search.tsx`)

Estado na URL (`?path=&q=`), consulta `['search', path, q]` (staleTime 30 s). Mostra de onde veio a resposta (`source`: índice com data da última varredura, ou disco) e, para admin, o botão "Reconstruir índice" (`POST /api/admin/reindex`). Filtra localmente itens ocultos (nome ou pasta iniciados por ponto) conforme `prefs.showHidden`. Aviso quando `partial`. O botão de lupa na barra do Browser abre a pesquisa já a partir da pasta atual.

## DiskBar e cota

`DiskBar` mostra a cota do usuário (`quota`/`quotaUsed` de `/api/files/disk`) quando existe, senão o disco do escopo. No formulário de usuário o admin informa a cota em GB (convertida para bytes; vazio = sem limite). `quota_exceeded` (507) é traduzido como os demais códigos.

## Página Trash (`pages/Trash.tsx`) e PublicShare

Trash: consulta `['trash']`, seleção por checkbox, ações restaurar/excluir de vez/esvaziar (`dialogs.confirm` nas permanentes), invalida `['list']`, `['disk']` e `['trash']`. PublicShare: `info.locked` mostra o formulário de senha (`POST unlock` grava o cookie; depois invalida `['public', token]`); `kind: file` mostra um cartão com download e preview em vez da listagem. Travado, a resposta não traz o nome, então o cabeçalho cai em `S.appName`.

## Miniaturas (`components/Thumb.tsx`)

O ícone por tipo é renderizado sempre e a miniatura entra por cima quando carrega: a listagem não
"pula" enquanto as imagens chegam, e qualquer falha (formato sem decodificador, imagem grande
demais, recurso desligado) simplesmente deixa o ícone à mostra, sem estado de erro na interface.

`loading="lazy"` mais a virtualização do `FileList` fazem com que só o que está na tela seja
pedido — rolar uma pasta com milhares de fotos não dispara milhares de requisições. A URL carrega o
`mtime`, então na segunda visita o navegador serve do próprio cache sem tocar no servidor.

As miniaturas só são ligadas no navegador autenticado (`thumbs` + `path` no `FileList`); o
`PublicShare` não as usa. Além do interruptor do administrador, cada pessoa pode desligar em
**Preferências**, para quem prefere a listagem mais enxuta possível.

## Análise de mídia (`components/MediaStats.tsx`)

**Analisar mídia** aparece no menu de contexto em três situações: sobre a área vazia (analisa a
pasta atual), sobre um arquivo e sobre uma seleção. O alvo vira uma lista de caminhos; quando é uma
pasta única, o diálogo também oferece **Exportar planilha** (`/api/files/media/csv`), que só faz
sentido com um caminho.

O servidor conta e a interface diz: cada faixa vem com um identificador estável (`4k`, `1080p`,
`portrait`, `mp24`) traduzido por `S.mediaRes`/`S.mediaMp`/`S.mediaShape`, e o que é texto do
próprio arquivo (codec, formato, câmera, `3840×2160`) aparece como veio. É o mesmo desenho das
notificações, pela mesma razão: o idioma é de quem lê, não de quem gravou.

Quando a resposta vem `202` com um job, o diálogo acompanha o progresso por `Api.job(id)` e, ao
terminar, incrementa uma chave da consulta para refazer o pedido — que aí sai inteiro do cache do
servidor. É o único lugar da interface que trata um job como etapa de uma leitura, e não como uma
operação que o usuário pediu.

As barras são proporcionais ao **maior item da lista**, não ao total: com uma faixa dominante as
outras virariam um fio e deixariam de ser comparáveis entre si. A lista de tamanhos exatos mostra
dez linhas e resume o resto, porque a cauda de "1 · 5%" é ruído.

`mediaRows()` é exportado do mesmo arquivo e monta as linhas técnicas de um arquivo só; o
`InfoDialog` o usa para mostrar duração, codec e dados da câmera junto das propriedades, sem
requisição extra (o `info` já traz `media`). A data de captura é formatada em UTC de propósito:
EXIF e cabeçalho de vídeo gravam o relógio do equipamento sem fuso, e converter para o fuso de quem
está olhando deslocaria a hora que estava na câmera.

## Operações de arquivo compactado

**Extrair aqui** e **Compactar em .zip** no menu de contexto — o primeiro condicionado por extensão
(`isExtractable` em `lib/paths.ts`), que até então o menu não fazia com nenhum item. Os dois rodam
como job e reusam `track(job)` e o `JobToasts`.

O rótulo do job passou a sair de um mapa com fallback (`lib/jobs.ts`) em vez do ternário fechado que
existia em dois lugares: um tipo de operação novo aparecia silenciosamente como "Excluindo".

## Editor (`components/Editor.tsx`)

Edita os mesmos tipos de texto que o preview mostra, até `previewMaxText` (1 MiB) — acima disso a
opção fica desabilitada, porque salvar um texto truncado destruiria o arquivo. Para `.md`, prévia
ao lado reusando o `Markdown` (o mesmo chunk lazy do preview); em tela estreita, alterna.

**Vive fora do `Preview` de propósito.** O `Preview` registra `keydown` em *capture* no `window`
para navegar entre arquivos com as setas e fechar com `Escape` (`Preview.tsx:60-71`); dentro dele,
digitar num `textarea` seria impossível. O `Browser` também trata `editing` como os outros modais no
guard do `onKeyDown`.

Abre por: **Editar** no menu de contexto (`F4`), botão no cabeçalho do preview de texto, ou `F4` na
listagem. Não aparece no `PublicShare`, que é somente leitura.

Ao salvar manda o `mtime` que leu ao abrir (`ifMtime`); se o servidor responder 409 `modified`, o
texto digitado **permanece na tela** e o usuário é avisado de que alguém salvou antes — nada é
descartado automaticamente. `dialogs.confirm` ao fechar sujo, e `beforeunload` enquanto houver
alteração pendente.

## PublicDrop (`pages/PublicDrop.tsx`) e `upload/dropUploader.ts`

Página de um link de recebimento: barra de cota, contador de arquivos, zona de soltar, a fila do
envio e a lista dos **próprios** envios (`info.mine`, que vem do servidor pelo cookie do remetente).
Não lista nem baixa nada.

O envio usa uma fila própria, e não o `UploadManager`. O gerenciador autenticado tem 550 linhas de
lote multipart, diálogo de conflito, caminhada de pastas, retomada de sessões pendentes e
`Api.uploadList`/`uploadAbort` fixos — nada disso existe aqui, e ele é um singleton configurado
dentro de `<Protected>`. O `dropUploader` tem duas rotas (PUT único abaixo de `chunkSize`, sessão em
blocos acima), 2 arquivos em paralelo, `xhrSend` para progresso e `chunkRange` reaproveitado do
`scheduler`; erros do próprio link (`drop_full`, `drop_file_limit`, `drop_count_exceeded`,
`share_locked`) nunca entram em retry. Cada arquivo concluído invalida `['public', token]`, e é o
servidor que devolve cota, contagem e lista atualizadas.

Só arquivos soltos entram: uma pasta arrastada é ignorada, porque o link é plano e achatá-la só
geraria colisões de nome. A configuração do cliente (`chunkSize`, `maxParallel`, `maxFileBytes`) vem
do próprio `GET /api/public/{token}`, já que `/api/config` exige sessão.

## AdminDashboard (`pages/AdminDashboard.tsx`)

Primeiro item de Administração no menu. Lê `GET /api/admin/dashboard` com `refetchInterval` de 15 s. O react-query não busca com a aba em segundo plano, então a página aberta e esquecida não pesa. De cima para baixo:

- **Cartões**: ativos agora, enviando agora, operações, links no ar, disco (barra com as cores do `DiskBar`) e lixeira.
- **Atenção**: alertas calculados no cliente por `lib/dashboard.ts` (`dashboardAlerts`, com testes). Disco a partir de 80 % (aviso) e 90 % (perigo), cota a partir de 90 %, login bloqueado, links quebrados, falhas de login em 24 h, envio em blocos parado há 1 h e índice desligado ou em construção. Os dois últimos só aparecem quando alguém tem cota. O texto de cada alerta fica em `S.alert*`.
- **Em andamento**: envios por usuário ou link (ponto verde pulsando com requisição em curso), arquivos grandes em blocos com progresso e operações de todos.
- **Usuários**: último acesso ("ativo agora" até 10 min), sessões, uso e cota com barra, e links.
- **Sessões abertas**: navegador resumido por `lib/userAgent.ts` (`describeUserAgent`, com o texto original no `title`), IP e o botão **Encerrar**, com confirmação. A sessão atual mostra "esta sessão" no lugar do botão.
- **Atividade recente**: logins, falhas e bloqueios em 24 h, links criados e arquivos recebidos por links em 7 dias.

Tabelas largas rolam dentro do próprio cartão, e colunas secundárias somem em telas estreitas.

## AdminSettings (`pages/AdminSettings.tsx`)

Interruptores de endereço personalizado, link de recebimento, extração, miniaturas e análise de
mídia, mais os tetos de cada um. Cada
mudança é um `PATCH` imediato que invalida `['settings']` e `['config']` — a segunda porque
`ShareDialog` esconde o que estiver desligado.

## Diálogos e toasts (`components/dialogs.tsx`)

API imperativa baseada em Promises: `dialogs.prompt({title, initial, selectExt})`, `dialogs.confirm({danger})`, `dialogs.conflict(name, existing?)` → `{choice, all}` (com `existing` diferente de `name`, a mensagem diz com que nome o item já está lá),  `dialogs.custom(render)`. `DialogHost` renderiza a pilha; `Modal` fecha com Esc e clique fora. `toast(text, kind)`.

## Propriedades e disco

- Links públicos: `shareLink(token, publicUrl)` monta a URL a partir de `FILEZAM_PUBLIC_URL` (quando o operador definiu) ou de `window.location.origin` — nunca do host visto pelo servidor, que pode ser interno. `copyText` copia com fallback para `document.execCommand('copy')` em origens não seguras (http://) e avisa por toast quando não consegue. `ShareDialog` já copia o link ao criar; `Shares` e `InfoDialog` copiam de novo pelo `token` que a API devolve.
- `InfoDialog` (`GET /api/files/info`): caminho com botão copiar, tipo, tamanho, modificação; para pastas, conteúdo calculado (bytes, arquivos, pastas, aviso de contagem parcial), se está nos favoritos e os links públicos ativos daquela pasta com validade, número de acessos e último acesso. Abre pelo botão ⓘ da toolbar ou "Propriedades" no menu de contexto. O menu de contexto mostra o caminho completo do item como primeira linha (desabilitada).
- `DiskBar` (`GET /api/files/disk`, refetch a cada 60 s e após qualquer operação via `useInvalidateDirs`): barra de uso com cor por faixa (azul, âmbar > 75 %, vermelho > 90 %) e "X livres de Y" no rodapé da lateral.

## Preview

`previewKind(entry)` por extensão: imagem (`<img>`), vídeo/áudio (`<video>/<audio>` com `preload=metadata`, seeking via Range), PDF (`<iframe>` **sem** atributo `sandbox`: o Chrome bloqueia o visualizador de PDF em frames com sandbox; o isolamento vem da CSP `sandbox` que o servidor envia com o arquivo, ver [03](03-seguranca.md#cabeçalhos-http); em navegadores sem visualizador embutido — `navigator.pdfViewerEnabled === false`, UA Android/iPhone/iPad/iPod ou iPadOS, que se apresenta como Mac com toque — o preview mostra "Abrir em nova aba" (visualizador nativo do sistema) e "Baixar" no lugar do iframe, porque o Chrome do Android não desenha PDF em frame e o Safari do iOS só desenha a primeira página), texto (fetch com `Range: bytes=0-<previewMaxText>` em `<pre>`). Setas navegam entre os previewáveis da pasta.

**Imagem ao navegar com as setas** (`ImageView` em `Preview.tsx`). O `<img>` é montado com `key` pela URL: trocar só o `src` do mesmo elemento deixava a foto anterior na tela até a nova chegar, com o nome no topo já trocado, e a seta parecia não ter feito nada. Enquanto a original carrega, aparece por baixo a miniatura (`thumbFor`, a mesma URL de `Thumb.tsx`, que o navegador guarda por 5 min) desfocada, com um indicador de carregamento; a original entra com fade de 150 ms. Sem miniatura (recurso desligado, formato sem decodificador, link público), fica só o indicador. A imagem vizinha de cada lado é pré-carregada com `new Image()` **depois** que a atual termina — antes, as duas dividiriam a banda e a foto aberta demoraria o dobro numa conexão lenta. O conteúdo sai com `no-store`, mas o navegador reaproveita a imagem já baixada dentro do mesmo documento, então a vizinha aparece na hora e sem fade (`complete` conferido em `useLayoutEffect`, antes da pintura). O pré-carregamento não é cancelado ao trocar de foto: a vizinha que virou a atual é justamente o download que o `<img>` aproveita.

`.md`/`.markdown` abrem **formatados** por padrão; o botão "Texto"/"Formatado" no cabeçalho alterna para o `<pre>` (a escolha vale enquanto o preview está aberto). `components/Markdown.tsx` usa `react-markdown` + `remark-gfm` (tabelas, listas de tarefas, riscado, autolinks) e é importado com `React.lazy`, então o chunk (~48 KB gzip) só é baixado quando um Markdown é aberto. Estilos no `@utility markdown` de `index.css` (sem `@tailwindcss/typography`). Links `http(s)`/`mailto` abrem em nova aba com `noopener noreferrer`; caminhos relativos (imagens e links) são resolvidos contra a pasta do arquivo por `resolveRelative` (`lib/paths.ts`) e viram URLs de conteúdo inline via a prop `assetUrl` — `null` para absolutos (`/…`) ou que sobem acima da pasta, e a prop fica ausente no link público de arquivo, onde irmãos não são alcançáveis; âncoras viram texto. Segurança em [03](03-seguranca.md#conteúdo-enviado-por-usuários).

## UploadManager

Ver [05](05-uploads.md) para o protocolo. Estados de item: `queued → uploading → done | failed | cancelled | skipped | conflict`. Métodos públicos: `configure(cfg)`, `add(files, destDir)`, `pause/resume`, `cancel(id)`, `cancelAll`, `retryFailed`, `clearDone`, `loadPending/discardPending`, `resetConflictDefault`. Callbacks injetados pelo `Shell`: `onConflict`, `onDirChanged`, `onError`.

`nextWork()` prioriza blocos de arquivos chunked já em andamento, depois o primeiro item da fila (lote agrupado, único ou nova sessão). O painel mostra progresso total, velocidade, ETA, contadores, pausa, cancelar tudo, repetir falhos. Com itens falhos ou cancelados, aparece uma faixa com a caixa "Só os não enviados (N)" (filtra a lista virtualizada e se desfaz sozinha quando não sobra nenhum), copiar a lista (`copyText`) e baixar CSV. As funções puras ficam em `upload/report.ts`: `problemText` e `problemCsv`, este separado por `;` e com BOM para o Excel em português, e `downloadText`, que baixa via `Blob` sem passar pelo servidor. Enquanto houver item na fila ou enviando, inclusive pausado, `UploadPanel` registra `beforeunload`; `PublicDrop` faz o mesmo para o envio anônimo. Na lista, tamanho e status têm largura mínima e não encolhem; quem cede espaço é o nome (truncado), e a lista só rola na vertical — rótulos longos como "Cancelado" não criam rolagem horizontal.

A velocidade vive em `upload/speed.ts` (`SpeedMeter`, puro e testado): amostras em janelas de 1s suavizadas por EMA (`0.75 * anterior + 0.25 * instantânea`); `formatSpeed` a imprime sempre com duas casas decimais, deixando só a unidade mudar (B/s, KB/s, MB/s…), e o painel usa `tabular-nums` para o número não mudar de largura a cada atualização.

Duas correções de sinal ficam no medidor, porque o total enviado não é uma reta crescente. Quando ele **diminui** — cancelar um item, "Limpar concluídas", um pedaço que falhou e volta do zero — a janela só reancora, sem gerar amostra: tratar a diferença negativa como velocidade zerava o painel até o envio reconquistar os bytes perdidos, o que em arquivo de gigabytes leva minutos. E como uma EMA só encolhe, sem byte nenhum ela nunca chega a zero: uma parada longa deixava ligada uma velocidade infinitesimal, que imprimia `0,00 B/s` ao lado de uma previsão absurda (`2.16e+68h 37min restante`). Abaixo de 1 B/s a velocidade é zero, e aí o painel mostra `uploadWaiting` ("aguardando…") em vez de velocidade e ETA — que é também o que aparece no primeiro segundo, antes da primeira amostra. `formatDuration` tem o teto correspondente: acima de um dia mostra `3d 4h` e acima de cem dias devolve `—`, para nenhuma conta com divisor perto de zero voltar a imprimir notação científica na interface.

Nomes comparados no cliente (checagem antes de colar ou arrastar, `uniqueName` do "Manter ambos") usam `nameKey` de `lib/paths.ts`: NFC e minúsculas, espelhando `vfs.NameKey`. Diferenças raras entre as duas funções (o `ß`, por exemplo) só fazem o cliente perguntar ou desviar à toa; quem decide é o servidor.

## Estilo

Tailwind 4 via `@tailwindcss/vite`. Classes compostas são declaradas com `@utility` em `index.css` (`btn`, `btn-primary`, `btn-ghost`, `btn-danger`, `input`, `card`, `menu`, `menu-item`, `row-selected`, `row-focused`); `@apply` de classe própria dentro de outra falha no build. Tema claro/escuro segue `prefers-color-scheme` (`color-scheme: light dark`).

## Build e embed

`vite.config.ts`: `outDir` aponta para `internal/server/webdist/dist`, `emptyOutDir: true`, plugin que recria `.keep` após o build (o `//go:embed all:dist` precisa que a pasta exista mesmo sem build). O servidor de desenvolvimento faz proxy de `/api` para `127.0.0.1:8080`. `@types/node` é dev-dependency por causa dos imports Node no `vite.config.ts` (incluído em `tsconfig.json` → `types`).

## Como adicionar uma funcionalidade

1. Endpoint: handler em `internal/server/handlers_*.go`, rota em `routes()` de `server.go`, código de erro em `respond.go` se novo, teste em `server_test.go`, doc em `docs/04`.
2. Cliente: método em `api/client.ts` e tipo em `api/types.ts`.
3. UI: ação na página, texto em `i18n/pt-BR.ts`, `i18n/en.ts` e `i18n/es.ts`, tradução do código de erro em `errorCodes` dos três.
