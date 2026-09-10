# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs não retomam**: reiniciar o container interrompe a operação (fica registrada como interrompida em Operações); copiar cancelado ou interrompido deixa a parte já copiada no destino.
- **Links não seguem o item**: excluir, mover ou renomear pelo app revoga o link (e os de tudo que estiver dentro); restaurar da lixeira não o traz de volta. Mudanças feitas por fora (Samba, SSH) dependem do inode: movido por fora, o link para de responder e volta se o item retornar ao caminho com o mesmo inode; em sistemas sem inode estável (alguns FUSE/SMB) vale só o caminho, e em sistemas que reutilizam números de inode (overlayfs) um item apagado e recriado **por fora** no mesmo caminho pode reativar o link.
- **Cota é aproximada**: medida com cache de 30 s e ajustada por escrita; entre duas medições uma pequena ultrapassagem é possível, e sem índice pronto a varredura limitada pode subestimar árvores enormes.
- **Admin com escopo vê todos os links** (com token e caminho completo) em Compartilhados e nas propriedades: o escopo do admin não é fronteira de confidencialidade.
- **Bloqueio e limitador por (usuário, IP)**: quem divide o IP com um atacante (mesmo NAT) fica bloqueado junto por até 24 h. Em troca, não há limite de tentativas por conta somado entre IPs (seria um vetor de bloqueio remoto); a defesa contra força bruta distribuída é o custo do Argon2 (4 verificações simultâneas no processo) e a política de senha.
- **Reserva de upload**: as sessões chunked abertas de um usuário reservam no disco (`fallocate`) até `FILEZAM_UPLOAD_MAX_RESERVED` (100 GiB por padrão) por até 24 h sem atividade. Vários usuários sem cota ainda podem, somados, encher o disco: defina cotas em instalações com pessoas que não são de confiança. Um único arquivo maior que o teto exige subir o valor.
- **Árvores muito profundas**: caminhos com mais de 128 níveis não são endereçáveis e o que passa de 256 níveis (só alcançável aninhando cadeias com `move`) fica fora do índice, da pesquisa, da cota e dos zips; copiar/excluir uma árvore dessas pelo app é lento (custo quadrático na profundidade) e conta só contra os jobs do próprio usuário.
- **Sistemas de arquivos sem distinção de maiúsculas** (vfat, NTFS, CIFS, ext4 com `casefold`): o prefixo reservado `.filezam-` é comparado byte a byte, então `.FILEZAM-trash` passaria pela normalização; use `FILEZAM_ROOT` num sistema de arquivos que diferencia maiúsculas (ext4, xfs, btrfs, zfs).
- **Queda durante a exclusão para a lixeira** pode deixar uma linha sem item (restaurar responde "não encontrado" e a remove; a retenção também) ou, entre dispositivos, uma cópia parcial visível na lixeira. Nunca fica item escondido sem linha.
- **Zip público sem teto de tamanho**: no máximo 4 zips públicos ao mesmo tempo no servidor e 2 downloads por IP; um link de pasta grande ainda permite zips enormes, um por vaga (memória constante, custo de CPU/disco).
- **Retomada de upload só soltando o mesmo arquivo de novo** (até 24 h): o navegador não guarda o `File`, e a retomada automática foi descartada (ver Decisões).
- **Listagem paginada só na ordem do servidor**: enquanto faltam páginas, o filtro da pasta e `Ctrl+A` só alcançam o que já foi carregado.
- **Índice de nomes não vê mudanças externas** (Samba, SSH) até a próxima varredura completa ou um "Reconstruir índice".
- **Lixeira sem cota**: itens excluídos continuam ocupando disco até a retenção vencer; entre dispositivos a exclusão vira cópia + remoção.
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Pesquisa só por nome**: o conteúdo dos arquivos não é pesquisado (decisão, ver abaixo).
- **Dispositivos confiáveis sem revogação individual**: desligar e religar o 2FA invalida todos de uma vez.
- **Arrastar dentro da UI** só com mouse; no toque use recortar/colar.
- **PDF no celular abre fora da interface**: navegadores móveis não mostram PDF em iframe, então o preview oferece nova aba/download. Mostrar dentro exigiria embutir o pdf.js (centenas de KB carregados sob demanda, worker próprio e revisão da CSP).

## Versão 1.0.0

