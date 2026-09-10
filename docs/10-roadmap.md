# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs em memória**: reiniciar o container perde o progresso exibido (não os dados). Copiar cancelado deixa a parte já copiada no destino.
- **Shares por caminho**: mover/renomear a pasta (ou o arquivo) compartilhada invalida o link, mas um item novo no mesmo caminho reativa um link ainda não expirado. Revogue antes de recriar.
- **Sem cotas nem limites por usuário para zips, jobs e pré-alocação de upload**: um usuário autenticado consegue ocupar o disco (a sessão de upload reserva o tamanho declarado) ou saturar I/O com muitos zips/jobs. Usuários são considerados semi-confiáveis; ver [03](03-seguranca.md).
- **Admin com escopo vê todos os links** (com token e caminho completo) em Compartilhados e nas propriedades: o escopo do admin não é fronteira de confidencialidade.
- **Bloqueio de conta revela que o usuário existe** (`423 locked`) e permite manter um usuário conhecido bloqueado com 10 tentativas por janela.
- **Retomada de upload exige soltar o arquivo de novo**: o navegador não guarda o `File`. Em Chrome seria possível persistir `FileSystemFileHandle` no IndexedDB.
- **Listagem paginada só na ordem do servidor**: enquanto faltam páginas, o filtro da pasta e `Ctrl+A` só alcançam o que já foi carregado.
- **Índice de nomes não vê mudanças externas** (Samba, SSH) até a próxima varredura completa ou um "Reconstruir índice".
- **Lixeira sem cota**: itens excluídos continuam ocupando disco até a retenção vencer; entre dispositivos a exclusão vira cópia + remoção.
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Pesquisa só por nome**: não indexa conteúdo.
- **Sem 2FA**.
- **Arrastar dentro da UI** só com mouse; no toque use recortar/colar.

## Próximos passos sugeridos (ordem de valor)

1. Retomada automática no Chrome com File System Access API.
2. Persistir jobs no SQLite para sobreviver a reinícios e permitir histórico.
3. 2FA TOTP para admins.
4. Métricas Prometheus em `/metrics` (protegido).
5. Cota de disco por usuário e limite de zips/jobs simultâneos; link público amarrado ao inode em vez do caminho.
6. Pesquisa por conteúdo (texto) sobre o índice.

## Feito

Lixeira com retenção, índice de nomes em SQLite, link de arquivo único e senha no link, arrastar e soltar interno, listagem paginada e interface em inglês (setembro de 2026).

## Ideias descartadas

- **tus** para uploads: exige blocos sequenciais e mais superfície; o protocolo próprio é menor e paralelo.
- **SSE** para progresso: exige `proxy_buffering off` e conexões abertas; polling de 500 ms é invisível e à prova de proxy.
- **Trocar a raiz pela UI**: um admin comprometido leria qualquer pasta que o container enxerga.
- **Volumes nomeados**: nascem como root e quebram o modelo `PUID:PGID`.
