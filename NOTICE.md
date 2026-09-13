# NOTICE — Filezam

**Filezam** — gerenciador de arquivos web auto-hospedado
Copyright (C) 2026 Miguel Zamberlan
Repositório original: <https://github.com/miguelzamberlan/filezam>
SPDX-License-Identifier: `AGPL-3.0-only`

*English version below.*

## Licença

Este programa é software livre: você pode redistribuí-lo e/ou modificá-lo nos termos da **GNU Affero General Public License, versão 3** (somente a versão 3), publicada pela Free Software Foundation. O texto integral está em [`LICENSE`](LICENSE). O programa é distribuído na esperança de ser útil, mas **sem nenhuma garantia**, nem mesmo a garantia implícita de comercialização ou de adequação a um propósito específico.

As versões **1.0.0 a 1.2.0** foram publicadas sob a licença MIT e continuam sob a MIT para quem as obteve. As versões seguintes — e todo o código na `main` a partir da troca de licença — são AGPL-3.0-only.

## Uso em rede (seção 13 da AGPLv3)

Quem **modifica** o Filezam e o oferece a usuários por rede (servidor próprio, nuvem, SaaS, intranet) deve oferecer a **todos** esses usuários — inclusive visitantes anônimos de links públicos — o **código-fonte correspondente** da versão modificada, sob a mesma licença, por meio de um link acessível na própria interface. O link **Código-fonte** do rodapé existe para isso: numa versão modificada, ele deve apontar para o código dessa versão (constante `APP.sourceUrl` em `web/src/lib/about.ts`).

## Termos adicionais (seção 7 da AGPLv3)

Conforme a seção 7 da AGPLv3, os termos abaixo complementam a licença e se aplicam a todos os arquivos deste repositório. Eles acompanham qualquer cópia, fork, versão modificada ou trabalho derivado.

1. **Avisos legais nos arquivos** — seção 7(b). Devem ser preservados, sem alteração: o cabeçalho de licença no topo dos arquivos-fonte (o bloco com `SPDX-License-Identifier: AGPL-3.0-only`), o arquivo [`LICENSE`](LICENSE) e este `NOTICE.md`. Arquivos novos de um trabalho derivado podem trazer o aviso de copyright de quem os escreveu, ao lado deste.
2. **Atribuição de autoria na interface** — seção 7(b). Os Avisos Legais Apropriados exibidos pela interface do programa (o crédito no rodapé do menu, da tela de login e das páginas públicas) devem continuar mostrando, de forma legível: o nome **Filezam**, o crédito **"Desenvolvido originalmente por Miguel Zamberlan"**, a licença **AGPL-3.0** e um link para o código-fonte correspondente. Versões modificadas podem acrescentar os próprios créditos e trocar o link do código-fonte pelo delas, mas não podem remover o crédito ao autor original.
3. **Versões modificadas identificadas** — seção 7(c). Uma versão modificada deve ser marcada de forma razoável como diferente da original (por exemplo, com outro nome ou com "baseado no Filezam" junto ao próprio nome), e as mudanças devem constar nos arquivos alterados ou no histórico do repositório.
4. **Nome e marca** — seção 7(e). A licença não concede direito de usar o nome "Filezam" ou o nome do autor para sugerir que uma versão modificada, um serviço ou uma empresa é oficial, endossado ou mantido pelo autor original.

Descumprir a licença ou estes termos **encerra automaticamente** os direitos concedidos por ela (seção 8 da AGPLv3); a partir daí, copiar, modificar ou distribuir o programa infringe os direitos autorais do autor.

## Licenciamento comercial (licença dual)

O Filezam é oferecido em **licença dual**:

- **AGPL-3.0-only**, gratuita, com todas as obrigações acima — em especial abrir o código das modificações a quem usa o sistema pela rede;
- **licença comercial**, negociada com o autor, para quem precisa usar, modificar ou embutir o Filezam **sem** as obrigações da AGPL (por exemplo, num produto ou serviço de código fechado).

Para licença comercial, entre em contato pelo perfil do autor no GitHub: <https://github.com/miguelzamberlan>.

