# Changelog

Todas as mudanças relevantes do Filezam ficam registradas aqui. O formato segue o [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto usa [versionamento semântico](https://semver.org/lang/pt-BR/): `MAJOR.MINOR.PATCH`. Como cada número é escolhido e como uma versão é publicada está em [`CONTRIBUTING.md`](CONTRIBUTING.md#versões-e-lançamentos).

## [Não lançado]

Nada ainda.

## [1.0.0] - 2026-09-10

Primeira versão pública.

### Arquivos
- Uma pasta do servidor (`FILEZAM_ROOT`) exposta pelo navegador. Cada usuário enxerga só a pasta de acesso (escopo) definida pelo admin.
- Listagem paginada com ordenação natural, filtro, zoom, grade/lista e seleção por teclado e mouse. Arrastar e soltar dentro da interface. Tema claro/escuro e interface em português e inglês.
- Copiar, mover e excluir rodam em segundo plano, com progresso, cancelamento e histórico em **Operações**. Renomear e criar pastas. Download de arquivo ou de várias entradas em ZIP streaming.
- Preview de imagem, vídeo, áudio, texto, PDF (no celular abre em nova aba ou baixa) e Markdown formatado com imagens relativas. Conteúdo enviado por usuários nunca é servido como HTML.
- Lixeira com retenção configurável e restauração ao caminho original.
- Pesquisa por nome sobre um índice em SQLite, com varredura periódica e reconstrução pelo admin.
- Favoritos e propriedades (tamanho de pasta, links do item).

### Uploads
- Arquivos pequenos em lote e grandes em blocos paralelos e fora de ordem, pensados para passar por proxies com limite por requisição (Cloudflare).
- Retomada soltando o mesmo arquivo de novo enquanto a sessão existir (24 h sem atividade).
- Pré-alocação do tamanho declarado, com teto de bytes reservados por usuário (`FILEZAM_UPLOAD_MAX_RESERVED`).
- Conflitos resolvidos na hora: substituir, manter os dois ou pular, com a opção de aplicar a todos.

### Compartilhamento
- Links públicos somente leitura de pasta ou de arquivo, com validade e senha opcional.
- O link é revogado quando o item é excluído, movido ou renomeado pelo app. Também fica amarrado ao inode, então item recriado por fora não herda o link.
- Limites para visitantes anônimos: por IP, por link e no total de zips públicos simultâneos.

### Contas e segurança
- Usuários com papel (admin/usuário), escopo, cota de disco e desativação. A troca da senha inicial é obrigatória.
- Senhas com Argon2id. Bloqueio silencioso por (usuário, IP) e limites de tentativa.
- Verificação em duas etapas TOTP com códigos de recuperação e dispositivo confiável. O admin pode ser obrigado a ativá-la (`FILEZAM_REQUIRE_2FA_ADMINS`), e `filezam reset-admin` recupera o acesso.
- Sandbox de caminhos sobre `os.Root`: symlinks nunca escapam da raiz, e nomes reservados (`.filezam-*`) não são endereçáveis. Proteção CSRF, CSP e cabeçalhos de segurança.
- `X-Forwarded-*` aceitos só de proxies confiáveis. Guia de configuração para porta local e proxies com a gravidade de cada combinação.
- Auditoria com retenção de 180 dias e métricas Prometheus opcionais.

### Operação
- Binário Go único e estático com a interface embutida, SQLite sem CGO e migrações automáticas.
- Imagem Docker distroless sem root, multi-arquitetura (`linux/amd64`, `linux/arm64`) em `ghcr.io/miguelzamberlan/filezam`, com imagens base fixadas por digest.
- `docker-compose.yml` de referência com serviço `init` para permissões, sub-rede fixa e contêiner somente leitura sem capabilities.
- Documentação completa em `docs/`: arquitetura, segurança, API, uploads, banco, frontend, operação (Docker, Easypanel, systemd, proxies), testes e roadmap.

[Não lançado]: https://github.com/miguelzamberlan/filezam/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/miguelzamberlan/filezam/releases/tag/v1.0.0
