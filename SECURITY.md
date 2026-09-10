# Política de segurança

O Filezam expõe arquivos de um servidor na web, então segurança é a prioridade número um do projeto. O modelo de ameaças, os controles implementados e as limitações aceitas estão em [`docs/03-seguranca.md`](docs/03-seguranca.md).

## Relatando uma vulnerabilidade

**Não abra uma issue pública** para falhas de segurança. Use *Report a vulnerability* na aba **Security** do repositório no GitHub ([Security Advisory privado](https://github.com/miguelzamberlan/filezam/security/advisories/new)). Descreva o cenário, a versão afetada (`filezam version` ou o campo `version` de `/api/health`) e, se possível, como reproduzir.

O que esperar:

- Resposta inicial em até 7 dias.
- Correção publicada em uma nova versão, com crédito a quem reportou (se desejar), e divulgação coordenada depois da correção.
- São tratadas como **críticas** falhas que permitam ler ou escrever fora da pasta exposta, executar script no navegador de outro usuário, usar sessões ou links de terceiros, ou contornar a autenticação e o 2FA.

Fora do escopo: ataques que exigem acesso ao host ou ao banco (`/config`), ao proxy reverso ou a um administrador; negação de serviço por volume de tráfego (o rate limit existe para força bruta, não para DDoS); e o que está listado como limitação aceita em `docs/03` e `docs/10`.

## Versões suportadas

Apenas a versão mais recente (`main` ou a última tag publicada) recebe correções.

## Boas práticas para quem hospeda

- Sempre atrás de um proxy reverso com HTTPS, com `FILEZAM_TRUSTED_PROXIES` apontando para ele e `FILEZAM_SECURE_COOKIES=true`.
- Troque a senha do admin no primeiro acesso (o app exige) e ative a verificação em duas etapas nas contas de administrador (`FILEZAM_REQUIRE_2FA_ADMINS=true` obriga).
- Não use `PUID=0`: anula a execução sem root do container.
- Mantenha a pasta do banco (`/config`) fora da pasta exposta (`/data`); o app recusa iniciar se estiverem aninhadas. Faça backup de `/config` inteira (banco e `secret.key`).
- Reconstrua ou atualize a imagem a cada versão: as correções da biblioteca padrão do Go entram quando a versão em `go.mod`/`Dockerfile` sobe. `govulncheck` e `npm audit` rodam na CI.
