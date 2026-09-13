// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import type { JobType } from '../api/types'
import { S } from '../strings'

// Mapa com fallback em vez de um ternário fechado: um tipo de operação novo aparecia como
// "Excluindo" até alguém notar.
export function jobLabel(type: JobType | string): string {
  const labels: Record<string, string> = {
    copy: S.jobCopy,
    move: S.jobMove,
    delete: S.jobDelete,
    extract: S.jobExtract,
    archive: S.jobArchive,
  }
  return labels[type] ?? type
}
