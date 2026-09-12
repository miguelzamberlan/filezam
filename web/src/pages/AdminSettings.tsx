import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { Settings } from '../api/types'
import { formatBytes } from '../lib/format'
import { S, errorMessage } from '../strings'
import { toast } from '../components/dialogs'
import { ISpinner } from '../components/Icons'
import { MenuButton } from '../components/Shell'

const GB = 1 << 30
const HOUR = 3600

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

function Num({ label, value, options, format, onPick }: { label: string; value: number; options: number[]; format: (n: number) => string; onPick: (n: number) => void }) {
  return (
    <div>
      <label className="block text-sm">{label}</label>
      <select className="input mt-1" value={value} onChange={(e) => onPick(Number(e.target.value))}>
        {(options.includes(value) ? options : [value, ...options].sort((a, b) => a - b)).map((n) => (
          <option key={n} value={n}>{format(n)}</option>
        ))}
      </select>
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
