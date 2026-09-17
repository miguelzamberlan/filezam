// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useEffect } from 'react'
import { safeToReload, useUpdate } from '../lib/updates'
import { S } from '../strings'
import { IRefresh } from './Icons'

// Com que frequência a aba escondida reconfere se já pode recarregar sozinha.
const RECHECK_MS = 15_000

// Versão pela qual esta aba já se recarregou sozinha. Guardado no sessionStorage porque precisa
// sobreviver justamente à recarga, e por aba, que é o alcance da decisão.
const TRIED_KEY = 'filezam.autoReloadedTo'

/**
 * Faixa "o servidor foi atualizado, recarregue". Quem decide a hora é o usuário: recarregar por
 * baixo de um envio em andamento ou de um texto não salvo perderia trabalho, e o reload sozinho
 * só acontece com a aba escondida e sem nenhuma trava (ver useReloadHold em lib/updates.ts) —
 * assim ele volta para a versão nova sem nunca ter visto a faixa. Olhando para a tela, ele lê o
 * aviso e escolhe.
 *
 * Recarregar basta: o index.html é servido com `no-store` e os assets têm hash no nome, então
 * não há cache velho para furar, e o pouco que fica no localStorage são as preferências, que
 * valem para qualquer versão.
 */
export default function UpdateBanner() {
  const version = useUpdate((s) => s.serverVersion)

  useEffect(() => {
    if (!version) return
    const check = () => {
      if (!document.hidden || !safeToReload()) return
      // Uma tentativa por versão anunciada: se a recarga não resolver — um binário publicado com
      // um número diferente do bundle que ele embute, um proxy servindo assets de outra versão —,
      // a aba escondida recarregaria de 15 em 15 segundos para sempre. A faixa continua na tela
      // para recarregar na mão, e uma atualização posterior, com outro número, volta a valer.
      try {
        if (sessionStorage.getItem(TRIED_KEY) === version) return
        sessionStorage.setItem(TRIED_KEY, version)
      } catch {
        return // sem onde registrar a tentativa, não dá para garantir que ela não vira laço
      }
      location.reload()
    }
    const timer = window.setInterval(check, RECHECK_MS)
    document.addEventListener('visibilitychange', check)
    check()
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', check)
    }
  }, [version])

  if (!version) return null
  return (
    <div className="pointer-events-auto flex max-w-[min(32rem,calc(100vw-2rem))] items-center gap-3 rounded-md bg-accent px-4 py-2 text-sm text-white shadow-lg">
      <IRefresh size={16} className="shrink-0" />
      <span className="min-w-0">{S.updateAvailable(version)}</span>
      <button className="ml-auto shrink-0 rounded px-2 py-1 font-medium underline underline-offset-2 hover:bg-white/15" onClick={() => location.reload()}>
        {S.updateReload}
      </button>
    </div>
  )
}
