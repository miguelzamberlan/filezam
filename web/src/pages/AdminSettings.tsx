import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { Settings } from '../api/types'
import { formatBytes, formatDuration, formatRelative } from '../lib/format'
import { S, errorMessage } from '../strings'
import { toast } from '../components/dialogs'
import { IRefresh, ISpinner } from '../components/Icons'
import { MenuButton } from '../components/Shell'

const GB = 1 << 30
const HOUR = 3600
const DAY = 86400

function Switch({ on, onClick, label, hint }: { on: boolean; onClick: () => void; label: string; hint: string }) {
  return (
    <div className="flex items-start gap-3 px-4 py-3">
      <button
        role="switch"
        aria-checked={on}
        aria-label={label}
        onClick={onClick}
        className={'mt-0.5 h-5 w-9 shrink-0 rounded-full transition ' + (on ? 'bg-accent' : 'bg-neutral-300 dark:bg-neutral-700')}
      >
        <span className={'block h-4 w-4 rounded-full bg-white transition ' + (on ? 'ml-4' : 'ml-0.5')} />
      </button>
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        <p className="text-xs text-neutral-500">{hint}</p>
      </div>
    </div>
  )
}

function Num({ label, value, options, format, onPick, hint }: { label: string; value: number; options: number[]; format: (n: number) => string; onPick: (n: number) => void; hint?: string }) {
  return (
    <div>
      <label className="block text-sm">{label}</label>
      <select className="input mt-1" value={value} onChange={(e) => onPick(Number(e.target.value))}>
        {(options.includes(value) ? options : [value, ...options].sort((a, b) => a - b)).map((n) => (
          <option key={n} value={n}>{format(n)}</option>
        ))}
      </select>
      {hint && <p className="mt-1 text-xs text-neutral-500">{hint}</p>}
    </div>
  )
}

