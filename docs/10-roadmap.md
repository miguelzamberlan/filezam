# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs em memória**: reiniciar o container perde o progresso exibido (não os dados). Copiar cancelado deixa a parte já copiada no destino.
- **Shares por caminho**: mover/renomear a pasta compartilhada invalida o link, mas uma pasta nova no mesmo caminho reativa um link ainda não expirado. Revogue antes de recriar.
- **Sem cotas nem limites por usuário para zips, jobs e pré-alocação de upload**: um usuário autenticado consegue ocupar o disco (a sessão de upload reserva o tamanho declarado) ou saturar I/O com muitos zips/jobs. Usuários são considerados semi-confiáveis; ver [03](03-seguranca.md).
- **Admin com escopo vê todos os links** (com token e caminho completo) em Compartilhados e nas propriedades: o escopo do admin não é fronteira de confidencialidade.
- **Bloqueio de conta revela que o usuário existe** (`423 locked`) e permite manter um usuário conhecido bloqueado com 10 tentativas por janela.
- **Retomada de upload exige soltar o arquivo de novo**: o navegador não guarda o `File`. Em Chrome seria possível persistir `FileSystemFileHandle` no IndexedDB.
- **Listagem sem paginação**: pastas com centenas de milhares de entradas geram um JSON grande (gzip ajuda; a UI é virtualizada).
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Sem lixeira**: exclusão é definitiva (com confirmação).
- **Pesquisa só por nome**: não indexa conteúdo; cada pesquisa percorre o disco (limites de 200 000 entradas / 10 s).
- **Uma pasta por link**: não há compartilhamento de arquivo único nem link com senha.
- **Sem 2FA**.
- **Arrastar dentro da UI** (mover soltando linha sobre pasta) não implementado; só copiar/recortar/colar.

## Próximos passos sugeridos (ordem de valor)

1. Lixeira (`.filezam-trash/` fora da listagem, esvaziamento programado) — reduz o risco de exclusão acidental.
2. Índice de nomes em SQLite (atualizado por jobs/uploads) para pesquisar árvores enormes sem percorrer o disco.
3. Share de arquivo único e senha opcional no link.
4. Arrastar e soltar interno para mover.
5. Paginação/streaming da listagem acima de N entradas.
6. Retomada automática no Chrome com File System Access API.
7. Persistir jobs no SQLite para sobreviver a reinícios e permitir histórico.
8. 2FA TOTP para admins.
9. Métricas Prometheus em `/metrics` (protegido).
9b. Cota de disco por usuário e limite de zips/jobs simultâneos; link público amarrado ao inode da pasta em vez do caminho.
10. Segundo idioma (EN) reaproveitando `strings.ts`.

## Ideias descartadas

- **tus** para uploads: exige blocos sequenciais e mais superfície; o protocolo próprio é menor e paralelo.
- **SSE** para progresso: exige `proxy_buffering off` e conexões abertas; polling de 500 ms é invisível e à prova de proxy.
- **Trocar a raiz pela UI**: um admin comprometido leria qualquer pasta que o container enxerga.
- **Volumes nomeados**: nascem como root e quebram o modelo `PUID:PGID`.