Lançada em 2026-09-10, primeira versão pública ([CHANGELOG](../CHANGELOG.md)). Versões publicadas não mudam: pedidos de alteração chegam por issue e entram numa versão nova, conforme a política de [`CONTRIBUTING.md`](../CONTRIBUTING.md#versões-e-lançamentos).

Antes do lançamento houve uma revisão de segurança, e as pendências da auditoria foram resolvidas, todas com teste automatizado ([09](09-testes.md)):

- revogação de links ao excluir, mover ou renomear;
- teto de bytes reservados por upload;
- sessão chunked por usuário;
- teto global de zips públicos;
- lixeira sem órfãos;
- cópia sem recursão por symlink;
- downloads públicos que esperam a vaga;
- proxies com sub-rede fixa.

A versão foi validada no servidor de produção (Docker atrás de Cloudflare Tunnel, dados num HD NTFS montado com `ntfs3`), com a imagem em produção e uma instância de homologação da mesma imagem sobre o mesmo HD:

1. `init` do compose: pastas novas entregues ao `PUID`; `/config` com subpasta recusado (exit 1); só `filezam.db*`/`secret.key` trocam de dono, outros arquivos e um `/data` já povoado ficam intactos. Volumes nomeados sobem com o usuário 65532 da imagem.
2. Pelo túnel, um `X-Forwarded-For` forjado (duas linhas) é ignorado e a Auditoria grava o IP público real; nenhum `*.ratelimited` no histórico. **Achado e corrigido**: num link público, um Markdown com várias imagens perdia 1–2 imagens com `429 busy` (limite de 2 downloads por IP); agora o excedente espera o slot (até 30 s). Também apareceu que porta publicada + sub-rede Docker inteira confiável permite forjar o IP a partir do próprio servidor (resolvido com a sub-rede fixa e a confiança só no IP do proxy).
3. 2FA: ativar sem senha ou com senha errada → 401 `bad_credentials`, código errado → `bad_totp`; `setup`/`enable` com 2FA ativo → 409 `totp_already_enabled`; anti-replay e código de recuperação funcionam.
4. Link com senha: cookie `fz_s_*` persistente (24 h, `HttpOnly`, `SameSite=Strict`); Chrome fechado e reaberto com o mesmo perfil abre a pasta sem pedir senha; validade ou assinatura adulteradas e o formato antigo são recusados.
5. PUT lento (20 s), dois PUT de 30 MiB, 20 PUT pequenos, 4 lotes de 40 e um chunked de 70 MiB enquanto 3 clientes listavam a pasta (11 mil listagens): nenhum 404, sha256 confere no disco, nenhuma parte sobra; parte sem sessão parada há 16 min é removida, há 14 min fica.
6. Pasta `chmod 000` no NTFS: varredura completa, pasta aparece na listagem, abri-la responde 403 `fs_permission`, conteúdo fora da pesquisa. A produção indexou as ~42 mil entradas do HD sem erro.
7. `filezam reset-admin` limpa o 2FA, invalida a senha antiga e exige troca.
8. Zip, link, favorito e listagem de `.filezam-trash` e de partes `.filezam-upload-*` → 400 `invalid_path`.
9. As correções da auditoria (links, sessão por usuário, teto de reserva, cópia via symlink real no NTFS, lixeira) foram conferidas na homologação sobre o mesmo HD. Em produção, a migração 010 subiu sem perder dados. Proxy com IP fixo: pelo túnel vale o IP público real, pela rede local o IP da máquina, e ninguém forja.

Não verificado manualmente antes do lançamento: o item 15 do checklist de [09](09-testes.md), o 2FA com um app autenticador real no celular. O fluxo inteiro está coberto por `TestTOTPFlow` e pelos vetores do RFC 4226.

## Próximos passos sugeridos (ordem de valor)

1. Retomar jobs interrompidos por reinício a partir do histórico.
2. Chaves de acesso (WebAuthn/passkeys) como alternativa ao TOTP; revogação individual de dispositivos confiáveis.

## Feito

Lixeira com retenção, índice de nomes em SQLite, link de arquivo único e senha no link, arrastar e soltar interno, listagem paginada, interface em inglês, histórico de operações persistido, métricas Prometheus, cota de disco por usuário com limites de zips/jobs, link amarrado ao inode e bloqueio de login silencioso por (usuário, IP) e verificação em duas etapas TOTP com códigos de recuperação e dispositivo confiável (setembro de 2026). Versão 1.0.0 pública (2026-09-10): revisão de segurança, validação em produção e pendências da auditoria resolvidas.

## Decisões e ideias descartadas

- **Pesquisa por conteúdo**: não será feita. A pesquisa é por nome, sobre o índice. Indexar conteúdo exigiria extrair texto de PDF/Office/imagens (parsers de formatos arbitrários rodando sobre arquivos de terceiros), muito mais espaço no banco e reindexação a cada escrita.
- **Retomada automática de upload** (persistir `FileSystemFileHandle` no IndexedDB, só no Chrome): não será feita. Um upload interrompido é descartado e recomeça; fica só o que já existe: soltar o mesmo arquivo de novo (mesmo nome, tamanho e data) retoma a sessão pendente enquanto ela existir (até 24 h sem atividade).
- **Teto de bytes por zip público**: trocado pelo teto de zips públicos simultâneos, que é mais simples e ainda permite o zip grande legítimo.
- **`btime` (`statx`) no link**: trocado pela revogação quando o app exclui/move/renomeia. Não precisa de migração e vale em qualquer sistema de arquivos para o que passa pelo app.
- **Substituir a sessão chunked parada de outro usuário**: trocado pela sessão por (usuário, destino), em que ninguém interfere no envio alheio.
- **Link que segue o item movido**: o link publicaria um caminho que quem o criou não escolheu.
- **tus** para uploads: exige blocos sequenciais e mais superfície; o protocolo próprio é menor e paralelo.
- **SSE** para progresso: exige `proxy_buffering off` e conexões abertas; polling de 500 ms é invisível e à prova de proxy.
- **Trocar a raiz pela UI**: um admin comprometido leria qualquer pasta que o container enxerga.
- **Volumes nomeados**: nascem como root e quebram o modelo `PUID:PGID`.
