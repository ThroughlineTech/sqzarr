import { useEffect, useState, useCallback } from 'react'
import { api, type Directory, type FSBrowseResult } from '../lib/api'
import { formatBitrate, basename } from '../lib/utils'
import { Plus, Pencil, Trash2, X, Check, FolderOpen, ChevronLeft, Copy } from 'lucide-react'

interface Settings {
  enabled: boolean
  min_age_days: number
  max_bitrate: number
  min_size_mb: number
  bitrate_skip_margin: number
}

const defaultSettings: Settings = {
  enabled: true,
  min_age_days: 7,
  max_bitrate: 2_222_000,
  min_size_mb: 500,
  bitrate_skip_margin: 0.10,
}

// ---- Directory browser modal ----

function DirectoryBrowser({ onSelect, onClose, startPath }: {
  onSelect: (path: string) => void
  onClose: () => void
  startPath?: string
}) {
  const [browse, setBrowse] = useState<FSBrowseResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const navigate = useCallback(async (path: string) => {
    setLoading(true)
    setError('')
    try { setBrowse(await api.browseFS(path)) }
    catch (e: any) { setError(e.message || 'Cannot read directory') }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { navigate(startPath ?? '') }, [navigate, startPath])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-md mx-4 flex flex-col max-h-[70vh]">
        <div className="flex items-center justify-between px-4 py-3 border-b border-stone-200">
          <h3 className="text-sm font-semibold text-stone-900">Browse for directory</h3>
          <button onClick={onClose} className="text-stone-400 hover:text-stone-700"><X size={16} /></button>
        </div>
        <div className="px-4 py-2 bg-stone-50 border-b border-stone-100">
          <p className="text-xs font-mono text-stone-600 truncate">{browse?.current ?? '/'}</p>
        </div>
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <p className="text-sm text-stone-400 text-center py-8">Loading…</p>
          ) : error ? (
            <p className="text-sm text-red-600 text-center py-8">{error}</p>
          ) : (
            <>
              {browse?.parent && (
                <button onClick={() => navigate(browse.parent)}
                  className="w-full flex items-center gap-2 px-4 py-2.5 text-sm text-stone-600 hover:bg-stone-50 border-b border-stone-100">
                  <ChevronLeft size={14} className="text-stone-400" /> Parent directory
                </button>
              )}
              {browse?.dirs.length === 0 && (
                <p className="text-sm text-stone-400 text-center py-6">No subdirectories</p>
              )}
              {browse?.dirs.map(dir => (
                <button key={dir} onClick={() => navigate(dir)}
                  className="w-full flex items-center gap-2 px-4 py-2.5 text-sm text-stone-700 hover:bg-stone-50 border-b border-stone-100 last:border-0">
                  <FolderOpen size={14} className="text-amber-500 shrink-0" />
                  <span className="truncate">{basename(dir)}</span>
                </button>
              ))}
            </>
          )}
        </div>
        <div className="flex items-center justify-between gap-2 px-4 py-3 border-t border-stone-200">
          <button onClick={() => browse?.parent && navigate(browse.parent)} disabled={!browse?.parent}
            className="flex items-center gap-1 text-sm text-stone-600 hover:text-stone-900 disabled:opacity-40 transition-colors">
            <ChevronLeft size={14} /> Parent
          </button>
          <button onClick={() => browse && onSelect(browse.current)} disabled={!browse}
            className="px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-50 text-white rounded-md transition-colors">
            Select This Folder
          </button>
        </div>
      </div>
    </div>
  )
}

// ---- Shared settings fields ----

function SettingsFields({ s, onChange }: { s: Settings; onChange: (s: Settings) => void }) {
  return (
    <>
      <div className="grid grid-cols-4 gap-3">
        <label className="block">
          <span className="text-xs font-medium text-stone-600">Min age (days)</span>
          <input type="number" min={0} value={s.min_age_days}
            onChange={e => onChange({ ...s, min_age_days: Number(e.target.value) })}
            className="mt-1 block w-full rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
        </label>
        <label className="block">
          <span className="text-xs font-medium text-stone-600">Max bitrate (bps)</span>
          <input type="number" min={0} value={s.max_bitrate}
            onChange={e => onChange({ ...s, max_bitrate: Number(e.target.value) })}
            className="mt-1 block w-full rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
        </label>
        <label className="block">
          <span className="text-xs font-medium text-stone-600">Skip margin (%)</span>
          <input type="number" min={0} max={100} step={1}
            value={Math.round(s.bitrate_skip_margin * 100)}
            onChange={e => onChange({ ...s, bitrate_skip_margin: Number(e.target.value) / 100 })}
            className="mt-1 block w-full rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
        </label>
        <label className="block">
          <span className="text-xs font-medium text-stone-600">Min size (MB)</span>
          <input type="number" min={0} value={s.min_size_mb}
            onChange={e => onChange({ ...s, min_size_mb: Number(e.target.value) })}
            className="mt-1 block w-full rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
        </label>
      </div>
      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={s.enabled} onChange={e => onChange({ ...s, enabled: e.target.checked })}
          className="rounded-sm border-stone-300 text-amber-600 focus:ring-amber-500" />
        <span className="text-sm text-stone-700">Enabled</span>
      </label>
    </>
  )
}

