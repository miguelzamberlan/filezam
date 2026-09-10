# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs não retomam**: reiniciar o container interrompe a operação (fica registrada como interrompida em Operações); copiar cancelado ou interrompido deixa a parte já copiada no destino.
- **Shares não seguem o item**: mover/renomear invalida o link (ele volta se o item retornar ao caminho com o mesmo inode). Em sistemas de arquivos sem inode estável (alguns FUSE/SMB) vale só o caminho, e em sistemas que reutilizam números de inode (overlayfs) um item recriado no mesmo caminho pode reativar o link.
- **Cota é aproximada**: medida com cache de 30 s e ajustada por escrita; entre duas medições uma pequena ultrapassagem é possível, e sem índice pronto a varredura limitada pode subestimar árvores enormes.
- **Admin com escopo vê todos os links** (com token e caminho completo) em Compartilhados e nas propriedades: o escopo do admin não é fronteira de confidencialidade.
- **Bloqueio e limitador por (usuário, IP)**: quem divide o IP com um atacante (mesmo NAT) fica bloqueado junto por até 24 h. Em troca, não há limite de tentativas por conta somado entre IPs (seria um vetor de bloqueio remoto); a defesa contra força bruta distribuída é o custo do Argon2 (4 verificações simultâneas no processo) e a política de senha.
- **Sessão chunked reserva o nome**: `UNIQUE(dir, name)` impede outro usuário do mesmo escopo de enviar um arquivo com o mesmo nome enquanto a sessão existir (até 24 h sem atividade); só o dono pode abortá-la.
- **Pré-alocação sem cota**: uma sessão chunked reserva (`fallocate`) o tamanho declarado, limitado só pelo espaço livre quando o usuário não tem cota; um usuário autenticado consegue ocupar o disco antes de enviar um byte, por até 24 h. Defina cotas para todos os usuários em instalações com pessoas que não são de confiança.
- **Árvores muito profundas**: caminhos com mais de 128 níveis não são endereçáveis e o que passa de 256 níveis (só alcançável aninhando cadeias com `move`) fica fora do índice, da pesquisa, da cota e dos zips; copiar/excluir uma árvore dessas pelo app é lento (custo quadrático na profundidade) e conta só contra os jobs do próprio usuário.
- **Sistemas de arquivos sem distinção de maiúsculas** (vfat, NTFS, CIFS, ext4 com `casefold`): o prefixo reservado `.filezam-` é comparado byte a byte, então `.FILEZAM-trash` passaria pela normalização; use `FILEZAM_ROOT` num sistema de arquivos que diferencia maiúsculas (ext4, xfs, btrfs, zfs).
- **Lixeira sem varredura de órfãos**: uma queda entre mover para `.filezam-trash` e gravar a linha no banco deixa o item invisível e fora da cota até alguém limpar à mão.
- **Cópia através de symlink que aponta para a própria origem** (criado fora do app) entra em recursão até o cancelamento ou o disco encher; a interface nunca cria symlinks.
- **Zip público sem teto de tamanho**: um link de pasta grande permite a visitantes anônimos gerar zips enormes (2 simultâneos por IP, 120 requisições/min por IP; memória constante, custo de CPU/disco).
- **Retomada de upload exige soltar o arquivo de novo**: o navegador não guarda o `File`. Em Chrome seria possível persistir `FileSystemFileHandle` no IndexedDB.
- **Listagem paginada só na ordem do servidor**: enquanto faltam páginas, o filtro da pasta e `Ctrl+A` só alcançam o que já foi carregado.
- **Índice de nomes não vê mudanças externas** (Samba, SSH) até a próxima varredura completa ou um "Reconstruir índice".
- **Lixeira sem cota**: itens excluídos continuam ocupando disco até a retenção vencer; entre dispositivos a exclusão vira cópia + remoção.
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Pesquisa só por nome**: não indexa conteúdo.
- **Dispositivos confiáveis sem revogação individual**: desligar e religar o 2FA invalida todos de uma vez.
- **Arrastar dentro da UI** só com mouse; no toque use recortar/colar.
- **PDF no celular abre fora da interface**: navegadores móveis não mostram PDF em iframe, então o preview oferece nova aba/download. Mostrar dentro exigiria embutir o pdf.js (centenas de KB carregados sob demanda, worker próprio e revisão da CSP).

