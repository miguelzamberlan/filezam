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
