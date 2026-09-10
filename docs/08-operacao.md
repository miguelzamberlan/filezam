# 08 · Operação

## Deploy padrão (Docker Compose)

```bash
git clone <repo> filezam && cd filezam
cp .env.example .env      # ajuste PUID/PGID, FILEZAM_HOST_ROOT, FILEZAM_TRUSTED_PROXIES, FILEZAM_SECURE_COOKIES, FILEZAM_PUBLIC_URL
docker compose up -d --build
```

O `docker-compose.yml` do repositório traz dois serviços: `init` (Alpine, ajusta o dono de `/config` e de `/data` só se vazia, sai) e `filezam` (depende do `init` concluir). Portas expostas só em `127.0.0.1` por padrão.

### Integrar num compose existente

Copie o serviço `filezam` para o compose do stack e:

- `build: /caminho/do/clone` + `image: filezam:latest` (a imagem não está em registro público).
- Monte a pasta de dados em `/data` e uma pasta de configuração **fora** da pasta de dados em `/config`.
- Com Cloudflare Tunnel ou Traefik na mesma rede Docker, não publique porta: aponte o proxy para `http://filezam:8080` e coloque a sub-rede Docker em `FILEZAM_TRUSTED_PROXIES` (ex.: `172.16.0.0/12`).
- Exemplo real em uso: stack `servidor` com Samba, Filebrowser, cloudflared e PiGallery2 compartilhando o mesmo HD; Filezam em `/data`, banco em `/home/<user>/config_filezam`.

## Variáveis de ambiente

Além das variáveis do README:

| Variável | Padrão | Descrição |
|---|---|---|
| `FILEZAM_TRASH_RETENTION` | `720h` | Quanto tempo itens excluídos ficam na lixeira (`<escopo>/.filezam-trash`) antes de serem apagados; `0` desativa a lixeira (excluir apaga de vez) |
| `FILEZAM_INDEX_INTERVAL` | `6h` | Intervalo da varredura completa do índice de nomes usado pela pesquisa; `0` desativa o índice (a pesquisa percorre o disco) |

Ver tabela completa no [README](../README.md#variáveis-de-ambiente). Validações no startup: raiz não pode ser `/`; `FILEZAM_DATA_DIR` não pode estar dentro da raiz; chunk entre 1 MiB e 1 GiB; `FILEZAM_SECURE_COOKIES=auto` sem proxies confiáveis gera aviso (cookies não serão `Secure` atrás de proxy).

## Proxy reverso

O app precisa de: `Host` preservado, `X-Forwarded-For`, `X-Forwarded-Proto`, corpo sem limite/buffer, timeouts de leitura ≥ 10 min. Receitas para nginx, Traefik, Caddy e Cloudflare no README. Com Cloudflare, 100 MB por requisição → chunk padrão de 16 MiB está OK.

## Permissões de arquivos

- O processo grava como `PUID:PGID`; arquivos criados pertencem a esse usuário com modo `0644`/`0755`.
- Discos NTFS/exFAT montados com `uid=/gid=` funcionam; `chown` neles falha silenciosamente no `init` (esperado).
- Sistemas sem `fallocate` (tmpfs, overlay, alguns NFS/FUSE) usam `Truncate`; sem hardlink usam `Lstat+Rename`.

## Atualização

```bash
cd /caminho/do/clone && git pull
cd /caminho/do/compose && docker compose up -d --build filezam
```

Migrações do banco rodam automaticamente no início. Jobs em andamento são perdidos (só o progresso exibido); uploads chunked em andamento retomam pelo cliente.

A versão do Go está fixada em `go.mod` e no `Dockerfile`; ao subir de versão (correções de segurança da biblioteca padrão), reconstrua a imagem. Uma imagem construída antes de um commit não recebe nada dele: `docker compose up -d` sem `--build` só reinicia o container antigo.

## Backup

Parar o serviço, copiar `filezam.db`, `filezam.db-wal`, `filezam.db-shm` de `/config`. Os arquivos em `/data` são seus e devem ter backup próprio.

## Diagnóstico

| Sintoma | Causa provável | Ação |
|---|---|---|
| `directory /config is not writable by uid N` em loop | bind mount criado pelo Docker como root | Manter o serviço `init` no compose ou `chown -R N:N` na pasta do host |
| Login funciona mas cai na hora | cookie `Secure` em HTTP puro, ou `auto` sem proxy confiável | `FILEZAM_SECURE_COOKIES=false` local / definir `FILEZAM_TRUSTED_PROXIES` |
| Upload falha sempre em N MB ou N segundos | limite/buffer/timeout do proxy | Aplicar receita do proxy; reduzir `FILEZAM_MAX_UPLOAD_CHUNK` |
| 403 `csrf` em scripts | falta `X-Filezam: 1` | Adicionar o header |
| 429 `busy` em uploads | mais de 8 requisições simultâneas por usuário | Normal; o cliente reenvia com backoff |
| Link copiado com endereço errado (host/porta interna) | proxy que não repassa `Host`/`X-Forwarded-Host` de um IP em `FILEZAM_TRUSTED_PROXIES` | A interface já usa a origem do navegador; para links gerados pela API defina `FILEZAM_PUBLIC_URL=https://arquivos.exemplo.com` |
| Botão "Copiar link" mostra "—" em Compartilhados | link criado antes da migração 002 (token não guardado) | Criar um link novo |
| Link público sempre 404 | expirado, revogado, pasta movida/renomeada, dono desativado ou escopo do dono estreitado | Criar novo link / reativar o usuário |
| Arquivo sumiu depois de excluir | foi para a lixeira | Menu **Lixeira** → Restaurar (itens expiram após `FILEZAM_TRASH_RETENTION`) |
| Pesquisa não acha arquivo copiado por Samba/SSH | índice de nomes só vê mudanças feitas pelo Filezam até a próxima varredura | Aguardar `FILEZAM_INDEX_INTERVAL` ou **Reconstruir índice** na tela Pesquisar (admin) |
| Pasta `.filezam-trash` no disco | lixeira do Filezam; invisível na interface | Não apagar à mão: use Esvaziar lixeira |
| Preview de PDF em branco ou com ícone de bloqueio | build antigo (cabeçalho `X-Frame-Options: DENY` no conteúdo inline) | Reconstruir a imagem |
| 429 `rate_limited` ao trocar a senha | mais de 5 tentativas/min com a senha atual errada | Aguardar um minuto |
| `scope_unavailable` | pasta de escopo apagada/renomeada | Admin redefine o escopo do usuário |
| Admin sem senha | — | `docker compose run --rm filezam reset-admin 'Senha123'` |

Logs em JSON no stdout (`docker compose logs -f filezam`); `FILEZAM_LOG_LEVEL=debug` registra cada requisição com método, path, status, ms e IP. Tokens de links públicos aparecem mascarados (`/api/public/<token>/…`).

## Desenvolvimento local

```bash
make run    # API em :8080 (troque com FILEZAM_LISTEN=127.0.0.1:8765 se ocupada)
make dev    # Vite :5173 com proxy
make web && make build   # binário com UI embutida
```
