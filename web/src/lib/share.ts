// shareLink monta a URL pública de um link de compartilhamento.
// O endereço certo é o que o navegador já está usando; o servidor só manda no
// resultado quando o operador definiu FILEZAM_PUBLIC_URL (proxy com outro domínio).
export function shareLink(token: string, publicUrl?: string | null): string {
  const base = (publicUrl ?? '').replace(/\/+$/, '') || window.location.origin
  return base + '/s/' + token
}
