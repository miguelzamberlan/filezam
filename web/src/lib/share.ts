// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// shareLink monta a URL pública de um link de compartilhamento.
// O endereço certo é o que o navegador já está usando; o servidor só manda no
// resultado quando o operador definiu FILEZAM_PUBLIC_URL (proxy com outro domínio).
export function shareLink(token: string, publicUrl?: string | null): string {
  const base = (publicUrl ?? '').replace(/\/+$/, '') || window.location.origin
  return base + '/s/' + token
}
