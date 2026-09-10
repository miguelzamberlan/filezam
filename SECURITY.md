# Política de segurança

O Filezam expõe arquivos de um servidor na web, então segurança é a prioridade número um do projeto. O modelo de ameaças, os controles implementados e as limitações aceitas estão em [`docs/03-seguranca.md`](docs/03-seguranca.md).

## Relatando uma vulnerabilidade

**Não abra uma issue pública** para falhas de segurança. Use o recurso *Report a vulnerability* na aba **Security** deste repositório no GitHub (Security Advisories privados). Descreva o cenário, a versão afetada e, se possível, como reproduzir.

O que esperar:

- Resposta inicial em até 7 dias.
- Correção publicada em uma nova versão, com crédito a quem reportou (se desejar).
- Falhas que permitam ler ou escrever fora da pasta exposta, executar script no navegador de outro usuário ou usar sessões/links de terceiros são tratadas como críticas.

## Versões suportadas

Apenas a versão mais recente da branch `main` (ou a última tag publicada) recebe correções.

## Boas práticas para quem hospeda

- Sempre atrás de um proxy reverso com HTTPS e `FILEZAM_TRUSTED_PROXIES` configurado.
- Troque a senha do admin no primeiro acesso (o app exige) e não use `PUID=0`.
- Mantenha a pasta do banco (`/config`) fora da pasta exposta (`/data`); o app recusa iniciar se estiverem aninhadas.
- Reconstrua a imagem a cada atualização: as correções de segurança da biblioteca padrão do Go entram quando a versão em `go.mod`/`Dockerfile` sobe.
