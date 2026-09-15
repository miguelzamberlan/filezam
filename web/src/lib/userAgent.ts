// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Descrição curta do navegador de uma sessão ("Chrome · Windows") para o painel do admin. Não é
// detecção de verdade: o User-Agent é o que o cliente diz ser, e aqui só serve para reconhecer
// o aparelho numa lista.
export function describeUserAgent(ua: string): string {
  if (!ua) return '—'
  const browser = /Edg(e|A|iOS)?\//.test(ua) ? 'Edge'
    : /OPR\/|Opera/.test(ua) ? 'Opera'
    : /Firefox\/|FxiOS\//.test(ua) ? 'Firefox'
    : /SamsungBrowser\//.test(ua) ? 'Samsung Internet'
    : /Chrome\/|CriOS\//.test(ua) ? 'Chrome'
    : /Safari\//.test(ua) ? 'Safari'
    : /^curl\//i.test(ua) ? 'curl'
    : ''
  const os = /Android/.test(ua) ? 'Android'
    : /iPhone|iPad|iPod/.test(ua) ? 'iOS'
    : /Windows/.test(ua) ? 'Windows'
    : /Macintosh|Mac OS X/.test(ua) ? 'macOS'
    : /CrOS/.test(ua) ? 'ChromeOS'
    : /Linux/.test(ua) ? 'Linux'
    : ''
  if (!browser && !os) return ua.length > 40 ? ua.slice(0, 40) + '…' : ua
  return [browser, os].filter(Boolean).join(' · ')
}
