import { useUI, ZOOM_STEPS } from '../store/ui'
import { COLOR_PRESETS, DEFAULT_COLORS, type ColorKey } from '../lib/theme'
import { S, LOCALE_NAMES, type LangPref } from '../strings'
import { Modal } from './dialogs'

export default function SettingsDialog({ onClose }: { onClose: () => void }) {
  const { prefs, setPrefs, view, setView } = useUI()
  const Row = ({ k, label }: { k: 'showHidden' | 'showHints' | 'confirmDelete'; label: string }) => (
    <label className="flex items-center gap-2 py-1.5 text-sm">
      <input type="checkbox" checked={prefs[k]} onChange={(e) => setPrefs({ [k]: e.target.checked })} /> {label}
    </label>
  )
  const Color = ({ k, label, hint }: { k: ColorKey; label: string; hint: string }) => (
    <label className="flex items-center gap-3 py-1 text-sm">
      <input type="color" className="h-7 w-10 cursor-pointer rounded border border-neutral-300 bg-transparent p-0 dark:border-neutral-700" value={prefs[k]} onChange={(e) => setPrefs({ [k]: e.target.value })} />
      <span className="w-20 font-medium">{label}</span>
      <span className="text-xs text-neutral-500">{hint}</span>
    </label>
  )
  const Section = ({ title, children }: { title: string; children: React.ReactNode }) => (
    <div className="mt-4">
      <div className="mb-1 text-xs font-medium uppercase tracking-wide text-neutral-500">{title}</div>
      {children}
    </div>
  )
  return (
    <Modal onClose={onClose}>
      <h2 className="text-base font-semibold">{S.settings}</h2>
      <div className="mt-2 flex flex-col">
        <Row k="showHidden" label={S.prefShowHidden} />
        <Row k="showHints" label={S.prefShowHints} />
        <Row k="confirmDelete" label={S.prefConfirmDelete} />
        <div className="mt-2 flex flex-wrap gap-x-6 gap-y-2 text-sm">
          <label className="flex items-center gap-2">
            <span>{S.view}:</span>
            <select className="input !w-auto !py-1" value={view} onChange={(e) => setView(e.target.value as 'list' | 'grid')}>
              <option value="list">{S.viewList}</option>
              <option value="grid">{S.viewGrid}</option>
            </select>
          </label>
          <label className="flex items-center gap-2">
            <span>{S.zoom}:</span>
            <select className="input !w-auto !py-1" value={prefs.zoom} onChange={(e) => setPrefs({ zoom: Number(e.target.value) })}>
              {ZOOM_STEPS.map((z) => <option key={z} value={z}>{Math.round(z * 100)}%</option>)}
            </select>
          </label>
        </div>

        <Section title={S.language}>
          {/* textos são lidos no render, mas constantes de módulo não: recarrega para aplicar em tudo */}
          <select className="input !w-auto !py-1" value={prefs.lang} onChange={(e) => { setPrefs({ lang: e.target.value as LangPref }); setTimeout(() => location.reload(), 50) }}>
            <option value="auto">{S.languageAuto}</option>
            {(Object.keys(LOCALE_NAMES) as (keyof typeof LOCALE_NAMES)[]).map((l) => <option key={l} value={l}>{LOCALE_NAMES[l]}</option>)}
          </select>
        </Section>

        <Section title={S.theme}>
          <div className="flex gap-1">
            {(['system', 'light', 'dark'] as const).map((t) => (
              <button key={t} className={'btn-ghost !py-1 ' + (prefs.theme === t ? 'nav-active' : '')} onClick={() => setPrefs({ theme: t })}>
                {t === 'system' ? S.themeSystem : t === 'light' ? S.themeLight : S.themeDark}
              </button>
            ))}
          </div>
        </Section>

        <Section title={S.colors}>
          <Color k="accent" label={S.colorAccent} hint={S.colorAccentHint} />
          <Color k="selection" label={S.colorSelection} hint={S.colorSelectionHint} />
          <Color k="focus" label={S.colorFocus} hint={S.colorFocusHint} />
          <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-neutral-500">
            <span className="mr-1">{S.colorPresets}:</span>
            {COLOR_PRESETS.map((p) => (
              <button key={p.name} className="flex items-center gap-1 rounded-md border border-neutral-200 px-1.5 py-0.5 hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800" onClick={() => setPrefs(p.colors)} title={p.name}>
                <span className="h-3 w-3 rounded-full" style={{ background: p.colors.accent }} />
                <span className="h-3 w-3 rounded-full" style={{ background: p.colors.selection }} />
                {p.name}
              </button>
            ))}
            <button className="btn-ghost !px-1.5 !py-0.5 text-xs" onClick={() => setPrefs({ ...DEFAULT_COLORS })}>{S.restoreDefaults}</button>
          </div>
          {/* amostra ao vivo */}
          <div className="mt-3 flex items-center gap-2 rounded-md border border-neutral-200 p-2 text-sm dark:border-neutral-800">
            <button className="btn-primary !py-1">{S.save}</button>
            <span className="row-selected rounded px-2 py-1">{S.selected(1)}</span>
            <span className="row-focused rounded px-2 py-1">{S.colorFocus}</span>
            <a className="text-accent hover:underline">{S.shareLink}</a>
          </div>
        </Section>
      </div>
      <div className="mt-4 flex justify-end">
        <button className="btn-primary" onClick={onClose}>{S.close}</button>
      </div>
    </Modal>
  )
}