// Quatro interruptores, todos valendo para a instalação inteira e conferidos a cada requisição
// (não só na criação), para que desligar um pare na hora o que já existe. O recebimento anônimo
// sai de fábrica **desligado**: ele inverte o modelo de ameaça do produto ao deixar alguém de
// fora escrever no disco, e isso precisa ser uma decisão de alguém, não um padrão.
export default function AdminSettings() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings'], queryFn: () => Api.adminSettings() })
  // Enquanto uma varredura roda, o status é consultado a cada 2 s para o botão e a duração
  // atualizarem sozinhos quando ela termina.
  const index = useQuery({ queryKey: ['adminIndex'], queryFn: () => Api.adminIndex(), refetchInterval: (q) => (q.state.data?.running ? 2000 : 30_000) })
  const scanNow = useMutation({
    mutationFn: () => Api.adminReindex(),
    onSuccess: (r) => {
      toast(r.started ? S.settingsIndexScanStarted : S.reindexRunning)
      qc.invalidateQueries({ queryKey: ['adminIndex'] })
    },
    onError: (e) => toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error'),
  })
  const [form, setForm] = useState<Settings | null>(null)
  useEffect(() => {
    if (q.data) setForm(q.data.settings)
  }, [q.data])

  const save = useMutation({
    mutationFn: (patch: Partial<Settings>) => Api.adminUpdateSettings(patch),
    onSuccess: (r) => {
      setForm(r.settings)
      qc.invalidateQueries({ queryKey: ['settings'] })
      qc.invalidateQueries({ queryKey: ['config'] })
      toast(S.settingsSaved)
    },
    onError: (e) => toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error'),
  })

  if (q.isLoading || !form) return <div className="flex h-full items-center justify-center"><ISpinner /></div>

  const set = (patch: Partial<Settings>) => setForm({ ...form, ...patch })
  const toggle = (key: 'slugsEnabled' | 'dropEnabled' | 'extractEnabled' | 'thumbsEnabled') => {
    set({ [key]: !form[key] } as Partial<Settings>)
    save.mutate({ [key]: !form[key] } as Partial<Settings>)
  }
  const maxTtlDays = Math.floor((q.data?.dropTtlHardMax ?? 30 * 86400) / 86400)

  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-4 flex items-center gap-2"><MenuButton /><h1 className="text-lg font-semibold">{S.settingsAdmin}</h1></div>
      <div className="card max-w-2xl divide-y divide-neutral-200 dark:divide-neutral-800">
        <Switch on={form.slugsEnabled} onClick={() => toggle('slugsEnabled')} label={S.settingsSlugs} hint={S.settingsSlugsHint} />
        <Switch on={form.dropEnabled} onClick={() => toggle('dropEnabled')} label={S.settingsDrop} hint={S.settingsDropHint} />
        <Switch on={form.extractEnabled} onClick={() => toggle('extractEnabled')} label={S.settingsExtract} hint={S.settingsExtractHint} />
        <Switch on={form.thumbsEnabled} onClick={() => toggle('thumbsEnabled')} label={S.settingsThumbs} hint={S.settingsThumbsHint} />
        <div className="space-y-3 px-4 py-4">
          <h2 className="text-sm font-semibold">{S.settingsMaintenance}</h2>
          <Num
            label={S.settingsTrashRetention}
            value={form.trashRetention}
            options={[0, 1, 7, 15, 30, 60, 90, 180, 365].map((d) => d * DAY)}
            format={(n) => (n === 0 ? S.settingsTrashOff : n % DAY === 0 ? `${n / DAY} ${n === DAY ? S.day : S.days}` : formatDuration(n))}
            onPick={(n) => { set({ trashRetention: n }); save.mutate({ trashRetention: n }) }}
            hint={S.settingsTrashHint}
          />
          {index.data?.enabled === false ? (
            <p className="text-xs text-neutral-500">{S.settingsIndexOff}</p>
          ) : (
            <Num
              label={S.settingsIndexInterval}
              value={form.indexInterval}
              options={[15 * 60, 30 * 60, HOUR, 2 * HOUR, 6 * HOUR, 12 * HOUR, 24 * HOUR]}
              format={(n) => (n < HOUR ? `${n / 60} ${S.minutes}` : n % HOUR === 0 ? `${n / HOUR} ${n === HOUR ? S.hour : S.hours}` : formatDuration(n))}
              onPick={(n) => { set({ indexInterval: n }); save.mutate({ indexInterval: n }) }}
              hint={S.settingsIndexHint}
            />
          )}
          {index.data?.enabled && (
            <div className="flex flex-wrap items-center gap-3">
              <button className="btn-ghost border border-neutral-300 text-sm dark:border-neutral-700" onClick={() => scanNow.mutate()} disabled={index.data.running || scanNow.isPending}>
                {index.data.running || scanNow.isPending ? <ISpinner size={16} /> : <IRefresh size={16} />} {S.settingsIndexScanNow}
              </button>
              <span className="text-xs text-neutral-500">
                {index.data.running
                  ? S.settingsIndexScanning
                  : index.data.lastFullAt
                    ? S.settingsIndexLast(formatRelative(index.data.lastFullAt), index.data.entries ?? 0, index.data.lastMs ? (index.data.lastMs < 1000 ? `${index.data.lastMs} ms` : formatDuration(index.data.lastMs / 1000)) : '')
                    : S.settingsIndexNever}
              </span>
            </div>
          )}
        </div>
        {form.dropEnabled && (
          <div className="space-y-3 px-4 py-4">
            <h2 className="text-sm font-semibold">{S.settingsDropLimits}</h2>
            <Num
              label={S.settingsDropMaxQuota}
              value={form.dropMaxQuota}
              options={[1, 5, 10, 25, 50, 100, 250, 500].map((n) => n * GB)}
              format={formatBytes}
              onPick={(n) => { set({ dropMaxQuota: n }); save.mutate({ dropMaxQuota: n }) }}
            />
            <Num
              label={S.settingsDropMaxTtl}
              value={form.dropMaxTtl}
              options={[1, 3, 7, 15, maxTtlDays].map((d) => d * 86400)}
              format={(n) => `${Math.round(n / 86400)} ${S.days}`}
              onPick={(n) => { set({ dropMaxTtl: n }); save.mutate({ dropMaxTtl: n }) }}
            />
            <Num
              label={S.settingsDropFileMax}
              value={form.dropFileMax}
              options={[1, 2, 5, 10, 25, 50].map((n) => n * GB)}
              format={formatBytes}
              onPick={(n) => { set({ dropFileMax: n }); save.mutate({ dropFileMax: n }) }}
            />
            <Num
              label={S.settingsDropMaxFiles}
              value={form.dropMaxFiles}
              options={[50, 100, 500, 1000, 5000]}
              format={(n) => String(n)}
              onPick={(n) => { set({ dropMaxFiles: n }); save.mutate({ dropMaxFiles: n }) }}
            />
            <Num
              label={S.settingsDropMaxLinks}
              value={form.dropMaxLinks}
              options={[1, 5, 10, 20, 50, 100]}
              format={(n) => String(n)}
              onPick={(n) => { set({ dropMaxLinks: n }); save.mutate({ dropMaxLinks: n }) }}
            />
            <Num
              label={S.settingsDropStaleAge}
              value={form.dropStaleAge}
              options={[1, 2, 6, 12, 24].map((h) => h * HOUR)}
              format={(n) => `${Math.round(n / HOUR)} ${n === HOUR ? S.hour : S.hours}`}
              onPick={(n) => { set({ dropStaleAge: n }); save.mutate({ dropStaleAge: n }) }}
            />
          </div>
        )}
      </div>
    </div>
  )
}
