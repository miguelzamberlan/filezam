# Changelog

Todas as mudanças relevantes do Filezam ficam registradas aqui. O formato segue o [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto usa [versionamento semântico](https://semver.org/lang/pt-BR/): `MAJOR.MINOR.PATCH`. Como cada número é escolhido e como uma versão é publicada está em [`CONTRIBUTING.md`](CONTRIBUTING.md#versões-e-lançamentos).

## [Não lançado]

### Alterado
- **Pesquisa sem acentos nem maiúsculas**: "acao" acha "Relatório de Ação.pdf", "BALANÇO" acha "balanco.xlsx", e nomes gravados pelo macOS casam com os do Windows. Vale para a pesquisa, para o filtro da pasta e para a digitação que salta para um item. O índice existente é convertido sozinho na atualização, sem esperar a próxima varredura.

## [1.2.0] - 2026-09-13

### Adicionado
- **Notificações**: novo item no menu, com contador, que avisa quando chegam arquivos por um link de recebimento — vários envios pelo mesmo link viram um aviso só ("12 arquivos recebidos pelo link…"), com atalho para abrir a pasta — e quando um link é revogado porque o item sumiu. Com o Filezam aberto, o aviso aparece na hora. A estrutura é genérica e serve para outros tipos de aviso no futuro.
- **Download de pastas grandes em partes**: baixar uma pasta ou seleção acima de 2 GB abre uma janela com o download dividido em partes de até 2 GB, cada uma um `.zip` completo que abre sozinho — como o Google Drive faz. Uma parte que falhar é baixada de novo sozinha, sem recomeçar tudo. Vale também nos links públicos.
- **Prazo da lixeira e frequência da varredura do índice em Configurações do sistema**: o administrador escolhe por quanto tempo os itens excluídos ficam na lixeira (até 365 dias, ou desligada) e de quanto em quanto tempo o índice de pesquisa relê o disco para enxergar mudanças feitas por fora (15 minutos a 7 dias), com o aviso do custo, a duração da última varredura e um botão **Varrer agora** para enxergar na hora o que mudou por fora. `FILEZAM_TRASH_RETENTION` e `FILEZAM_INDEX_INTERVAL` passam a ser só o valor inicial.
- **Cópia interrompida não deixa arquivo pela metade**: cada arquivo é gravado num temporário oculto e só aparece com o nome certo quando termina. Cancelada, ou cortada por um reinício do servidor, a cópia de um único arquivo não deixa nada no destino; a de vários mantém os que terminaram e **Operações** avisa que a cópia foi parcial. Extração e compactação interrompidas por um reinício também são apagadas. Substituir um arquivo não destrói mais o original antes de a cópia nova estar inteira.
- **Exclusão interrompida por um reinício é concluída**: se o servidor cair no meio de mandar algo para a lixeira, a manutenção termina o serviço na subida seguinte — o item vai inteiro para a lixeira e some da pasta — em vez de deixar uma entrada vazia na lixeira ou uma cópia pela metade. Uma falha no meio de uma exclusão entre discos desfaz a cópia parcial, e o item continua no lugar.
- **Links que perderam o item são limpos**: quando uma pasta ou arquivo compartilhado é apagado, movido ou trocado fora do Filezam (Samba, SSH), a tela **Compartilhamentos** mostra que o item não foi encontrado e quando o link será revogado; se ele não voltar ao lugar em 24 horas, o link é revogado. Se muitos links quebram ao mesmo tempo (um disco que não montou, por exemplo), nada é revogado.
- **Extrair um `.zip` enorme não pesa mais no servidor**: o tamanho do índice do arquivo e o total que ele diz conter são conferidos antes de abri-lo, e um arquivo que passaria dos limites é recusado na hora, com a orientação de baixar e descompactar no computador — inclusive os montados para enganar o leitor com um índice que mente o próprio tamanho.

### Alterado
- **Minha conta reúne tudo o que é seu**: senha, verificação em duas etapas, idioma, tema e cores ficam numa página só, com as abas Preferências e Senha e segurança. O rodapé do menu troca os botões Conta e engrenagem por um cartão com o seu nome, ao lado do Sair.
- Ao criar um **link de recebimento**, o diálogo explica que o limite conta tudo o que já entrou pelo link: apagar ou mover os arquivos recebidos não devolve espaço, e um link esgotado é substituído por um novo.
- **Ctrl+A numa pasta muito grande** que ainda não carregou inteira avisa que só o que já apareceu foi selecionado, e explica como alcançar o resto: rolar até o fim para excluir, mover ou copiar tudo, ou usar **Baixar** sem seleção para levar a pasta inteira.
- **Aviso antes de derrubar um link público**: excluir, mover ou renomear um item compartilhado (ou uma pasta com algo compartilhado dentro) agora pede confirmação e lista os links que vão deixar de funcionar. O link continua sendo revogado — ele não acompanha o item, de propósito —, mas ninguém mais é pego de surpresa; é preciso criar um link novo.

### Corrigido
- **Nomes com ã, õ e maiúsculas acentuadas em zips do Windows**: um `.zip` feito pelo "Enviar para > Pasta compactada" ou pelo 7-Zip num Windows em português extraía "São Paulo" como "S╞o Paulo" e "Configurações" como "ConfiguraçΣes". Os nomes agora são lidos na code page certa (850).
- **Atalhos de teclado depois de fechar o visualizador com o mouse**: abrir um arquivo, fechar pelo X (ou clicando fora) e apertar `Delete` não fazia nada, embora o arquivo continuasse selecionado — era preciso clicar nele de novo. O mesmo acontecia depois de fechar um diálogo, como o de renomear. Agora as teclas voltam a valer para a lista na hora.

## [1.1.1] - 2026-09-13

### Alterado
- **Extrair um `.zip` protegido por senha** agora avisa na hora que arquivos com senha não são suportados, em vez de terminar com uma pasta só de subpastas vazias e um aviso por arquivo. Um `.zip` corrompido também é recusado logo ao pedir a extração. Num zip em que só parte dos arquivos tem senha, o resto continua sendo extraído e o aviso diz quantos foram pulados por estarem protegidos.
- Documentação de **como expor várias pastas do host**: a resposta estava numa célula de tabela e não dizia o que muda com mais de uma montagem. Agora é uma seção própria em `docs/08-operacao.md`, resumida no README, com o exemplo de `volumes` e as ressalvas — dono das pastas extras (o serviço `init` não mexe nelas), mover e excluir entre sistemas de arquivos virando cópia + remoção, espaço livre por escopo e symlink recusado pelo `os.Root`.
- O **README em inglês** deixa de ser um resumo e passa a ter as mesmas seções do português: sumário, funcionalidades completas, os quatro cenários de instalação com os blocos de configuração, pastas e permissões, como usar (usuários, links públicos, links de recebimento, uploads, interface e atalhos), documentação, contribuição e roadmap.

### Corrigido
- **Passar fotos com as setas** no visualizador não deixa mais a foto anterior na tela enquanto a próxima carrega — o nome mudava no topo e parecia que nada tinha acontecido. Agora a foto nova começa pela miniatura desfocada, com um indicador de carregamento, e a anterior e a próxima de cada foto já ficam pré-carregadas, então avançar uma a uma é instantâneo.

## [1.1.0] - 2026-09-12

### Adicionado
- **Miniaturas das imagens**: pastas de fotos mostram uma prévia de cada imagem em vez do ícone genérico, na lista e na grade. A miniatura é gerada na primeira vez que a pasta é aberta e reusada depois, então a segunda visita é instantânea. Formatos: JPEG, PNG, GIF, WebP, BMP e TIFF. Pode ser desligado pelo administrador para toda a instalação, e por cada pessoa nas próprias preferências.
- **Extrair e compactar**: `Extrair aqui` no menu de contexto de um `.zip` descompacta para uma pasta nova ao lado, sem sobrescrever nada — extrair duas vezes cria `fotos` e `fotos (1)`. `Compactar em .zip` faz o contrário, com os arquivos selecionados. Os dois rodam em segundo plano, com progresso e cancelamento.
- **Editor de texto e markdown**: corrija um `.txt`, `.md` ou arquivo de configuração direto no navegador, sem baixar e reenviar. Arquivos `.md` mostram a prévia formatada ao lado enquanto você digita. Se alguém salvar o mesmo arquivo enquanto você edita, o Filezam avisa e **não** apaga o trabalho da outra pessoa — o seu texto continua na tela para você copiar.
- **Endereço personalizado no link público**: em vez do token aleatório, o link pode ter um apelido escolhido por quem o cria (`/s/orcamento-2026`), único em toda a instalação. Como um apelido é fácil de adivinhar, ele **exige senha** — o sigilo do link passa a morar nela. Revogar um link com apelido não devolve o endereço para outras pessoas: ele continua reservado a quem o criou, até ser liberado de propósito em **Compartilhamentos**.
- **Link público para receber arquivos**: uma caixa de entrada que qualquer pessoa, sem conta, usa para enviar arquivos para uma pasta sua. A pasta é criada na hora e precisa estar vazia; cota e vencimento (no máximo 30 dias) são obrigatórios. Quem envia não lista nem baixa nada — vê apenas os próprios envios — e nenhum arquivo existente é sobrescrito: um nome repetido vira `nome (1).ext`.
- **Administração → Configurações do sistema**: liga e desliga os quatro recursos opcionais (endereço personalizado, link de recebimento, extrair/compactar, miniaturas) para todos os usuários e define os tetos dos links de recebimento (cota, validade, tamanho por arquivo, número de arquivos, links por usuário). O link de recebimento vem **desligado** por padrão.

### Alterado
- **Subir pela primeira vez não precisa mais de `.env`**: `git clone` e `docker compose up -d --build` bastam, porque todo valor já tinha padrão e o compose lê o arquivo só se ele existir. O `.env` continua sendo o lugar de configurar para valer (usuário do host, proxy, URL pública), e não é mais passo obrigatório do primeiro contato.
- **Configurações do sistema** ganha o interruptor de extrair/compactar, que existia no banco e na API mas não tinha controle na tela.
- Documentação de onde guardar os dados em Windows e macOS, e do que muda nos nomes de arquivo entre NTFS, APFS e ext4 (`docs/08-operacao.md`).
- Um link público protegido por senha não revela mais o nome do item enquanto não for destravado.
- Desligar um recurso em **Configurações do sistema** vale também para o que já existe — links no ar, item de menu — e não só para a criação de novos.

### Corrigido
- No link de recebimento, **um envio lento prendia os outros**: cada arquivo esperava o anterior terminar de subir por inteiro, o que em conexão doméstica e atrás de proxy estourava o tempo limite e aparecia como "erro interno no servidor". A fila agora só existe para as contas de cota e para escolher o nome, que levam milissegundos.
- Envio interrompido por rede deixou de aparecer como "erro interno no servidor": agora diz que a transferência parou no meio, e o cliente tenta de novo sozinho.
- Clicar com o botão direito com **vários itens selecionados** deixava a tela em branco. Vinha da 1.0.0.
- No link de recebimento, os primeiros arquivos enviados podiam **não aparecer na lista** de quem enviou. A identidade do visitante só era criada na primeira gravação, e dois envios simultâneos — o comportamento normal — geravam duas identidades, perdendo o rastro de uma delas.
- Ao enviar uma **pasta**, a listagem aberta não mostrava o conteúdo novo até recarregar a página: o aviso de mudança ia só para a subpasta de destino, não para a pasta que estava na tela.
- O campo de senha dizia "opcional" mesmo quando um endereço personalizado a tornava obrigatória.

### Segurança
- **A senha do primeiro administrador deixa de ser `admin`.** Sem `FILEZAM_ADMIN_PASSWORD`, o Filezam sorteia uma senha e a mostra uma única vez no log (`docker compose logs filezam`). A troca no primeiro acesso continua obrigatória. O padrão fixo valia do momento em que o serviço subia até alguém entrar pela primeira vez — e é a primeira coisa que qualquer varredura tenta. `filezam reset-admin` sem argumento nem variável também sorteia e imprime, em vez de falhar.
- A senha de um link público passa a exigir **8 caracteres**, o mesmo mínimo das senhas de conta. Num link com endereço personalizado ela é o único segredo, porque o endereço é escolhido para ser fácil de dizer — e de adivinhar.
- Duas sessões de envio abertas no mesmo instante no mesmo link não reservam mais espaço além da cota: conferir a cota e criar a sessão viraram uma operação só.
- Um arquivo recebido por link público é registrado mesmo que quem enviou feche a aba no exato momento em que a transferência termina. Antes, o arquivo podia ficar no disco sem entrar na conta do link.
- Nomes de arquivo com caracteres de inversão de texto (U+202A–U+202E, U+2066–U+2069) são recusados: eles fazem `nota‮gpj.exe` aparecer na tela como `notaexe.jpg`, dando a um executável cara de foto. As marcas de direção usadas em árabe e hebraico continuam valendo.
- As miniaturas ficam cinco minutos no cache do navegador, e não sete dias: é conteúdo do usuário, e o que o navegador guarda em disco sobrevive ao logout. Passado esse tempo a imagem é revalidada sem ser transferida de novo.

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

[Não lançado]: https://github.com/miguelzamberlan/filezam/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/miguelzamberlan/filezam/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/miguelzamberlan/filezam/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/miguelzamberlan/filezam/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/miguelzamberlan/filezam/releases/tag/v1.0.0