A licença dual só é possível porque o autor detém os direitos de todo o código. Por isso, contribuições externas são aceitas nas condições descritas em [`CONTRIBUTING.md`](CONTRIBUTING.md#licença-das-contribuições).

## Componentes de terceiros

As dependências (módulos Go listados em `go.mod`, pacotes npm listados em `web/package.json`) continuam sob as próprias licenças, todas compatíveis com a AGPLv3. Os avisos delas acompanham os respectivos pacotes.

---

## English version

**Filezam** — self-hosted web file manager
Copyright (C) 2026 Miguel Zamberlan
Original repository: <https://github.com/miguelzamberlan/filezam>
SPDX-License-Identifier: `AGPL-3.0-only`

*In case of divergence, the Portuguese version above prevails.*

### License

This program is free software: you can redistribute it and/or modify it under the terms of the **GNU Affero General Public License, version 3** (version 3 only), as published by the Free Software Foundation. The full text is in [`LICENSE`](LICENSE). It is distributed in the hope that it will be useful, but **without any warranty**; without even the implied warranty of merchantability or fitness for a particular purpose.

Versions **1.0.0 to 1.2.0** were released under the MIT license and remain MIT for whoever obtained them. Later versions, and all code on `main` since the license change, are AGPL-3.0-only.

### Network use (AGPLv3 section 13)

Whoever **modifies** Filezam and offers it to users over a network (own server, cloud, SaaS, intranet) must offer **all** those users, including anonymous visitors of public links, the **Corresponding Source** of the modified version under the same license, through a link available in the interface itself. The **Source code** link in the footer exists for that: in a modified version it must point to that version's source (`APP.sourceUrl` in `web/src/lib/about.ts`).

### Additional terms (AGPLv3 section 7)

Under section 7 of the AGPLv3, the terms below supplement the license and apply to every file in this repository. They travel with any copy, fork, modified version or derivative work.

1. **Legal notices in files**, section 7(b). The license header at the top of the source files (the block with `SPDX-License-Identifier: AGPL-3.0-only`), the [`LICENSE`](LICENSE) file and this `NOTICE.md` must be preserved unchanged. New files in a derivative work may carry their authors' copyright notice next to this one.
2. **Author attribution in the interface**, section 7(b). The Appropriate Legal Notices displayed by the program's interface (the credit in the footer of the menu, the login screen and the public pages) must keep showing, legibly: the name **Filezam**, the credit **"Originally developed by Miguel Zamberlan"**, the **AGPL-3.0** license and a link to the corresponding source code. Modified versions may add their own credits and point the source link to their own code, but may not remove the credit to the original author.
3. **Modified versions marked as such**, section 7(c). A modified version must be marked in reasonable ways as different from the original (for example, under another name or with "based on Filezam" next to its own name), and the changes must be recorded in the modified files or in the repository history.
4. **Name and trademark**, section 7(e). The license grants no right to use the name "Filezam" or the author's name to suggest that a modified version, service or company is official, endorsed or maintained by the original author.

Violating the license or these terms **automatically terminates** the rights it grants (AGPLv3 section 8); from then on, copying, modifying or distributing the program infringes the author's copyright.

### Commercial licensing (dual license)

Filezam is offered under a **dual license**:

- **AGPL-3.0-only**, free of charge, with all the obligations above, notably releasing the source of modifications to whoever uses the system over a network;
- a **commercial license**, negotiated with the author, for those who need to use, modify or embed Filezam **without** the AGPL obligations (for example, in a closed-source product or service).

For a commercial license, contact the author through their GitHub profile: <https://github.com/miguelzamberlan>.

Dual licensing is only possible because the author holds the rights to all the code. That is why external contributions are accepted under the conditions described in [`CONTRIBUTING.md`](CONTRIBUTING.md#licença-das-contribuições).

### Third-party components

Dependencies (Go modules listed in `go.mod`, npm packages listed in `web/package.json`) remain under their own licenses, all compatible with the AGPLv3. Their notices ship with the respective packages.
