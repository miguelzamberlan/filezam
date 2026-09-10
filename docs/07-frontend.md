# 07 · Frontend

Projeto Vite em `web/`, React 19 + TypeScript estrito + Tailwind CSS 4. Saída do build em `internal/server/webdist/dist`, embutida no binário.

## Estrutura

```
web/src/
  main.tsx            QueryClientProvider + BrowserRouter
  App.tsx             Rotas e guarda de autenticação (Protected)
  hooks.ts            useAuth, useConfig, useListing, useInvalidateDirs, useFavorites
  strings.ts          Todos os textos (pt-BR) e a tradução dos códigos de erro
  index.css           Tailwind + utilitários próprios (@utility btn, input, card, menu…)
  api/client.ts       fetch tipado (X-Filezam, 401 → /login), ApiError, objeto Api
  api/types.ts        Tipos espelhando docs/04
  lib/                paths (join/dirname/encode/uniqueName), naturalSort, format,
                      clipboard (copyText com fallback), share (shareLink)
  store/ui.ts         zustand: seleção, âncora, foco, clipboard, ordenação, visão, filtro
  store/jobs.ts       zustand: jobs acompanhados pelos toasts
  upload/manager.ts   UploadManager (sem React) — fila, modos, slots, retries, conflitos, retomada
  upload/scheduler.ts funções puras (classify, pickBatch, backoffMs, chunkRange) — testadas
  upload/walk.ts      travessia de DataTransfer/FileList
  upload/xhr.ts       XMLHttpRequest com progresso → Promise
  components/         Shell, FileList, Breadcrumb, Toolbar (dentro de Browser), ContextMenu,
                      Preview, ShareDialog, InfoDialog (propriedades), SettingsDialog, DiskBar,
                      UploadPanel, JobToasts, dialogs (prompt/confirm/conflict/toast), Icons
  pages/              Login, ChangePassword, Browser, Shares, AdminUsers, AdminAudit, PublicShare
```

## Rotas

| Rota | Página | Guarda |
|---|---|---|
| `/login` | Login | — |
| `/change-password` | ChangePassword | sessão |
| `/b/*` | Browser (`*` = caminho escopo-relativo, cada segmento `encodeURIComponent`) | sessão + senha em dia |
| `/search?path=&q=` | Search (pesquisa recursiva por nome a partir de `path`; resultados levam a `/b/<pasta>?sel=<nome>`, que seleciona o item) | sessão + senha em dia |
| `/shares`, `/admin/users`, `/admin/audit` | idem | idem (admin para `/admin/*`; a API também valida) |
| `/s/:token/*` | PublicShare | — |

`Protected` redireciona para `/login` sem sessão e para `/change-password` quando `mustChangePassword`.

## Estado

- **Servidor** (TanStack Query): `['me']`, `['config']` (staleTime ∞), `['list', path]` (staleTime 10 s), `['favorites']`, `['shares']`, `['admin-users']`, `['admin-dirs', path]`, `['job', id]` (refetch 500 ms enquanto `running`), `['public', token, ...]`. Mutações invalidam as chaves afetadas; `useInvalidateDirs(dirs)` é o ponto único para listagens.
- **UI** (zustand `useUI`): `selection: Set<string>` de nomes na pasta atual, `anchor`/`focused` para Shift/teclado, `clipboard {op: copy|cut, dir, names}`, `sort`, `view`, `filter` e `prefs` (persistidos em `localStorage`: `sort`, `view`, `prefs`).
- **Preferências** (`prefs`, editadas em `SettingsDialog` pela engrenagem do rodapé): `showHidden` (arquivos iniciados por ponto; o servidor sempre os lista, o filtro é no cliente e o rodapé mostra "N ocultos"), `showHints` (dicas de atalhos), `confirmDelete`, `zoom` (fator da listagem, passos em `ZOOM_STEPS`; botões −/%/+ na barra do Browser e seleção em Configurações). `theme` (`system`/`light`/`dark`: `lib/theme.ts` põe a classe `.dark` no `<html>` e `color-scheme`; o Tailwind usa `@custom-variant dark (&:where(.dark, .dark *))`, nunca a media query direto) e `accent`/`selection`/`focus` (hex; viram as variáveis `--accent`/`--selection`/`--focus` no `<html>`, expostas como `bg-accent`, `text-accent`, `ring-focus` etc. via `@theme inline`; `row-selected`, `nav-active` e `btn-primary` derivam tons com `color-mix`, então uma cor serve para os dois temas). `uploadPanelOpen` e `sidebarOpen` (não persistidos) controlam a lista do painel de envios e o menu lateral em telas estreitas; o item **Uploads** do menu lateral expande o painel ou, sem envios, explica como enviar. Novas preferências: adicionar em `Prefs`/`defaultPrefs` em `store/ui.ts`, uma linha no `SettingsDialog` e o texto em `strings.ts`.
- **Uploads**: fora do React; `useUploads()` lê o snapshot via `useSyncExternalStore`.