## Pendente antes de tornar o repositório público

Estado em 2026-09-10, após a revisão de segurança e documentação. As correções abaixo já estão na `main` (testadas por suíte automatizada e imagem Docker local) mas **ainda não foram validadas na máquina de produção**; o que não foi corrigido está listado como limitação acima.

Validar no servidor real (checklist completo em [09](09-testes.md)):

1. Reconstruir a imagem (`docker compose up -d --build`) e conferir o `init` novo: ele agora só troca o dono de `/config` e de `filezam.db*`/`secret.key` (nunca `-R`) e recusa rodar se `/config` tiver subpastas.
2. Login atrás do proxy real: o limitador por usuário passou a ser por (usuário, IP) e `X-Forwarded-For` lê todas as linhas do cabeçalho; conferir IP correto na Auditoria e que nenhum usuário legítimo toma `429`.
3. Ativar o 2FA numa conta: a tela pede a senha atual além do código; com 2FA ativo, "ativar" de novo deve responder `totp_already_enabled`.
4. Link público com senha: destravar, fechar e reabrir o navegador (o cookie mudou de formato e vale 24 h assinadas).
5. Upload de vários arquivos pequenos e um `PUT` grande enquanto outra aba lista a mesma pasta: nenhum upload pode falhar com 404 (carência de 15 min para partes órfãs).
6. Pesquisa e índice com uma pasta sem permissão de leitura dentro de `/data`: a varredura deve completar e a pasta aparecer sem conteúdo.
7. `filezam reset-admin` agora também desliga o 2FA do admin.
8. Zip e link público de `.filezam-trash` devem responder 400.

Decidir e, se for o caso, corrigir (achados da auditoria não corrigidos):

- **Link amarrado ao inode** não segura "apagar e recriar no mesmo caminho" em sistemas que reutilizam o número de inode (overlayfs; ext4 às vezes). Opções: gravar também o `btime` (`statx`, migração nova) ou revogar links quando o app apaga/move o item.
- **Pré-alocação de upload chunked sem cota** permite ocupar o disco inteiro antes de enviar um byte. Opções: margem de espaço livre, teto de bytes reservados por usuário, sessão sem blocos expira mais cedo.
- **Zip público sem teto de tamanho**: limitar bytes por zip anônimo ou o número global de zips públicos simultâneos.
- **Sessão chunked reserva `(dir, name)` por 24 h** para outros usuários do escopo; permitir que outra pessoa substitua uma sessão parada há minutos.
- **Lixeira sem varredura de órfãos** e **cópia através de symlink que aponta para a origem** (ver limitações acima).
- Testes unitários para `internal/config` (banco dentro da raiz via symlink) e `internal/index` (linhas velhas filtradas): os dois pacotes não têm testes.
- Fixar as imagens base do `Dockerfile` por digest para builds reproduzíveis.

No GitHub, ao publicar: ativar *Private vulnerability reporting* (Security), conferir que a CI (`.github/workflows/ci.yml`) passa, criar a tag `v1.0.0` (publica a imagem em `ghcr.io`) e tornar o pacote público.

## Próximos passos sugeridos (ordem de valor)

1. Retomada automática no Chrome com File System Access API.
2. Pesquisa por conteúdo (texto) sobre o índice.
3. Retomar jobs interrompidos por reinício a partir do histórico.
4. Chaves de acesso (WebAuthn/passkeys) como alternativa ao TOTP; revogação individual de dispositivos confiáveis.

## Feito

Lixeira com retenção, índice de nomes em SQLite, link de arquivo único e senha no link, arrastar e soltar interno, listagem paginada, interface em inglês, histórico de operações persistido, métricas Prometheus, cota de disco por usuário com limites de zips/jobs, link amarrado ao inode e bloqueio de login silencioso por (usuário, IP) e verificação em duas etapas TOTP com códigos de recuperação e dispositivo confiável (setembro de 2026).

## Ideias descartadas

- **tus** para uploads: exige blocos sequenciais e mais superfície; o protocolo próprio é menor e paralelo.
- **SSE** para progresso: exige `proxy_buffering off` e conexões abertas; polling de 500 ms é invisível e à prova de proxy.
- **Trocar a raiz pela UI**: um admin comprometido leria qualquer pasta que o container enxerga.
- **Volumes nomeados**: nascem como root e quebram o modelo `PUID:PGID`.
