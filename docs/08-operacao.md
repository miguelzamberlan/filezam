# 08 · Operação

Este documento cobre as formas de rodar o Filezam (máquina local, servidor com Docker Compose, painel Easypanel, binário direto), as variáveis de ambiente, o proxy reverso, backup, atualização e diagnóstico. O README traz a versão resumida; aqui está o detalhe.

## Antes de tudo: o modelo de pastas

| Caminho no container | O que é | Regras |
|---|---|---|
| `/data` (`FILEZAM_ROOT`) | A pasta que os usuários veem. Para expor várias pastas do host, monte-as como subpastas ([abaixo](#expondo-várias-pastas-do-host)). | Nunca pode ser `/`. Tudo que está nela é alcançável por um admin. |
| `/config` (`FILEZAM_DATA_DIR`) | Banco SQLite (usuários, sessões, links, lixeira, índice, auditoria) e `secret.key` (chave do 2FA). | **Fora** de `/data`; o app recusa iniciar se estiver dentro. Nunca é servida. Backup da pasta inteira. |

O processo roda **sem root** e precisa de permissão de escrita nas duas pastas. Na imagem, o usuário padrão é o `nonroot` do distroless (uid/gid `65532`) e as duas pastas já existem na imagem com esse dono, então volumes nomeados criados pelo Docker nascem graváveis. Com *bind mounts* de pastas do host, o dono é o da pasta no host: por isso o `docker-compose.yml` do repositório roda o container como `PUID:PGID` (o seu usuário) e traz um serviço `init` que ajusta o dono de `/config` e dos arquivos do Filezam dentro dela (`filezam.db*`, `secret.key`; nunca recursivo, e ele se recusa a rodar se `/config` tiver subpastas, sinal de `FILEZAM_HOST_CONFIG` errado) e de `/data`, só se estiver vazia. Se a pasta não for gravável, o app encerra na inicialização com a mensagem `directory /config is not writable by uid N` e o `chown` sugerido.

Discos NTFS/exFAT montados com `uid=`/`gid=` funcionam: basta que o `uid` do mount seja o mesmo `PUID`. Nunca use `PUID=0`.

## Expondo várias pastas do host

O Filezam expõe **uma raiz só**. Para alcançar pastas espalhadas pelo host (outro disco, um compartilhamento Samba, uma pasta de mídia), monte cada uma como **subpasta de `/data`** no `docker-compose.yml`:

```yaml
    volumes:
      - ${FILEZAM_HOST_ROOT:-./data}:/data
      - /mnt/midia:/data/midia                 # aparece como a pasta "midia" na raiz
      - /mnt/backup/projetos:/data/projetos
      - ${FILEZAM_HOST_CONFIG:-./config}:/config
```

Os usuários veem `midia/` e `projetos/` como pastas normais dentro da raiz. Para parametrizar, crie variáveis próprias no `.env` (`FILEZAM_HOST_MIDIA=/mnt/midia`) e use `${FILEZAM_HOST_MIDIA}:/data/midia`: o `.env` sozinho não resolve, quem decide a montagem é o compose. Não existe variável que aceite uma lista de pastas.

O que muda com mais de uma montagem:

- **As permissões são por sua conta.** O serviço `init` só acerta o dono de `/config` e de `/data` (esta apenas se estiver vazia); as pastas extras já precisam pertencer a `PUID:PGID` no host, ou estar montadas com `uid=`/`gid=` iguais no caso de NTFS/exFAT.
- **Cada montagem costuma ser um sistema de arquivos diferente.** Mover, ou mandar para a lixeira, atravessando montagens não é um `rename`: o kernel devolve `EXDEV`, o Filezam detecta e cai em copiar + apagar. Funciona, mas é lento com arquivo grande e não é atômico. Como a lixeira fica em `<escopo>/.filezam-trash`, na raiz do escopo, excluir algo de uma submontagem já copia os bytes entre discos — considere `FILEZAM_TRASH_RETENTION=0` se isso incomodar.
- **A regra de caixa vale para cada pasta montada**: todas precisam estar num sistema de arquivos que diferencie maiúsculas (próxima seção), não só a raiz.
- **O espaço livre** nunca soma as montagens. Na interface é o do sistema de arquivos que contém a raiz do escopo de quem está olhando (um usuário com escopo em `midia/` vê o disco de `/mnt/midia`; um admin na raiz vê o disco de `/data`); em `filezam_disk_free_bytes` é sempre o de `FILEZAM_ROOT`.
- **Symlink não substitui a montagem.** Todo acesso passa pelo `os.Root`, que recusa link apontando para fora da raiz; tem que ser bind mount.
- **Backup**: o que está numa pasta extra não entra num backup de `FILEZAM_HOST_ROOT`. Trate cada montagem separadamente.

Se a intenção é cada usuário ver uma pasta diferente, isso não é montagem: é o **escopo** da conta, definido em Administração → Usuários e apontando para a subpasta.

## Sistema de arquivos da pasta de dados

`FILEZAM_ROOT` precisa estar num sistema de arquivos que **diferencie maiúsculas de minúsculas**: ext4, xfs, btrfs, zfs. Não é preferência de estilo — o prefixo reservado `.filezam-` (lixeira e partes de upload) é comparado byte a byte, então num sistema que não diferencia, um caminho escrito `.FILEZAM-trash` passa pela validação e chega na pasta real.

Isso decide onde os dados moram em cada sistema operacional:

| Onde você roda o Docker | Onde colocar `FILEZAM_HOST_ROOT` |
|---|---|
| Linux | qualquer pasta em ext4/xfs/btrfs/zfs — o caso normal |
| **Windows** (Docker Desktop/WSL2) | dentro do WSL2 (`/home/você/filezam/data`, que é ext4) ou num volume nomeado do Docker. **Nunca** um caminho `C:\...`, `/mnt/c/...` |
| **macOS** (Docker Desktop) | um volume nomeado do Docker. Bind mount de pasta do host é APFS, que também não diferencia |

Além da regra do prefixo reservado, manter os dados fora do disco do host em Windows e macOS é muito mais rápido: o bind mount atravessa uma camada de tradução de sistema de arquivos, e listar uma pasta grande fica ordens de grandeza mais lenta que no volume nativo.

### Nomes de arquivo

O Filezam **nunca normaliza** um nome: os bytes que o navegador envia são os que vão para o disco e os que voltam na listagem. Acentuação de português, espanhol, inglês, alemão (`á ç ñ ü ã ß`) atravessa idêntica em qualquer sistema. O que muda é o que o sistema de arquivos por baixo aceita:

| | ext4/xfs/btrfs | NTFS | APFS |
|---|---|---|---|
| `MAIÚSCULA.txt` e `maiúscula.txt` na mesma pasta | dois arquivos | um só | um só |
| o mesmo nome em NFC e em NFD | dois arquivos | dois arquivos | um só |
| `Relatório: Final.pdf` (`: * ? " < > \|`) | vale | o sistema recusa | vale |
| `CON.txt`, `AUX.txt`, `NUL.txt`, `COM1.txt` | vale | o sistema recusa | vale |
| trocar só a caixa ao renomear (`foto` → `Foto`) | vale | 409 `exists` | 409 `exists` |

Nomes já existentes no disco com **UTF-8 inválido** (vindos de um Samba antigo com página de código Latin-1, por exemplo) aparecem na listagem em vermelho e não podem ser abertos, renomeados nem apagados pela interface — o caminho não é endereçável. Renomeie-os pelo sistema de arquivos.

Limites do próprio Filezam, iguais em todo lugar: 255 bytes por segmento, 4096 bytes no caminho inteiro, 128 níveis de profundidade.

## 1. Na sua máquina (teste rápido)

### Com Docker Compose

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
docker compose up -d --build
docker compose logs filezam | grep password
```

**O `.env` não é obrigatório.** O `docker-compose.yml` declara `env_file: required: false` e todo valor tem padrão (`${PUID:-1000}`, `${FILEZAM_BIND:-127.0.0.1}`…), então um clone limpo sobe sem configurar nada. Copie `.env.example` para `.env` quando for para valer — UID/GID reais, pasta do host, proxy, URL pública.

Abra `http://127.0.0.1:8080` e entre como `admin` com a senha que apareceu no log; a troca é obrigatória. Sem `FILEZAM_ADMIN_PASSWORD`, a senha inicial é **sorteada e mostrada uma única vez** — se o log já rolou, `docker compose exec filezam /filezam reset-admin` sorteia outra e a imprime. Sem `.env`, `FILEZAM_SECURE_COOKIES` vale `auto` e o cookie sai sem `Secure`, que é o certo em HTTP local; o `.env.example` já vem com `true`, para produção atrás de HTTPS, e aí em HTTP o navegador descarta o cookie e o login não completa. Para parar: `docker compose down`. Os dados ficam em `./data` e `./config`.

### Sem Docker (binário)

Precisa de Go (a versão de `go.mod`) e Node 22+:

```bash
git clone https://github.com/miguelzamberlan/filezam.git && cd filezam
make web && make build            # build do frontend e binário ./filezam com a UI embutida
FILEZAM_ROOT=$HOME/Arquivos FILEZAM_DATA_DIR=$HOME/.filezam FILEZAM_SECURE_COOKIES=false ./filezam
```

O binário escuta em `:8080`, cria a pasta do banco se não existir e imprime logs JSON no terminal. Todas as variáveis da tabela abaixo valem igual. Para desenvolvimento com hot reload, veja [`CONTRIBUTING.md`](../CONTRIBUTING.md).

## 2. Num servidor com Docker Compose e proxy reverso

O cenário para o qual o Filezam foi feito: um servidor Linux, uma pasta grande (HD externo, RAID, NAS montado), um proxy com HTTPS na frente e o container escutando só em `127.0.0.1`.

```bash
sudo mkdir -p /opt/filezam && cd /opt/filezam
git clone https://github.com/miguelzamberlan/filezam.git .
cp .env.example .env
```

`.env` mínimo para produção:

```ini
PUID=1000                                  # id -u do dono da pasta de dados
PGID=1000
FILEZAM_HOST_ROOT=/mnt/dados               # pasta exposta
FILEZAM_HOST_CONFIG=/opt/filezam/config    # banco + secret.key, fora da pasta exposta
FILEZAM_BIND=127.0.0.1                     # só o proxy local alcança a porta
FILEZAM_PORT=8080
FILEZAM_TRUSTED_PROXIES=172.31.250.1       # gateway da rede do compose: é por ele que o proxy do host chega
FILEZAM_SECURE_COOKIES=true                # acesso público é HTTPS
FILEZAM_PUBLIC_URL=https://arquivos.exemplo.com
# FILEZAM_ADMIN_PASSWORD=                  # vazia: sorteada e mostrada uma vez no log (recomendado)
FILEZAM_REQUIRE_2FA_ADMINS=true            # recomendado
```

```bash
docker compose up -d --build
docker compose logs -f filezam     # "filezam listening" = ok; e a senha do admin, uma única vez
```

Depois configure o proxy (receitas abaixo), abra `https://arquivos.exemplo.com`, entre com a senha que apareceu no log, troque-a (é obrigatório), ative o 2FA e crie os usuários com as pastas de acesso deles. Se o log já se perdeu antes do primeiro acesso, `docker compose exec filezam /filezam reset-admin` sorteia outra senha e a imprime.

### Proxy reverso

O app escuta HTTP puro e espera TLS no proxy. Ele precisa de: `Host` preservado, `X-Forwarded-For` e `X-Forwarded-Proto` vindos de um IP listado em `FILEZAM_TRUSTED_PROXIES` (usados para o IP real na auditoria e no rate limit, para o cookie `Secure=auto` e para montar links), e corpo de requisição **sem limite e sem buffer**. Uploads vão em blocos de `FILEZAM_MAX_UPLOAD_CHUNK` (16 MiB por padrão), então nenhuma requisição é enorme, mas o proxy não pode cortá-las nem guardá-las em disco.

**nginx**

```nginx
server {
    listen 443 ssl http2;
    server_name arquivos.exemplo.com;
    # ssl_certificate ...; ssl_certificate_key ...;

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
}
```

**Caddy** (TLS automático; funciona sem ajustes):

```caddyfile
arquivos.exemplo.com {
    reverse_proxy 127.0.0.1:8080
}
```

**Traefik v3** na mesma rede Docker: remova o bloco `ports` do compose, descomente `networks`/`labels` no `docker-compose.yml`, dê ao Traefik um IP fixo na rede `proxy` e ponha só esse IP em `FILEZAM_TRUSTED_PROXIES` (a sub-rede inteira da rede `proxy` só se nela houver apenas o Traefik e apps de confiança) e aumente `entryPoints.websecure.transport.respondingTimeouts.readTimeout` (padrão 60 s) para `10m`.

**Cloudflare Tunnel** (`cloudflared` na mesma rede Docker ou no host): aponte o túnel para `http://filezam:8080` (ou `http://127.0.0.1:8080`) e liste a origem do túnel em `FILEZAM_TRUSTED_PROXIES`: o IP fixo do container do `cloudflared`, ou o gateway da rede do compose (`172.31.250.1`) se o `cloudflared` roda no host. O proxy da Cloudflare limita cada requisição a 100 MB: mantenha o chunk em até 64 MiB (o padrão de 16 MiB já serve).

### Integrar num compose existente

Copie o serviço `filezam` (e, se usar bind mounts, o `init`) para o compose do seu stack:

- `build: /caminho/do/clone` + `image: filezam:latest`, ou a imagem publicada `ghcr.io/miguelzamberlan/filezam:<versão>` (gerada pela CI a cada tag `v*`; veja a aba *Packages* do repositório).
- Monte a pasta de dados em `/data` e uma pasta de configuração **fora** dela em `/config`.
- Com o proxy na mesma rede Docker, aponte-o para `http://filezam:8080` e confie **só no IP fixo dele**, não na sub-rede inteira (veja a seção seguinte). A porta publicada é opcional.
- Sem `env_file`, declare as variáveis no bloco `environment`; o que não for declarado assume o padrão da tabela abaixo.
- Mantenha `read_only`, `cap_drop`, `no-new-privileges` e `tmpfs /tmp` do compose de referência: o binário só grava em `/data` e `/config`.

### Porta local e proxies confiáveis

Publicar a porta na rede local é permitido e útil (servidor de casa ou do escritório, sem acesso externo, ou como acesso alternativo ao proxy). O que exige cuidado é `FILEZAM_TRUSTED_PROXIES`: de quem estiver nessa lista o app aceita `X-Forwarded-For`, `X-Forwarded-Proto` e `X-Forwarded-Host`.

**De onde o app vê cada conexão** (Docker com a porta publicada; conferido no Docker 29):

- de outra máquina da rede ou da internet: com o IP real dela (o Docker preserva a origem);
- do próprio servidor (proxy no host, `curl`, cron): com o IP do **gateway da rede do compose** (`172.31.250.1` no `docker-compose.yml` do repositório, que fixa a sub-rede `FILEZAM_SUBNET=172.31.250.0/24`), nunca `127.0.0.1`;
- de outro container na mesma rede: com o IP do container.

**O que um endereço confiável consegue**: escolher o IP gravado na auditoria e usado nos limites por IP, ou seja, escapar do limite de tentativas e do bloqueio por (usuário, IP) e testar senhas limitado só pelo custo do Argon2 (4 verificações simultâneas); informar o esquema (cookie `Secure=auto`, HSTS) e o host do link devolvido pela API. **Não** consegue entrar sem a senha (nem sem o código, com 2FA), sair do escopo, passar pelo CSRF ou ler arquivos.

| Confiar em | Quem consegue forjar o IP | Gravidade |
|---|---|---|
| nada (vazio) | ninguém | nenhuma |
| só o IP fixo do proxy, ou só o gateway do compose | quem já tem um shell no servidor | baixa |
| a sub-rede inteira do Docker (`172.16.0.0/12`) | também qualquer container ligado à rede do app | média |
| uma faixa da rede local (`192.168.0.0/16`) ou `0.0.0.0/0` | qualquer máquina da rede, ou da internet | alta: nunca faça |

**Recomendação por cenário**:

| Cenário | `FILEZAM_BIND` | `FILEZAM_TRUSTED_PROXIES` |
|---|---|---|
| Só rede local, sem proxy (`http://ip-do-servidor:8080`) | `0.0.0.0` | vazio (e `FILEZAM_SECURE_COOKIES=false`) |
| Proxy no próprio servidor (Caddy, nginx, `cloudflared` instalado no host) | `127.0.0.1` | `172.31.250.1` |
| Proxy no servidor e também acesso direto pela rede local | `0.0.0.0` | `172.31.250.1`: quem vem da rede aparece com o IP real e não consegue forjar; `FILEZAM_SECURE_COOKIES=auto` |
| Proxy em container na mesma rede (Traefik, `cloudflared`), com ou sem porta local | `0.0.0.0` (rede local), `127.0.0.1` ou sem `ports` | só o IP fixo do container do proxy; com acesso HTTP pela rede, `FILEZAM_SECURE_COOKIES=auto` |
| Binário sem Docker (systemd) com proxy no host | — | `127.0.0.1` |

Sem proxy o tráfego da rede local é HTTP puro (senha e cookie de sessão legíveis por quem escuta a rede): em rede que não é de confiança, ponha um proxy com HTTPS mesmo para uso interno. Se mudar `FILEZAM_SUBNET`, o gateway é o `.1` da nova sub-rede.

**HTTPS pelo proxy e HTTP pela rede local ao mesmo tempo** (ex.: Cloudflare Tunnel para a internet e `http://ip-do-servidor:8080` em casa): use `FILEZAM_SECURE_COOKIES=auto` (com `true` o navegador descarta o cookie no HTTP e o login pela rede local não funciona; com `auto` o cookie é `Secure` quando o proxy confiável informa HTTPS) e `FILEZAM_PUBLIC_URL=https://seu-dominio`, para que um link público criado pela rede local saia com o endereço da internet e não com o IP interno. Cada endereço tem sua própria sessão.

IP fixo para um proxy em container (ex.: `cloudflared` no mesmo compose, com a porta local aberta para a rede). Uma rede só para o app e o proxy evita recriar a rede padrão do stack (e reiniciar os outros serviços); o proxy continua também na `default` se servir outros apps:

```yaml
services:
  cloudflared:
    networks:
      default: {}
      tunel:
        ipv4_address: 172.30.10.10
  filezam:
    ports:
      - "8080:8080"                                  # rede local
    networks: [tunel]
    environment:
      FILEZAM_TRUSTED_PROXIES: 172.30.10.10
      FILEZAM_SECURE_COOKIES: auto
      FILEZAM_PUBLIC_URL: https://arquivos.exemplo.com
networks:
  tunel:
    ipam:
      config:
        - subnet: 172.30.10.0/24
```

Com o túnel apontando para `http://filezam:8080`, o app vê o `cloudflared` como `172.30.10.10` e confia nele; quem entra pela rede local aparece com o próprio IP; conexões do próprio servidor chegam pelo gateway `172.30.10.1`, que não é confiável.

## 3. No Easypanel

O [Easypanel](https://easypanel.io) sobe containers a partir de um repositório Git com `Dockerfile` e coloca o Traefik dele na frente com HTTPS automático. Passo a passo:

1. **Criar o serviço**: no projeto, *+ Service → App*. Em *Source*, escolha *GitHub* (ou *Git*), repositório `miguelzamberlan/filezam`, branch `main`. Em *Build*, selecione *Dockerfile* (caminho `Dockerfile`, contexto `/`). Alternativa sem build: *Source → Docker Image* com `ghcr.io/miguelzamberlan/filezam:latest`.
2. **Environment**: cole as variáveis (o Easypanel injeta como ambiente do container):

   ```ini
   FILEZAM_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
   FILEZAM_SECURE_COOKIES=true
   FILEZAM_PUBLIC_URL=https://arquivos.exemplo.com
   FILEZAM_ADMIN_PASSWORD=troque-esta-senha
   FILEZAM_REQUIRE_2FA_ADMINS=true
   FILEZAM_MAX_UPLOAD_CHUNK=16MiB
   ```

   Os CIDRs cobrem as redes Docker que o Traefik do Easypanel usa; se preferir o valor exato, veja a sub-rede em `docker network inspect easypanel`.
3. **Mounts**:
   - `/config`: *Volume Mount* (volume nomeado, ex.: `filezam-config`). Ele nasce com o dono certo porque a pasta já existe na imagem com o uid `65532`.
   - `/data`: *Bind Mount* apontando para a pasta do host que você quer expor (ex.: `/mnt/dados`). Como o container roda com o uid `65532`, a pasta precisa ser gravável por ele: `sudo chown -R 65532:65532 /mnt/dados` ou, se a pasta é compartilhada com outros serviços (Samba, outro app) e você não quer mudar o dono, use a opção abaixo.
4. **Domains**: adicione o domínio, porta `8080`, HTTPS ligado. O Traefik do Easypanel é a origem dos `X-Forwarded-*`, por isso o passo 2.
5. **Deploy**. Nos logs deve aparecer `filezam listening`. Abra o domínio, entre com `admin` e a senha da variável, e troque-a.

**Quando a pasta de dados pertence a outro usuário**: use o tipo de serviço *Compose* do Easypanel em vez de *App* e cole o `docker-compose.yml` do repositório com `user: "1000:1000"` (o uid do dono da pasta), removendo o bloco `ports` e adicionando os `labels` do Traefik. Isso é o mesmo modelo `PUID:PGID` da seção 2. Em qualquer dos modos, o serviço `init` do compose só é necessário para bind mounts cujo dono ainda não é o usuário do container.

**Uploads grandes**: o Traefik do Easypanel usa timeouts padrão; um bloco de 16 MiB completa em bem menos de 60 s em qualquer conexão razoável. Se a rede for lenta, reduza `FILEZAM_MAX_UPLOAD_CHUNK` (`4MiB`) em vez de mexer no Traefik.

## 4. Binário direto com systemd

Para quem não quer Docker: compile (`make web && make build`) ou baixe o binário, copie para `/usr/local/bin/filezam` e crie um usuário de serviço.

```ini
# /etc/systemd/system/filezam.service
[Unit]
Description=Filezam
After=network-online.target

[Service]
User=filezam
Group=filezam
Environment=FILEZAM_ROOT=/srv/arquivos
Environment=FILEZAM_DATA_DIR=/var/lib/filezam
Environment=FILEZAM_LISTEN=127.0.0.1:8080
Environment=FILEZAM_TRUSTED_PROXIES=127.0.0.1
Environment=FILEZAM_SECURE_COOKIES=true
Environment=FILEZAM_PUBLIC_URL=https://arquivos.exemplo.com
ExecStart=/usr/local/bin/filezam serve
Restart=on-failure
# Endurecimento
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/srv/arquivos /var/lib/filezam

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd -r -s /usr/sbin/nologin filezam
sudo mkdir -p /var/lib/filezam && sudo chown filezam:filezam /var/lib/filezam /srv/arquivos
sudo systemctl enable --now filezam
journalctl -u filezam -f
```

O proxy reverso é o mesmo da seção 2.

## Variáveis de ambiente

| Variável | Padrão | Descrição |
|---|---|---|
| `FILEZAM_ROOT` | `/data` (imagem) / `./data` | Pasta raiz dos arquivos. Não pode ser `/` |
| `FILEZAM_DATA_DIR` | `/config` (imagem) / `./config` | Banco, índice e `secret.key`. Não pode ficar dentro de `FILEZAM_ROOT` |
| `FILEZAM_LISTEN` | `:8080` | Endereço de escuta |
| `FILEZAM_TRUSTED_PROXIES` | vazio | IPs/CIDRs dos proxies, separados por vírgula. Só deles `X-Forwarded-*` é aceito; confie no mínimo possível ([Porta local e proxies confiáveis](#porta-local-e-proxies-confiáveis)) |
| `FILEZAM_SECURE_COOKIES` | `auto` | `true` / `false` / `auto` (detecta HTTPS via `X-Forwarded-Proto` de proxy confiável) |
| `FILEZAM_PUBLIC_URL` | vazio | Base dos links públicos gerados pela API (`https://...`); a interface usa o endereço do navegador quando vazio |
| `FILEZAM_ADMIN_USER` / `FILEZAM_ADMIN_PASSWORD` | `admin` / *(sorteada)* | Admin criado no primeiro início, com troca forçada. Senha vazia = sorteada e mostrada **uma única vez** no log; preencha só para instalação automatizada |
| `FILEZAM_SESSION_TTL` | `168h` | Validade deslizante da sessão (teto absoluto: 30 dias) |
| `FILEZAM_MAX_UPLOAD_CHUNK` | `16MiB` | Tamanho do bloco de upload (1 MiB a 1 GiB). Até 64 MiB atrás da Cloudflare |
| `FILEZAM_UPLOAD_MAX_RESERVED` | `100GiB` | Espaço que os uploads em blocos inacabados de um usuário podem reservar no disco (a sessão pré-aloca o tamanho do arquivo); `0` = sem teto. Um arquivo maior que isso precisa de um valor maior |
| `FILEZAM_SHARE_MAX_TTL` | `720h` | Validade máxima de um link público. Um link de recebimento nunca passa de 30 dias, nem com valor maior aqui |
| `FILEZAM_TRASH_RETENTION` | `720h` | Tempo na lixeira (`<escopo>/.filezam-trash`) antes de apagar de vez; `0` desativa a lixeira. É só o **valor inicial**: depois que o admin escolhe um prazo em **Configurações do sistema** (0 a 365 dias), o painel manda e a variável deixa de ter efeito |
| `FILEZAM_INDEX_INTERVAL` | `6h` | Varredura completa do índice de nomes da pesquisa; `0` desativa o índice (a pesquisa percorre o disco). Com o índice ligado, é o **valor inicial** do intervalo, que o admin muda em **Configurações do sistema** (15 min a 7 dias; a tela mostra quanto durou a última varredura). Desligar continua só pelo ambiente |
| `FILEZAM_METRICS_TOKEN` | vazio | Liga `GET /metrics` (Prometheus) para quem envia `Authorization: Bearer <token>` |
| `FILEZAM_SECRET_KEY` | vazio | 64 hex (32 bytes) que cifra os segredos de 2FA e assina o cookie de dispositivo confiável; vazio = `<DATA_DIR>/secret.key` gerado no primeiro início (modo 0600) |
| `FILEZAM_REQUIRE_2FA_ADMINS` | `false` | Obriga administradores a ativar a verificação em duas etapas |
| `FILEZAM_FSYNC` | `true` | `fsync` antes de finalizar cada upload (`false` só para máximo throughput em disco lento) |
| `FILEZAM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Só no `.env` do compose (não são lidas pelo binário): `PUID`/`PGID`, `FILEZAM_HOST_ROOT`, `FILEZAM_HOST_CONFIG`, `FILEZAM_BIND`, `FILEZAM_PORT`, `FILEZAM_SUBNET` (sub-rede da rede do compose, padrão `172.31.250.0/24`), `FILEZAM_VERSION`.

Validações no startup: raiz não pode ser `/`; `FILEZAM_DATA_DIR` não pode estar dentro da raiz (symlinks são resolvidos antes da checagem); chunk entre 1 MiB e 1 GiB; admin e senha iniciais não podem ser vazios; `FILEZAM_SECURE_COOKIES=auto` sem proxies confiáveis gera aviso no log (cookies não serão `Secure` atrás de proxy).

Os links com endereço personalizado e os links de recebimento **não têm variável de ambiente**: são
ligados e ajustados em **Administração → Configurações do sistema**, que grava na tabela `settings`
e vale na hora, sem reiniciar. O recebimento nasce desligado; o teto de 30 dias do vencimento é
constante no código e o painel só consegue encurtá-lo.

Dimensionamento de disco com links de recebimento: o pior caso por usuário é
`min(links ativos × cota por link, cota do usuário)`, e `FILEZAM_UPLOAD_MAX_RESERVED` continua sendo
a rede final para o que as sessões inacabadas reservam. Numa instalação com gente de fora enviando,
defina cota por usuário.

O cache de miniaturas fica em `<DATA_DIR>/thumbs` e entra no dimensionamento do volume de
configuração, não no de dados: o padrão é no máximo 2 GiB, ajustável em **Configurações do
sistema**. Apagar a pasta é seguro a qualquer momento — o servidor regenera sob demanda.

### Reiniciar o serviço com operações em andamento

Copiar, mover, excluir, extrair e compactar rodam dentro do próprio processo. Ao receber `SIGTERM`
(um `docker compose restart`, uma atualização), o servidor **espera até 30 segundos** as operações
terminarem e então cancela o que sobrou. Na subida seguinte, o que ficou como "em andamento" aparece
no histórico como interrompido.

Não há retomada: uma cópia de 50 GB cortada na metade deixa no destino o que já havia sido copiado.
Refazer a operação copia o que falta — os arquivos que já existem caem na política de conflito
escolhida (renomear, sobrescrever ou pular).

Na prática: antes de atualizar ou reiniciar, vale olhar **Operações** e esperar o que estiver
rodando, como já se recomenda para envios em andamento.

### Baixar uma pasta grande como ZIP

O ZIP é montado **em transmissão**, sem passar pelo disco e sem buffer do tamanho do conteúdo: um
download de 2 GB custa menos de 2 MB de memória no servidor, o mesmo que um de 256 MB (medido). Não
há teto de tamanho — baixar a raiz inteira funciona, e o método é `Store` (sem compressão), então o
custo é praticamente só o de ler o disco e mandar pela rede.

Consequências práticas de ser transmitido:

- A resposta vai **sem `Content-Length`** (`Transfer-Encoding: chunked`), porque o tamanho final só
  seria conhecido no fim. O navegador mostra os bytes já baixados, mas **não o total nem o tempo
  restante**, e a barra de progresso fica indeterminada.
- **Não há como retomar**: o download não aceita `Range`. Cair a conexão a 90% significa recomeçar.
- Se o cliente desiste (fecha a aba), a montagem para na hora — a escrita falha e a varredura
  aborta. O servidor não continua lendo disco à toa.
- Um erro no meio (arquivo removido durante a leitura, disco com problema) só pode derrubar a
  conexão, porque o cabeçalho `200` já foi enviado. O navegador mostra um download incompleto.
- Cada usuário pode ter **2 zips ao mesmo tempo**; o terceiro recebe 429 `busy`.

**Atrás de proxy**, esse é o ponto a conferir num download de horas: o tempo limite de resposta.
No nginx, `proxy_read_timeout` e `proxy_send_timeout`; no Traefik, `respondingTimeouts`; no
Cloudflare Tunnel, a conexão é mantida enquanto houver bytes fluindo, mas um HD lento com uma pausa
longa entre arquivos pode encostar no limite. O Filezam em si não impõe tempo limite de escrita.

Para arquivamentos muito grandes e repetidos, **Compactar em .zip** costuma servir melhor: gera o
arquivo no servidor em segundo plano, com progresso e cancelamento, e o download que vem depois é
de um arquivo comum — com tamanho conhecido e retomável.

## Subcomandos do binário

| Comando | Uso |
|---|---|
| `filezam serve` (padrão) | Sobe o servidor |
| `filezam healthcheck` | Sai com 0 se `/api/health` responde; usado pelo `HEALTHCHECK` da imagem |
| `filezam reset-admin [senha]` | Recria ou redefine `FILEZAM_ADMIN_USER` como admin ativo com troca obrigatória, apaga as sessões dele, zera bloqueios e **desliga o 2FA** da conta (é o caminho de recuperação de quem perdeu o autenticador e os códigos). Sem argumento usa `FILEZAM_ADMIN_PASSWORD`; sem os dois, **sorteia uma senha e a imprime** — quem chega aqui perdeu o acesso e precisa de algo que funcione agora. Prefira a variável ao argumento, que fica no histórico do shell. Com Docker: `docker compose run --rm -e FILEZAM_ADMIN_PASSWORD='NovaSenha123' filezam reset-admin` |
| `filezam version` | Imprime a versão embutida no build |

## Atualização

```bash
cd /opt/filezam && git pull
docker compose up -d --build filezam      # com imagem publicada: docker compose pull && docker compose up -d
```

Migrações do banco rodam automaticamente no início. Operações em andamento (copiar/mover/excluir) são interrompidas e ficam marcadas em **Operações**; uploads em blocos retomam pelo cliente. Uma imagem construída antes de um commit não recebe nada dele: `docker compose up -d` sem `--build` só reinicia o container antigo.

A versão do Go está fixada em `go.mod` e no `Dockerfile`; quando ela sobe (correções de segurança da biblioteca padrão), reconstrua a imagem.

## Backup

Pare o serviço e copie `/config` inteira: `filezam.db`, `filezam.db-wal`, `filezam.db-shm` **e `secret.key`** (sem a chave os segredos de 2FA não abrem e todos os usuários perdem a segunda etapa). Com o serviço rodando, o modo WAL permite copiar os três arquivos do banco juntos, mas parar é mais seguro. O banco é pequeno (KB a poucos MB). Restaurar = substituir os arquivos com o serviço parado. Os arquivos em `/data` são seus e precisam de backup próprio.

## Métricas

Com `FILEZAM_METRICS_TOKEN` definido, aponte o Prometheus para `/metrics` com `authorization: {credentials: <token>}` (ou `bearer_token`). Métricas: `filezam_http_requests_total{method,code}`, `filezam_http_request_seconds_{sum,count}`, `filezam_logins_total{result}`, `filezam_upload_bytes_total`, `filezam_jobs_finished_total{type,state}`, `filezam_users_total`, `filezam_sessions_active`, `filezam_shares_active`, `filezam_trash_items`, `filezam_trash_bytes`, `filezam_index_entries`, `filezam_index_last_scan_timestamp_seconds`, `filezam_jobs{state}`, `filezam_disk_total_bytes`, `filezam_disk_free_bytes`, `filezam_build_info{version}`. Não exponha `/metrics` sem HTTPS: o token viaja no cabeçalho.

## Sistemas de arquivos

- O processo grava como o usuário do container; arquivos criados ficam com modo `0644`/`0755`.
- Sistemas sem `fallocate` (tmpfs, overlay, alguns NFS/FUSE) usam `Truncate` na pré-alocação; sem hardlink a finalização do upload usa `Lstat + Rename`.
- Excluir, mover ou renomear pelo Filezam revoga os links do item. Para mudanças feitas por fora (Samba, SSH) o link fica amarrado ao inode do item: em sistemas que não informam inode estável (alguns FUSE/SMB) vale só o caminho, e em sistemas que reutilizam números de inode (overlayfs) um item apagado e recriado por fora no mesmo caminho pode reativar o link.
- Use um sistema de arquivos que diferencie maiúsculas em `FILEZAM_ROOT` (ext4, xfs, btrfs, zfs): em vfat/NTFS/CIFS o prefixo reservado `.filezam-` (lixeira, partes de upload) poderia ser alcançado com outra caixa.
- Caminhos têm no máximo 128 níveis; conteúdo criado por fora mais fundo que isso não é alcançável pela interface.

## Diagnóstico

Logs em JSON no stdout (`docker compose logs -f filezam` ou `journalctl -u filezam`); `FILEZAM_LOG_LEVEL=debug` registra cada requisição com método, path, status, ms e IP. Tokens de links públicos aparecem mascarados.

| Sintoma | Causa provável | Ação |
|---|---|---|
| `directory /config is not writable by uid N` em loop | bind mount criado pelo Docker como root, ou dono errado | Manter o serviço `init` no compose, ou `chown -R N:N` na pasta do host |
| `FILEZAM_DATA_DIR must not be inside FILEZAM_ROOT` | banco dentro da pasta exposta | Mover `/config` para fora de `/data` |
| Login funciona mas cai na hora | cookie `Secure` em HTTP puro, ou `auto` sem proxy confiável | `FILEZAM_SECURE_COOKIES=false` local / definir `FILEZAM_TRUSTED_PROXIES` |
| Auditoria mostra sempre o IP do proxy, ou todos os usuários com o mesmo `172.x.0.1` e 429 `rate_limited` para vários ao mesmo tempo | proxy fora de `FILEZAM_TRUSTED_PROXIES`; com proxy no host e Docker, ele chega pelo gateway da rede, não por `127.0.0.1` | Confiar só no IP do proxy; proxy no host: o gateway (`172.31.250.1`) |
| Upload falha sempre em N MB ou N segundos | limite/buffer/timeout do proxy | Aplicar a receita do proxy; reduzir `FILEZAM_MAX_UPLOAD_CHUNK` |
| 403 `csrf` em scripts | falta `X-Filezam: 1` | Adicionar o header |
| 429 `busy` em uploads | mais de 8 requisições simultâneas por usuário | Normal; o cliente reenvia com backoff |
| Link copiado com endereço errado (host/porta interna) | proxy que não repassa `Host`/`X-Forwarded-Host` de um IP confiável | A interface já usa a origem do navegador; para links gerados pela API defina `FILEZAM_PUBLIC_URL` |
| Botão "Copiar link" mostra "—" em Compartilhados | link criado antes da migração 002 (token não guardado) | Criar um link novo |
| Link público sempre 404 | expirado, revogado, item excluído/movido/renomeado pelo app (o link é revogado junto) ou recriado por fora, dono desativado ou escopo do dono estreitado | Criar novo link / reativar o usuário |
| Arquivo sumiu depois de excluir | foi para a lixeira | Menu **Lixeira** → Restaurar (itens expiram após o prazo de **Configurações do sistema**) |
| Pesquisa não acha arquivo copiado por Samba/SSH | o índice só vê mudanças feitas pelo Filezam até a próxima varredura | Aguardar a próxima varredura (intervalo em **Configurações do sistema**) ou **Reconstruir índice** na tela Pesquisar (admin) |
| Pasta `.filezam-trash` no disco | lixeira do Filezam; invisível na interface | Não apagar à mão: use Esvaziar lixeira |
| Usuário diz que a senha certa não entra | bloqueio de 15 min+ por (usuário, IP) após 10 erros; a resposta é igual à de senha errada | Esperar, ou entrar de outro endereço; ver `login.locked` na auditoria |
| Miniaturas não aparecem | formato sem decodificador (HEIC, AVIF, RAW, vídeo), imagem acima de `thumbs_max_pixels`, ou recurso desligado | É o comportamento esperado: o arquivo fica com o ícone por tipo. Conferir o interruptor em **Configurações do sistema** e a preferência pessoal em **Preferências** |
| 507 `drop_full` no link de recebimento | a cota do link acabou, ou a do dono | Criar um link novo, aumentar a cota do dono, ou esperar as sessões inacabadas caírem (2 h por padrão). A cota do link conta **tudo que já entrou**, mesmo que os arquivos tenham sido apagados depois |
| 409 `not_empty` ao criar um link de recebimento | a pasta escolhida já tem conteúdo (inclusive parte de upload) | Escolher um nome de pasta que ainda não existe; o link cria a pasta |
| 409 `slug_taken` com um endereço que você mesmo usava | o link foi revogado, e o apelido continua reservado ao dono de propósito | Liberar em **Compartilhamentos → Liberar endereço** |
| 413 `upload_reserve_exceeded` | uploads em blocos inacabados do usuário já reservam `FILEZAM_UPLOAD_MAX_RESERVED` | Concluir ou cancelar os envios pendentes (somem sozinhos após 24 h parados) ou aumentar o teto |
| 507 `quota_exceeded` | cota do usuário (Administração → Usuários) estourada | Aumentar a cota ou liberar espaço; a medição atualiza em até 30 s |
| Usuário perdeu o celular e os códigos de recuperação | 2FA sem como validar | Administração → Usuários → editar → **Redefinir 2FA** (se for o único admin: `reset-admin`, que também limpa o 2FA) |
| Código do app sempre inválido | relógio do servidor fora de hora (tolerância de ±30 s) | Sincronizar o host com NTP |
| Operação sumiu depois de reiniciar | jobs não retomam; ficam como "interrompida por reinício" em **Operações** | Refazer a operação |
| Preview de PDF em branco ou com ícone de bloqueio | build antigo (`X-Frame-Options: DENY` no conteúdo inline) | Reconstruir a imagem |
| 429 `rate_limited` ao trocar a senha | mais de 5 tentativas/min com a senha atual errada | Aguardar um minuto |
| `scope_unavailable` | pasta de escopo apagada/renomeada | Admin redefine o escopo do usuário |
| Admin sem senha | — | `docker compose exec filezam /filezam reset-admin` (imprime uma senha nova) |