## Página Browser (`pages/Browser.tsx`)

Concentra as ações. Convenções:

- Seleção: clique = única; Ctrl/Cmd = alternar; Shift = intervalo desde a âncora; clique no fundo limpa.
- Abrir: pasta navega; arquivo com preview abre o modal; senão baixa por `<a download>` invisível.
- Colar: se algum nome já existe no destino, pergunta uma vez (`dialogs.conflict`) e envia a política à API; `cut` limpa o clipboard após o job.
- Soltar arquivos: no fundo → pasta atual; sobre uma linha de pasta → dentro dela (destaque azul). Tipos sem `Files` são ignorados.
- Erros da API viram toast com `errorMessage(code)`.

Atalhos: `↑ ↓ Home End PgUp PgDn` (com Shift estende), `→ ←` na grade, `Enter`, `Backspace`/`Alt+↑`, `Esc`, `F2`, `Delete`, `Espaço` alterna, `Ctrl+A/C/X/V`, `Ctrl+Shift+N`, digitação salta para o prefixo (buffer de 700 ms). O handler ignora eventos vindos de inputs e quando há menu/preview/diálogo aberto.

## Celular e toque

- Menu lateral vira gaveta abaixo de `lg`; cada página coloca `<MenuButton />` (de `Shell.tsx`) na sua primeira linha em vez de gastar uma linha só com o botão.
- Browser: a barra de ações completa só aparece a partir de `sm`; abaixo disso há uma linha compacta (nova pasta, enviar, filtro, colar quando há área de transferência e **⋯** que abre o mesmo menu de contexto da pasta ou da seleção). Zoom só na barra ≥ `sm` e em Configurações.
- Toque (`pointerType === 'touch'` no `click`): um toque **abre** pasta/arquivo; toque longo (~600 ms sem mover, implementado em `FileList` com pointer events, para o iOS que não dispara `contextmenu`; no Android o evento nativo cancela o timer) abre o menu de contexto e seleciona o item; com uma seleção ativa, toques alternam itens e a linha compacta mostra "N selecionados ✕" para limpar. Mouse mantém clique = selecionar, duplo clique = abrir.
- Tabelas (Pesquisar, Compartilhamentos, Usuários, Auditoria) escondem colunas secundárias abaixo de `sm`/`md` e os botões ficam só com ícone.
- `height: 100dvh` quando suportado e `touch-action: manipulation` nos botões.

## FileList

Virtualizada com `@tanstack/react-virtual` (34 px por linha; grade com colunas calculadas por `ResizeObserver`). A prop `zoom` aplica CSS `zoom` ao contêiner de rolagem: o virtualizador trabalha nas coordenadas já ampliadas (`scrollTop`/`clientHeight` do próprio elemento), então os tamanhos estimados não mudam; validado no Chrome rolando até a última linha a 150 %. Recebe entradas já ordenadas (`sortEntries`: pastas primeiro, `Intl.Collator` numérico) e filtradas. É a mesma para o browser autenticado e a página pública (`readOnly`). `focused` é rolado para a vista.

## Ícones (`components/Icons.tsx`)

`iconFor(name, type)` escolhe por extensão em `EXT_ICONS`: imagem, vídeo, áudio, arquivo compactado, PDF/DOC/XLS/PPT (contorno de arquivo com a sigla e cor por família), texto e código; qualquer outra extensão cai no ícone genérico. Para acrescentar um tipo, adicione a extensão à lista certa ou crie um `IDoc('SIGLA')`.

## Página Search (`pages/Search.tsx`)

Estado na URL (`?path=&q=`), consulta `['search', path, q]` (staleTime 30 s). Filtra localmente itens ocultos (nome ou pasta iniciados por ponto) conforme `prefs.showHidden`. Aviso quando `partial`. O botão de lupa na barra do Browser abre a pesquisa já a partir da pasta atual.

## Diálogos e toasts (`components/dialogs.tsx`)

