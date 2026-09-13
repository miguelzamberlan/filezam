// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import type { ReactNode } from 'react'
import { APP } from '../lib/about'
import { S } from '../strings'

// Avisos Legais Apropriados (AGPLv3, seções 0, 5(d) e 13): nome e versão, crédito ao autor
// original, licença e o link para o código-fonte, que é como quem usa o sistema pela rede acha o
// código do que está rodando. Os termos adicionais do NOTICE.md exigem que o crédito continue
// visível em cópias e versões modificadas. Discreto de propósito: texto pequeno, sem cor de
// destaque, fora do caminho das ações.
// stacked: uma informação por linha, para caber na largura do menu lateral. Sem ele, nome e autor
// dividem a linha a partir de sm e quebram no separador em telas estreitas.
export default function Credits({ className = '', stacked = false }: { className?: string; stacked?: boolean }) {
  const link = 'underline-offset-2 hover:text-neutral-600 hover:underline dark:hover:text-neutral-300'
  const sep = stacked ? <br /> : <><span className="hidden sm:inline"> · </span><br className="sm:hidden" /></>
  return (
    <p className={'text-[11px] leading-snug text-neutral-400 dark:text-neutral-500 ' + className} data-credits>
      <span className="whitespace-nowrap">{S.poweredBy} {APP.name} v{APP.version}</span>
      {sep}
      <span>{S.originallyBy} <a className={link} href={APP.authorUrl} target="_blank" rel="noopener noreferrer">{APP.author}</a></span>
      <br />
      <a className={link} href={APP.licenseUrl} target="_blank" rel="noopener noreferrer license" title={APP.license}>{APP.licenseName}</a>
      {' · '}
      <a className={link} href={APP.sourceUrl} target="_blank" rel="noopener noreferrer">{S.sourceCode}</a>
    </p>
  )
}

// Telas sem o menu lateral (login, links públicos, troca de senha, 2FA): a página ocupa o espaço
// que sobra e os créditos ficam numa faixa fixa embaixo, sem rolar junto com o conteúdo.
export function WithCredits({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      <div className="min-h-0 flex-1">{children}</div>
      <Credits className="shrink-0 px-4 pb-2 pt-1 text-center" />
    </div>
  )
}
