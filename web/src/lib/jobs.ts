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
