# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs não retomam**: reiniciar o container interrompe a operação (fica registrada como interrompida em Operações); copiar cancelado ou interrompido deixa a parte já copiada no destino.
- **Shares não seguem o item**: mover/renomear invalida o link (ele volta se o item retornar ao caminho com o mesmo inode). Em sistemas de arquivos sem inode estável (alguns FUSE/SMB), vale só o caminho.
- **Cota é aproximada**: medida com cache de 30 s e ajustada por escrita; entre duas medições uma pequena ultrapassagem é possível, e sem índice pronto a varredura limitada pode subestimar árvores enormes.
- **Admin com escopo vê todos os links** (com token e caminho completo) em Compartilhados e nas propriedades: o escopo do admin não é fronteira de confidencialidade.
- **Bloqueio por (usuário, IP)**: quem divide o IP com um atacante (mesmo NAT) fica bloqueado junto por até 24 h.
- **Retomada de upload exige soltar o arquivo de novo**: o navegador não guarda o `File`. Em Chrome seria possível persistir `FileSystemFileHandle` no IndexedDB.
- **Listagem paginada só na ordem do servidor**: enquanto faltam páginas, o filtro da pasta e `Ctrl+A` só alcançam o que já foi carregado.
- **Índice de nomes não vê mudanças externas** (Samba, SSH) até a próxima varredura completa ou um "Reconstruir índice".
- **Lixeira sem cota**: itens excluídos continuam ocupando disco até a retenção vencer; entre dispositivos a exclusão vira cópia + remoção.
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Pesquisa só por nome**: não indexa conteúdo.
- **Dispositivos confiáveis sem revogação individual**: desligar e religar o 2FA invalida todos de uma vez.
- **Arrastar dentro da UI** só com mouse; no toque use recortar/colar.

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
