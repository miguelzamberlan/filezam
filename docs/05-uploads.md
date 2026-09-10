# 05 · Protocolo de upload

Objetivo: saturar o disco/rede com poucas requisições, nunca deixar arquivo pela metade visível, e retomar após falhas.

## Escolha do modo (cliente, `upload/scheduler.ts`)

| Tamanho do arquivo | Modo | Endpoint |
|---|---|---|
| ≤ `batchFileMax` (1 MiB) | **lote** | `POST /api/files/batch` |
| ≤ `chunkSize` (16 MiB padrão) | **único** | `PUT /api/files/content` |
| > `chunkSize` | **chunked** | `/api/uploads` |

Os limites vêm de `GET /api/config`. O cliente mantém um pool global de `maxParallel` (4) requisições simultâneas, de qualquer modo; o servidor limita a 8 por usuário (429 → retry).

## Regras comuns no servidor

- O caminho de destino passa por `NormalizeWritable`; pais são criados com `MkdirAll`.
- O conteúdo vai para `.filezam-upload-<id>.part` **no diretório de destino** (mesmo sistema de arquivos → rename atômico). O prefixo é oculto na listagem e proibido em escrita.
- Com `FILEZAM_FSYNC=true`, `fsync` antes de finalizar.
- `mtime` do cliente (ms) é aplicado à parte: valores `<= 0` viram "agora"; o teto é `agora + 24 h`.
- **Finalize**: `overwrite=true` → `Rename` (substitui atomicamente; erro `is_dir` se o destino for pasta). `overwrite=false` → `Link(part, final)` + `Remove(part)`; `EEXIST` → 409 `exists`. Sem suporte a hardlink → `Lstat` + `Rename`.
- Leitura do corpo com prazo renovado a cada 1 MiB; 60 s sem bytes aborta a requisição.

## Único (`PUT`)

Corpo bruto, `Content-Type: application/octet-stream`. Recusa `Content-Length > chunkSize` com 413. Resposta 201 com a `Entry` final.

## Lote

`POST /api/files/batch?dir=<destino>&overwrite=0|1`, `multipart/form-data`:

1. Primeira parte, campo `meta`: `{"files":[{"path":"sub/a.txt","mtime":1700000000000,"size":10}, ...]}`. Caminhos relativos a `dir` (podem conter subpastas). Máximo `batchMaxFiles` (200).
2. Partes seguintes: campo nomeado pelo **índice** no manifesto (`"0"`, `"1"`, …), conteúdo do arquivo. O nome de arquivo da parte é ignorado (Go descarta diretórios de `FileName()`).

O servidor processa em streaming com `multipart.NewReader`, nunca `ParseMultipartForm`. Corpo total ≤ `batchMaxBytes` (32 MiB) + 1 MiB. Resposta sempre 200 com `results[i]` por arquivo: `{path, ok, code?, error?, entry?}`. Índices sem parte recebem `code: "missing"`; caminhos inválidos, `invalid_path`. Um lote com falhas parciais **não** é atômico: os arquivos `ok` já estão gravados.

Empacotamento no cliente (`pickBatch`): arquivos consecutivos com o mesmo `destDir` e o mesmo flag `overwrite`, até 200 arquivos ou 32 MiB.

## Chunked

### Criar sessão

`POST /api/uploads {dir, name, size, mtime, overwrite}` (`size` entre 0 e 1 PiB, e não maior que o espaço livre; senão 400 `invalid_path` / 507):

- Verifica destino (409 `exists` se existe e `overwrite=false`).
- Verifica espaço livre (`statfs`) → 507 `no_space`.
- Insere a linha em `uploads` (índice único `(dir, name)` base-relativo → 409 `upload_in_progress`).
- Cria a parte com `O_EXCL` e **pré-aloca** `size` bytes (`fallocate`; fallback `Truncate` em tmpfs/overlay/NFS/FUSE). `ENOSPC` aqui remove parte e linha → 507.
- Devolve `{id, chunkSize, chunks, received: []}`. O `chunkSize` fica gravado na sessão.

### Enviar blocos

`PUT /api/uploads/{id}?index=N`, corpo bruto do bloco. O tamanho esperado é `min(chunkSize, size - N*chunkSize)` e o `Content-Length` deve ser exatamente esse (400 `bad_length`; 411 sem `Content-Length`). O servidor escreve com `io.NewOffsetWriter` no offset `N*chunkSize`, marca o bit `N` no bitset (`received BLOB`) sob um mutex por sessão e persiste. Blocos podem chegar **em paralelo e fora de ordem**; reenviar um bloco já recebido é idempotente.

