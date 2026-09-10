# Documentação do Filezam

Especificações do projeto, na ordem sugerida de leitura. Cada documento é a fonte de verdade do seu tema: ao alterar o comportamento correspondente no código, atualize o documento no mesmo commit.

| # | Documento | Conteúdo |
|---|---|---|
| 01 | [Visão geral e decisões](01-visao-geral.md) | Objetivo, requisitos, stack e registro de decisões de arquitetura (ADRs) |
| 02 | [Arquitetura](02-arquitetura.md) | Pacotes Go, fluxo de uma requisição, ciclo de vida do processo, layout do repositório |
| 03 | [Segurança](03-seguranca.md) | Modelo de ameaças, sandbox de caminhos, autenticação, CSRF, cabeçalhos, previews, links públicos, container |
| 04 | [API HTTP](04-api.md) | Todos os endpoints, formatos de requisição/resposta, códigos de erro |
| 05 | [Protocolo de upload](05-uploads.md) | Modos lote/único/chunked, sessões, retomada, conflitos, limpeza |
| 06 | [Banco de dados](06-banco-de-dados.md) | Esquema SQLite, migrações, convenções de caminho |
| 07 | [Frontend](07-frontend.md) | Estrutura React, estado, gerenciador de uploads, componentes, teclado, i18n |
| 08 | [Operação](08-operacao.md) | Deploy com Docker, variáveis, proxy reverso, backup, atualização, diagnóstico |
| 09 | [Testes](09-testes.md) | Suítes automatizadas, como rodar, checklist manual |
| 10 | [Roadmap e limitações](10-roadmap.md) | Limitações conhecidas e melhorias planejadas |

Guia rápido para quem vai desenvolver: leia 01, 02 e 03 inteiros; consulte 04 a 07 conforme a área; 08 e 09 antes de publicar uma versão. Como contribuir: [`CONTRIBUTING.md`](../CONTRIBUTING.md); como reportar uma vulnerabilidade: [`SECURITY.md`](../SECURITY.md).
