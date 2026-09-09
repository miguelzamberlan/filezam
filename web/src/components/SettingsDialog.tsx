import { useUI, ZOOM_STEPS } from '../store/ui'
import { S } from '../strings'
import { Modal } from './dialogs'

export default function SettingsDialog({ onClose }: { onClose: () => void }) {
  const { prefs, setPrefs, view, setView } = useUI()
  const Row = ({ k, label }: { k: 'showHidden' | 'showHints' | 'confirmDelete'; label: string }) => (
    <label className="flex items-center gap-2 py-1.5 text-sm">
      <input type="checkbox" checked={prefs[k]} onChange={(e) => setPrefs({ [k]: e.target.checked })} /> {label}
    </label>
  )
  return (
    <Modal onClose={onClose}>
      <h2 className="text-base font-semibold">{S.settings}</h2>
      <div className="mt-3 flex flex-col">
        <Row k="showHidden" label={S.prefShowHidden} />
        <Row k="showHints" label={S.prefShowHints} />
        <Row k="confirmDelete" label={S.prefConfirmDelete} />
        <label className="mt-2 flex items-center gap-2 text-sm">
          <span>{S.view}:</span>
          <select className="input !w-auto !py-1" value={view} onChange={(e) => setView(e.target.value as 'list' | 'grid')}>
            <option value="list">{S.viewList}</option>
            <option value="grid">{S.viewGrid}</option>
          </select>
        </label>
        <label className="mt-2 flex items-center gap-2 text-sm">
          <span>{S.zoom}:</span>
          <select className="input !w-auto !py-1" value={prefs.zoom} onChange={(e) => setPrefs({ zoom: Number(e.target.value) })}>
            {ZOOM_STEPS.map((z) => <option key={z} value={z}>{Math.round(z * 100)}%</option>)}
          </select>
        </label>
      </div>
      <div className="mt-4 flex justify-end">
        <button className="btn-primary" onClick={onClose}>{S.close}</button>
      </div>
    </Modal>
  )
}
