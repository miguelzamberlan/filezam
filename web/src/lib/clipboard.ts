// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { toast } from '../components/dialogs'
import { S } from '../strings'

// Fallback para contextos não seguros (http://ip:porta), onde navigator.clipboard não existe.
function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  try {
    ta.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    ta.remove()
  }
}

// copyText copia e avisa o usuário; nunca falha em silêncio.
export async function copyText(text: string, okMessage: string = S.copied): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      toast(okMessage, 'success')
      return true
    }
  } catch {
    /* segue para o fallback */
  }
  if (legacyCopy(text)) {
    toast(okMessage, 'success')
    return true
  }
  toast(S.copyFailed, 'error')
  return false
}