// ---- Inline edit card ----

function EditCard({ dir, onSave, onCancel }: {
  dir: Directory
  onSave: () => void
  onCancel: () => void
}) {
  const [path, setPath] = useState(dir.Path)
  const [settings, setSettings] = useState<Settings>({
    enabled: dir.Enabled,
    min_age_days: dir.MinAgeDays,
    max_bitrate: dir.MaxBitrate,
    min_size_mb: dir.MinSizeMB,
    bitrate_skip_margin: dir.BitrateSkipMargin,
  })
  const [showBrowser, setShowBrowser] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!path.trim()) { setError('Path is required'); return }
    setSaving(true)
    setError('')
    try {
      await api.updateDirectory(dir.ID, { path, ...settings })
      onSave()
    } catch (e: any) {
      setError(e.message ?? 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      {showBrowser && (
        <DirectoryBrowser
          onSelect={p => { setPath(p); setShowBrowser(false) }}
          onClose={() => setShowBrowser(false)}
          startPath={path}
        />
      )}
      <div className="bg-white border-2 border-amber-300 rounded-lg p-4 space-y-3">
        <div className="flex gap-2">
          <input type="text" value={path} onChange={e => setPath(e.target.value)}
            className="flex-1 min-w-0 rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
          <button type="button" onClick={() => setShowBrowser(true)}
            className="shrink-0 flex items-center px-3 py-2 text-sm border border-stone-300 rounded-md text-stone-600 hover:bg-stone-50 transition-colors">
            <FolderOpen size={14} />
          </button>
        </div>
        <SettingsFields s={settings} onChange={setSettings} />
        {error && <p className="text-sm text-red-600">{error}</p>}
        <div className="flex gap-2">
          <button onClick={save} disabled={saving}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-stone-800 text-white hover:bg-stone-700 disabled:opacity-50 transition-colors">
            <Check size={14} /> {saving ? 'Saving…' : 'Save'}
          </button>
          <button onClick={onCancel}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-stone-300 text-stone-700 hover:bg-stone-50 transition-colors">
            <X size={14} /> Cancel
          </button>
        </div>
      </div>
    </>
  )
}

// ---- Main page ----

