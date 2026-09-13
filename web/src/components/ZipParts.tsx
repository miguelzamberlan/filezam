import { useState } from 'react'
import { ApiError } from '../api/client'
import type { ZipPart, ZipPlan } from '../api/types'
import { formatBytes } from '../lib/format'
import { S } from '../strings'
import { Modal, dialogs, toast } from './dialogs'
import { ICheck, IDownload } from './Icons'

interface Options {
  /** Pergunta ao servidor como o zip se divide. */
  plan: () => Promise<ZipPlan>
  /** URL do zip inteiro (part null) ou de uma parte, com o nome do arquivo. */
  url: (part: ZipPart | null, name?: string) => string
  /** Nome do .zip sem extensão; as partes viram "nome (parte 1 de 3)". */
  name: string
  /** Dispara um download (link invisível ou navegação). */
  start: (url: string) => void
}

/**
 * Baixa uma pasta ou seleção como zip, dividindo em partes de até 2 GB quando passa disso — o que
 * o Google Drive faz com pastas grandes. Cada parte é um .zip completo: um download de dezenas de
 * gigabytes deixa de depender de uma única conexão ficar de pé até o fim, e uma parte que falhar
 * é baixada de novo sozinha. Até 2 GB (ou se o servidor não conseguir medir a pasta a tempo),
 * segue o zip único de sempre.
 */
export async function downloadZip({ plan, url, name, start }: Options) {
  const slow = setTimeout(() => toast(S.zipPreparing), 700)
  let p: ZipPlan
  try {
    p = await plan()
  } catch (e) {
    clearTimeout(slow)
    // Pasta enorme demais para medir a tempo: o zip único ainda funciona.
    if (e instanceof ApiError && e.code === 'timeout') return start(url(null))
    throw e
  } finally {
    clearTimeout(slow)
  }
  if (p.parts.length <= 1) return start(url(null))
  void dialogs.custom((close) => <ZipPartsDialog plan={p} url={url} name={name} onClose={close} />)
}

function ZipPartsDialog({ plan, url, name, onClose }: { plan: ZipPlan; url: Options['url']; name: string; onClose: () => void }) {
  const [clicked, setClicked] = useState<Set<number>>(new Set())
  const n = plan.parts.length
  return (
    <Modal onClose={onClose} size="lg">
      <h2 className="text-base font-semibold">{S.zipPartsTitle(n)}</h2>
      <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-400">{S.zipPartsHint(formatBytes(plan.bytes), plan.files, formatBytes(plan.partSize))}</p>
      <ul className="mt-3 max-h-80 divide-y divide-neutral-200 overflow-y-auto rounded-md border border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
        {plan.parts.map((part, i) => {
          const label = S.zipPartName(name, i + 1, n)
          const done = clicked.has(i)
          return (
            <li key={i} className="flex items-center gap-3 px-3 py-2 text-sm">
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">{S.zipPartLabel(i + 1, n)}</div>
                <div className="text-xs text-neutral-500">{S.zipPartFiles(part.files)} · {formatBytes(part.bytes)}</div>
              </div>
              <a
                href={url(part, label)}
                download
                className={done ? 'btn-ghost' : 'btn-primary'}
                onClick={() => setClicked(new Set(clicked).add(i))}
              >
                {done ? <ICheck size={16} /> : <IDownload size={16} />} {done ? S.zipPartStarted : S.download}
              </a>
            </li>
          )
        })}
      </ul>
      <div className="mt-4 flex justify-end">
        <button className="btn-ghost" onClick={onClose}>{S.close}</button>
      </div>
    </Modal>
  )
}
