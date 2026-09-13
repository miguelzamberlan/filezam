// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Identidade do projeto num lugar só: rodapé de créditos, banner do bundle e qualquer tela
// "Sobre" leem daqui.
//
// A versão não é digitada em lugar nenhum além de web/package.json: o vite.config.ts a injeta
// como __APP_VERSION__ no build, e a release (.github/workflows/release.yml) recusa a tag vX.Y.Z
// se o package.json disser outro número, o mesmo que vai para o binário por -X main.version.
// Para lançar uma versão: cd web && npm version X.Y.Z --no-git-tag-version.
export const APP = {
  name: 'Filezam',
  // typeof: o próprio vite.config.ts importa este arquivo antes de o define existir
  version: typeof __APP_VERSION__ === 'string' ? __APP_VERSION__ : 'dev',
  author: 'Miguel Zamberlan',
  authorUrl: 'https://github.com/miguelzamberlan',
  year: 2026,
  license: 'AGPL-3.0-only',
  licenseName: 'AGPL-3.0',
  licenseUrl: 'https://www.gnu.org/licenses/agpl-3.0.html',
  // Seção 13 da AGPLv3: quem usa pela rede precisa achar o código-fonte do que está rodando.
  // Uma versão modificada troca este endereço pelo do próprio código (NOTICE.md); o crédito ao
  // autor original fica.
  sourceUrl: 'https://github.com/miguelzamberlan/filezam',
} as const

// Comentário no topo do bundle JS. O "/*!" marca comentário legal: o minificador o preserva.
export const bundleBanner = (version: string) =>
  `/*! ${APP.name} v${version} | (C) ${APP.year} ${APP.author} | ${APP.license} | ${APP.sourceUrl} */`