API imperativa baseada em Promises: `dialogs.prompt({title, initial, selectExt})`, `dialogs.confirm({danger})`, `dialogs.conflict(name)` → `{choice, all}`, `dialogs.custom(render)`. `DialogHost` renderiza a pilha; `Modal` fecha com Esc e clique fora. `toast(text, kind)`.

## Propriedades e disco

- Links públicos: `shareLink(token, publicUrl)` monta a URL a partir de `FILEZAM_PUBLIC_URL` (quando o operador definiu) ou de `window.location.origin` — nunca do host visto pelo servidor, que pode ser interno. `copyText` copia com fallback para `document.execCommand('copy')` em origens não seguras (http://) e avisa por toast quando não consegue. `ShareDialog` já copia o link ao criar; `Shares` e `InfoDialog` copiam de novo pelo `token` que a API devolve.
- `InfoDialog` (`GET /api/files/info`): caminho com botão copiar, tipo, tamanho, modificação; para pastas, conteúdo calculado (bytes, arquivos, pastas, aviso de contagem parcial), se está nos favoritos e os links públicos ativos daquela pasta com validade, número de acessos e último acesso. Abre pelo botão ⓘ da toolbar ou "Propriedades" no menu de contexto. O menu de contexto mostra o caminho completo do item como primeira linha (desabilitada).
- `DiskBar` (`GET /api/files/disk`, refetch a cada 60 s e após qualquer operação via `useInvalidateDirs`): barra de uso com cor por faixa (azul, âmbar > 75 %, vermelho > 90 %) e "X livres de Y" no rodapé da lateral.

## Preview

`previewKind(entry)` por extensão: imagem (`<img>`), vídeo/áudio (`<video>/<audio>` com `preload=metadata`, seeking via Range), PDF (`<iframe>` **sem** atributo `sandbox`: o Chrome bloqueia o visualizador de PDF em frames com sandbox; o isolamento vem da CSP `sandbox` que o servidor envia com o arquivo, ver [03](03-seguranca.md#cabeçalhos-http)), texto (fetch com `Range: bytes=0-<previewMaxText>` em `<pre>`). Setas navegam entre os previewáveis da pasta.

## UploadManager

Ver [05](05-uploads.md) para o protocolo. Estados de item: `queued → uploading → done | failed | cancelled | skipped | conflict`. Métodos públicos: `configure(cfg)`, `add(files, destDir)`, `pause/resume`, `cancel(id)`, `cancelAll`, `retryFailed`, `clearDone`, `loadPending/discardPending`, `resetConflictDefault`. Callbacks injetados pelo `Shell`: `onConflict`, `onDirChanged`, `onError`.

`nextWork()` prioriza blocos de arquivos chunked já em andamento, depois o primeiro item da fila (lote agrupado, único ou nova sessão). O painel mostra progresso total, velocidade, ETA, contadores, pausa, cancelar tudo, repetir falhos.

A velocidade é amostrada em janelas de 1s e suavizada por EMA (`0.75 * anterior + 0.25 * instantânea`); `formatSpeed` a imprime sempre com duas casas decimais, deixando só a unidade mudar (B/s, KB/s, MB/s…), e o painel usa `tabular-nums` para o número não mudar de largura a cada atualização.

## Estilo

Tailwind 4 via `@tailwindcss/vite`. Classes compostas são declaradas com `@utility` em `index.css` (`btn`, `btn-primary`, `btn-ghost`, `btn-danger`, `input`, `card`, `menu`, `menu-item`, `row-selected`, `row-focused`); `@apply` de classe própria dentro de outra falha no build. Tema claro/escuro segue `prefers-color-scheme` (`color-scheme: light dark`).

## Build e embed

`vite.config.ts`: `outDir` aponta para `internal/server/webdist/dist`, `emptyOutDir: true`, plugin que recria `.keep` após o build (o `//go:embed all:dist` precisa que a pasta exista mesmo sem build). O servidor de desenvolvimento faz proxy de `/api` para `127.0.0.1:8080`. `@types/node` é dev-dependency por causa dos imports Node no `vite.config.ts` (incluído em `tsconfig.json` → `types`).

## Como adicionar uma funcionalidade

1. Endpoint: handler em `internal/server/handlers_*.go`, rota em `routes.go`, código de erro em `respond.go` se novo, teste em `server_test.go`, doc em `docs/04`.
2. Cliente: método em `api/client.ts` e tipo em `api/types.ts`.
3. UI: ação na página, texto em `strings.ts`, tradução do código de erro em `S.errorCodes`.
