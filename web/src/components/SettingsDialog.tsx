import { useUI, ZOOM_STEPS, type Prefs } from '../store/ui'
import { COLOR_PRESETS, DEFAULT_COLORS, type ColorKey } from '../lib/theme'
import { S, LOCALE_NAMES, type LangPref } from '../strings'
import { Modal } from './dialogs'
import { IFolder, ICheck } from './Icons'

// Os subcomponentes ficam FORA do diálogo de propósito: definidos dentro, ganhariam uma
// identidade nova a cada render e o React remontaria o <input type="color"> a cada arraste
// no seletor, fechando-o no meio da escolha.

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="border-t border-neutral-200 pt-4 first:border-t-0 first:pt-0 dark:border-neutral-800">
      <h3 className="text-sm font-semibold">{title}</h3>
      {hint && <p className="mt-0.5 text-xs text-neutral-500">{hint}</p>}
      <div className="mt-3 space-y-3">{children}</div>
    </section>
  )
}

// Linha rótulo | controle, com o rótulo numa coluna fixa para tudo alinhar.
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-1.5 text-sm sm:grid-cols-[7rem_1fr] sm:items-center sm:gap-3">
      <span className="text-neutral-600 dark:text-neutral-300">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

function Toggle({ checked, label, onChange }: { checked: boolean; label: string; onChange: (v: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center gap-3 text-sm">
      <input type="checkbox" className="h-4 w-4 accent-[var(--accent)]" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  )
}

function Segmented<T extends string>({ value, options, onChange }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void }) {
  return (
    <div className="inline-flex rounded-md border border-neutral-300 p-0.5 dark:border-neutral-700" role="radiogroup">
      {options.map((o) => (
        <button key={o.value} role="radio" aria-checked={value === o.value} className={'whitespace-nowrap rounded px-2.5 py-1 text-sm transition ' + (value === o.value ? 'nav-active font-medium' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800')} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  )
}

function ColorRow({ k, label, hint, value, onChange }: { k: ColorKey; label: string; hint: string; value: string; onChange: (v: string) => void }) {
  return (
    <div className="grid grid-cols-[2.75rem_1fr] items-center gap-3 text-sm">
      <label className="relative block h-8 w-11 cursor-pointer overflow-hidden rounded-md border border-neutral-300 dark:border-neutral-700" style={{ background: value }} title={label}>
        {/* o input cobre o quadrado inteiro; a cor de fundo é o que o usuário vê */}
        <input id={'color-' + k} type="color" className="absolute inset-0 h-full w-full cursor-pointer opacity-0" value={value} onChange={(e) => onChange(e.target.value)} />
      </label>
      <div className="min-w-0 sm:grid sm:grid-cols-[7rem_1fr] sm:items-center sm:gap-3">
        <label htmlFor={'color-' + k} className="block cursor-pointer font-medium">{label}</label>
        <span className="block text-xs text-neutral-500">{hint}</span>
      </div>
    </div>
  )
}

export default function SettingsDialog({ onClose }: { onClose: () => void }) {
  const { prefs, setPrefs, view, setView } = useUI()
  const set = (p: Partial<Prefs>) => setPrefs(p)
  const isPreset = (colors: Record<ColorKey, string>) => colors.accent === prefs.accent && colors.selection === prefs.selection && colors.focus === prefs.focus
  return (
    <Modal onClose={onClose} size="lg">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">{S.settings}</h2>
        <button className="btn-primary !py-1" onClick={onClose}>{S.close}</button>
      </div>
      <div className="mt-4 space-y-5">
        <Section title={S.settingsGeneral}>
          <Toggle checked={prefs.showHidden} label={S.prefShowHidden} onChange={(v) => set({ showHidden: v })} />
          <Toggle checked={prefs.showHints} label={S.prefShowHints} onChange={(v) => set({ showHints: v })} />
          <Toggle checked={prefs.confirmDelete} label={S.prefConfirmDelete} onChange={(v) => set({ confirmDelete: v })} />
        </Section>

        <Section title={S.settingsAppearance}>
          <Field label={S.language}>
            {/* textos são lidos no render, mas constantes de módulo não: recarrega para aplicar em tudo */}
            <select className="input !w-auto !py-1" value={prefs.lang} onChange={(e) => { set({ lang: e.target.value as LangPref }); setTimeout(() => location.reload(), 50) }}>
              <option value="auto">{S.languageAuto}</option>
              {(Object.keys(LOCALE_NAMES) as (keyof typeof LOCALE_NAMES)[]).map((l) => <option key={l} value={l}>{LOCALE_NAMES[l]}</option>)}
            </select>
          </Field>
          <Field label={S.theme}>
            <Segmented value={prefs.theme} onChange={(v) => set({ theme: v })} options={[{ value: 'system', label: S.themeSystem }, { value: 'light', label: S.themeLight }, { value: 'dark', label: S.themeDark }]} />
          </Field>
          <Field label={S.view}>
            <Segmented value={view} onChange={setView} options={[{ value: 'list', label: S.viewList }, { value: 'grid', label: S.viewGrid }]} />
          </Field>
          <Field label={S.zoom}>
            <select className="input !w-auto !py-1" value={prefs.zoom} onChange={(e) => set({ zoom: Number(e.target.value) })}>
              {ZOOM_STEPS.map((z) => <option key={z} value={z}>{Math.round(z * 100)}%</option>)}
            </select>
          </Field>
        </Section>

        <Section title={S.colors} hint={S.colorsHint}>
          <ColorRow k="accent" label={S.colorAccent} hint={S.colorAccentHint} value={prefs.accent} onChange={(v) => set({ accent: v })} />
          <ColorRow k="selection" label={S.colorSelection} hint={S.colorSelectionHint} value={prefs.selection} onChange={(v) => set({ selection: v })} />
          <ColorRow k="focus" label={S.colorFocus} hint={S.colorFocusHint} value={prefs.focus} onChange={(v) => set({ focus: v })} />
          <Field label={S.colorPresets}>
            <div className="flex flex-wrap gap-1.5">
              {COLOR_PRESETS.map((p) => (
                <button key={p.name} className={'flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition ' + (isPreset(p.colors) ? 'border-accent ring-1 ring-accent' : 'border-neutral-300 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800')} onClick={() => set(p.colors)} title={p.name}>
                  <span className="flex overflow-hidden rounded-full"><span className="h-3.5 w-3.5" style={{ background: p.colors.accent }} /><span className="h-3.5 w-3.5" style={{ background: p.colors.selection }} /></span>
                  {p.name}
                  {isPreset(p.colors) && <ICheck size={12} className="text-accent" />}
                </button>
              ))}
              <button className="btn-ghost !px-2 !py-1 text-xs" onClick={() => set({ ...DEFAULT_COLORS })}>{S.restoreDefaults}</button>
            </div>
          </Field>
          <Field label={S.colorsPreview}>
            {/* amostra ao vivo: botão principal, linha selecionada, linha focada e um link */}
            <div className="overflow-hidden rounded-md border border-neutral-200 text-sm dark:border-neutral-800">
              <div className="flex items-center gap-2 border-b border-neutral-200 px-2 py-1.5 dark:border-neutral-800"><button className="btn-primary !py-0.5 text-xs">{S.save}</button><a className="text-xs text-accent hover:underline">{S.shareLink}</a></div>
              <div className="row-selected flex items-center gap-2 px-2 py-1"><IFolder size={16} className="text-amber-500" /> {S.selected(1)}</div>
              <div className="row-focused flex items-center gap-2 px-2 py-1"><IFolder size={16} className="text-amber-500" /> {S.colorFocus}</div>
            </div>
          </Field>
        </Section>
      </div>
    </Modal>
  )
}