export function Directories() {
  const [dirs, setDirs] = useState<Directory[]>([])
  const [editingId, setEditingId] = useState<number | null>(null)
  const [adding, setAdding] = useState(false)
  const [addPaths, setAddPaths] = useState<string[]>([''])
  const [addSettings, setAddSettings] = useState<Settings>(defaultSettings)
  const [browserForIndex, setBrowserForIndex] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [copiedFrom, setCopiedFrom] = useState<string | null>(null)

  const load = useCallback(async () => {
    try { setDirs(await api.listDirectories() ?? []) } catch {}
  }, [])

  useEffect(() => { load() }, [load])

  const startAdd = (fromDir?: Directory) => {
    setAddPaths([''])
    if (fromDir) {
      setAddSettings({
        enabled: fromDir.Enabled,
        min_age_days: fromDir.MinAgeDays,
        max_bitrate: fromDir.MaxBitrate,
        min_size_mb: fromDir.MinSizeMB,
        bitrate_skip_margin: fromDir.BitrateSkipMargin,
      })
      setCopiedFrom(fromDir.Path)
    } else {
      setAddSettings(defaultSettings)
      setCopiedFrom(null)
    }
    setEditingId(null)
    setAdding(true)
    setError('')
  }

  const addPathRow = () => setAddPaths(p => [...p, ''])
  const removePathRow = (i: number) => setAddPaths(p => p.filter((_, j) => j !== i))
  const setPath = (i: number, val: string) =>
    setAddPaths(p => p.map((v, j) => j === i ? val : v))

  const saveAll = async () => {
    const paths = addPaths.map(p => p.trim()).filter(Boolean)
    if (paths.length === 0) { setError('Add at least one path'); return }
    setSaving(true)
    setError('')
    try {
      await api.batchCreateDirectories({ paths, ...addSettings })
      await load()
      setAdding(false)
      setCopiedFrom(null)
    } catch (e: any) {
      setError(e.message ?? 'Save failed')
      await load()
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: number) => {
    if (!confirm('Delete this directory? Existing jobs will remain in history.')) return
    try { await api.deleteDirectory(id); await load() }
    catch (e: any) { setError(e.message || 'Delete failed') }
  }

  const filledPaths = addPaths.filter(p => p.trim()).length

  return (
    <div className="p-6 space-y-6">
      {/* Browser for add rows */}
      {browserForIndex !== null && (
        <DirectoryBrowser
          onSelect={path => { setPath(browserForIndex, path); setBrowserForIndex(null) }}
          onClose={() => setBrowserForIndex(null)}
          startPath={dirs[0]?.Path}
        />
      )}

      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-stone-900">Directories</h1>
        {!adding && (
          <button onClick={() => startAdd()}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-stone-800 text-white hover:bg-stone-700 transition-colors">
            <Plus size={14} /> Add Directory
          </button>
        )}
      </div>

      {/* Add form — multi-path */}
      {adding && (
        <div className="bg-white border border-stone-200 rounded-lg p-5 space-y-4">
          <h2 className="text-sm font-medium text-stone-700">Add Directories</h2>
          {copiedFrom && (
            <div className="flex items-center gap-2 px-3 py-2 bg-amber-50 border border-amber-200 rounded-md text-xs text-amber-800">
              <Copy size={12} className="shrink-0" />
              <span className="truncate">Settings copied from <span className="font-mono font-medium">{copiedFrom}</span></span>
              <button onClick={() => { setCopiedFrom(null); setAddSettings(defaultSettings) }}
                className="ml-auto shrink-0 text-amber-500 hover:text-amber-700"><X size={12} /></button>
            </div>
          )}

          <div className="space-y-2">
            <span className="text-xs font-medium text-stone-600">Paths</span>
            {addPaths.map((path, i) => (
              <div key={i} className="flex gap-2">
                <input type="text" value={path}
                  onChange={e => setPath(i, e.target.value)}
                  placeholder="/media/Videos/Movies"
                  className="flex-1 min-w-0 rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-amber-500 focus:outline-hidden" />
                <button type="button" onClick={() => setBrowserForIndex(i)}
                  className="shrink-0 flex items-center px-3 py-2 text-sm border border-stone-300 rounded-md text-stone-600 hover:bg-stone-50 transition-colors"
                  title="Browse filesystem">
                  <FolderOpen size={14} />
                </button>
                {addPaths.length > 1 && (
                  <button type="button" onClick={() => removePathRow(i)}
                    className="shrink-0 flex items-center px-2 text-stone-400 hover:text-red-500 transition-colors">
                    <X size={14} />
                  </button>
                )}
              </div>
            ))}
            <button type="button" onClick={addPathRow}
              className="flex items-center gap-1.5 text-xs text-stone-500 hover:text-stone-800 transition-colors mt-1">
              <Plus size={12} /> Add another directory
            </button>
          </div>

          <div className="pt-3 border-t border-stone-100 space-y-3">
            <span className="text-xs font-medium text-stone-500">Settings applied to all</span>
            <SettingsFields s={addSettings} onChange={setAddSettings} />
          </div>

          {error && <p className="text-sm text-red-600">{error}</p>}

          <div className="flex gap-2">
            <button onClick={saveAll} disabled={saving}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-stone-800 text-white hover:bg-stone-700 disabled:opacity-50 transition-colors">
              <Check size={14} />
              {saving ? 'Saving…' : filledPaths > 1 ? `Save ${filledPaths} Directories` : 'Save'}
            </button>
            <button onClick={() => { setAdding(false); setError(''); setCopiedFrom(null) }}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-stone-300 text-stone-700 hover:bg-stone-50 transition-colors">
              <X size={14} /> Cancel
            </button>
          </div>
        </div>
      )}

      {/* Directory list — edit is inline */}
      {dirs.length === 0 && !adding ? (
        <p className="text-sm text-stone-400 py-8 text-center">No directories configured. Add one to get started.</p>
      ) : (
        <div className="space-y-3">
          {dirs.map(d => (
            editingId === d.ID ? (
              <EditCard key={d.ID} dir={d}
                onSave={async () => { await load(); setEditingId(null) }}
                onCancel={() => setEditingId(null)} />
            ) : (
              <div key={d.ID}
                className={`bg-white border rounded-lg px-4 py-3 flex items-start gap-3 ${
                  d.Enabled ? 'border-stone-200' : 'border-stone-100 opacity-60'
                }`}>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-stone-900 truncate">{d.Path}</p>
                  <p className="text-xs text-stone-400 mt-0.5">
                    Age ≥ {d.MinAgeDays}d &middot; Bitrate &gt; {formatBitrate(d.MaxBitrate)} +{Math.round(d.BitrateSkipMargin * 100)}% &middot; Size ≥ {d.MinSizeMB} MB
                  </p>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {!d.Enabled && (
                    <span className="text-xs text-stone-400 bg-stone-100 px-2 py-0.5 rounded-sm">Disabled</span>
                  )}
                  <button onClick={() => startAdd(d)}
                    className="text-stone-400 hover:text-amber-600 transition-colors"
                    title="Copy settings to new directories">
                    <Copy size={14} />
                  </button>
                  <button onClick={() => { setEditingId(d.ID); setAdding(false); setError('') }}
                    className="text-stone-400 hover:text-stone-700 transition-colors">
                    <Pencil size={14} />
                  </button>
                  <button onClick={() => remove(d.ID)}
                    className="text-stone-400 hover:text-red-500 transition-colors">
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>
            )
          ))}
        </div>
      )}
    </div>
  )
}