O cliente mantém até 2 blocos em voo por arquivo (`CHUNK_SLOTS_PER_FILE`), dentro do pool global.

### Concluir

`POST /api/uploads/{id}/complete`: se faltarem bits → 409 `incomplete {missing:[...]}` e o cliente reenvia só esses. Completo → `fsync` (se configurado), `Chtimes`, `Finalize`, apaga a linha, devolve `{entry}`. Se o destino surgiu no meio tempo com `overwrite=false` → 409 `exists`; o cliente pergunta ao usuário e, se for sobrescrever ou renomear, aborta a sessão e recomeça (as partes não são reaproveitáveis com outro nome).

### Abortar

`DELETE /api/uploads/{id}` remove parte e linha.

### Retomada

O navegador não persiste objetos `File`. Fluxo:

1. Ao carregar a UI, `GET /api/uploads` lista sessões pendentes; a tela mostra "N uploads incompletos. Solte os mesmos arquivos para retomar." com botão para descartar.
2. Ao soltar/selecionar arquivos, cada candidato a chunked é comparado com as pendentes por `(dir, name, size, mtime ± 1 s)`. Se casar, o item adota a sessão e envia só os índices ausentes.
3. Se a criação devolver `upload_in_progress`, o cliente recarrega as pendentes e tenta casar; se não casar (outro arquivo com o mesmo nome), o item falha com essa mensagem.
4. Se um `PUT` de bloco devolver 404 (sessão removida pela limpeza), o item volta à fila e recomeça do zero.

### Limpeza

- Tarefa horária (e na inicialização): sessões com `updated_at` mais antigo que `FILEZAM_UPLOAD_STALE` (24 h fixo em `config`) têm a parte removida via root base e a linha apagada.
- Na listagem de um diretório, partes `.filezam-upload-*.part` cujo id não existe na tabela **e cujo mtime está parado há mais de `uploads.OrphanGrace` (15 min)** são apagadas (cobre perda do banco, crash, etc.). A carência existe porque `PUT` único e lote gravam a parte sem linha no banco: enquanto o corpo chega o mtime avança, e sem ela uma listagem da mesma pasta (de outro usuário, ou o refetch da própria interface) apagaria um upload em andamento e o `Finalize` falharia com 404.
- Exclusão de usuário remove as partes das sessões dele antes do cascade.

## Conflitos (cliente, `upload/manager.ts`)

Um 409 `exists` põe o item em estado `conflict` e abre o diálogo "Substituir / Pular / Manter ambos / Cancelar" com "Aplicar a todos". Os diálogos são serializados numa fila para que "aplicar a todos" valha para os itens seguintes. "Manter ambos" gera `nome (n).ext` considerando os nomes já na fila para o mesmo diretório. A escolha padrão é zerada a cada nova ação de soltar/selecionar.

## Erros e novas tentativas

Retryáveis: 429, 502, 503, 504 e falhas de rede. Backoff 1, 2, 4, 8, 16 s, até 6 tentativas por item. `no_space` pausa a fila inteira e avisa. Cancelar aborta o XHR e, se chunked, apaga a sessão.

## Progresso e responsividade

- `XMLHttpRequest` para ter `upload.onprogress`; `fetch` não expõe progresso de envio.
- O gerenciador vive fora do React; publica um snapshot no máximo 4×/s (`useSyncExternalStore`); o painel é virtualizado.
- Velocidade: média móvel exponencial amostrada a cada 500 ms.
- Invalidação da listagem por diretório afetado com atraso (300 ms ocioso, 2 s com vários uploads ativos).
- A travessia de pastas soltas (`webkitGetAsEntry`) itera `readEntries` até vazio (o navegador devolve no máximo ~100 por chamada), cede o thread a cada 50 entradas e enfileira em lotes de 500 arquivos, então o envio começa antes de terminar a varredura. Pastas vazias são criadas com `mkdir` ao final.

## Limites relevantes de proxies

| Proxy | Configuração |
|---|---|
| nginx | `client_max_body_size 0; proxy_request_buffering off; proxy_read_timeout 600s` |
| Traefik v3 | `respondingTimeouts.readTimeout` ≥ 10 min |
| Cloudflare | 100 MB por requisição → chunk ≤ 64 MiB (padrão 16 MiB) |
| Caddy | nada |
