# 10 · Roadmap e limitações conhecidas

## Limitações atuais

- **Jobs em memória**: reiniciar o container perde o progresso exibido (não os dados). Copiar cancelado deixa a parte já copiada no destino.
- **Shares por caminho**: mover/renomear a pasta compartilhada invalida o link.
- **Retomada de upload exige soltar o arquivo de novo**: o navegador não guarda o `File`. Em Chrome seria possível persistir `FileSystemFileHandle` no IndexedDB.
- **Listagem sem paginação**: pastas com centenas de milhares de entradas geram um JSON grande (gzip ajuda; a UI é virtualizada).
- **Move com "substituir" entre pastas** faz merge quando ambos são pastas (copia + apaga origem); entre dispositivos vira cópia + exclusão sem progresso de bytes.
- **Nomes com UTF-8 inválido** aparecem em vermelho e não podem ser manipulados.
- **Sem lixeira**: exclusão é definitiva (com confirmação).
- **Sem busca recursiva**: o filtro é só na pasta atual.
- **Uma pasta por link**: não há compartilhamento de arquivo único nem link com senha.
- **Sem 2FA**.
- **Arrastar dentro da UI** (mover soltando linha sobre pasta) não implementado; só copiar/recortar/colar.

## Próximos passos sugeridos (ordem de valor)

1. Lixeira (`.filezam-trash/` fora da listagem, esvaziamento programado) — reduz o risco de exclusão acidental.
2. Busca recursiva por nome com limite de tempo/entradas.
3. Share de arquivo único e senha opcional no link.
4. Arrastar e soltar interno para mover.
5. Paginação/streaming da listagem acima de N entradas.
6. Retomada automática no Chrome com File System Access API.
7. Persistir jobs no SQLite para sobreviver a reinícios e permitir histórico.
8. 2FA TOTP para admins.
9. Métricas Prometheus em `/metrics` (protegido).
10. Segundo idioma (EN) reaproveitando `strings.ts`.

## Ideias descartadas

- **tus** para uploads: exige blocos sequenciais e mais superfície; o protocolo próprio é menor e paralelo.
- **SSE** para progresso: exige `proxy_buffering off` e conexões abertas; polling de 500 ms é invisível e à prova de proxy.
- **Trocar a raiz pela UI**: um admin comprometido leria qualquer pasta que o container enxerga.
- **Volumes nomeados**: nascem como root e quebram o modelo `PUID:PGID`.
