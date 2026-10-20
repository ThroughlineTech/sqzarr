import { useEffect, useState, useCallback, useMemo } from 'react'
import { useBlocker } from 'react-router-dom'
import { api, type RuntimeConfig, type Status, type FSBrowseResult, type EncoderOption } from '../lib/api'
import { Card } from '../components/Card'
import { Cpu, Shield, Server, RefreshCw, FolderOpen, Plus, X, ChevronLeft, Lock } from 'lucide-react'
import { basename } from '../lib/utils'

// Unrestricted directory browser — used only for selecting root directories.
function RootDirBrowser({ onSelect, onClose }: { onSelect: (path: string) => void; onClose: () => void }) {
  const [browse, setBrowse] = useState<FSBrowseResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const navigate = useCallback(async (path: string | undefined) => {
    setLoading(true)
    setError('')
    try {
      setBrowse(await api.browseFS(path, false, true))
    } catch (e: any) {
      setError(e.message || 'Cannot read directory')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { navigate(undefined) }, [navigate])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-md mx-4 flex flex-col max-h-[70vh]">
        <div className="flex items-center justify-between px-4 py-3 border-b border-stone-200">
          <h3 className="text-sm font-semibold text-stone-900">Select root directory</h3>
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
                <button
                  onClick={() => navigate(browse.parent)}
                  className="w-full flex items-center gap-2 px-4 py-2.5 text-sm text-stone-600 hover:bg-stone-50 border-b border-stone-100"
                >
                  <ChevronLeft size={14} className="text-stone-400" /> Parent directory
                </button>
              )}
              {browse?.dirs.length === 0 && (
                <p className="text-sm text-stone-400 text-center py-6">No subdirectories</p>
              )}
              {browse?.dirs.map(dir => (
                <button
                  key={dir}
                  onClick={() => navigate(dir)}
                  className="w-full flex items-center gap-2 px-4 py-2.5 text-sm text-stone-700 hover:bg-stone-50 border-b border-stone-100 last:border-0"
                >
                  <FolderOpen size={14} className="text-amber-500 shrink-0" />
                  <span className="truncate">{basename(dir)}</span>
                </button>
              ))}
            </>
          )}
        </div>
        <div className="flex items-center justify-between gap-2 px-4 py-3 border-t border-stone-200">
          <button
            onClick={() => browse?.parent && navigate(browse.parent)}
            disabled={!browse?.parent}
            className="flex items-center gap-1 text-sm text-stone-600 hover:text-stone-900 disabled:opacity-40 transition-colors"
          >
            <ChevronLeft size={14} /> Parent
          </button>
          <button
            onClick={() => browse && onSelect(browse.current)}
            disabled={!browse}
            className="px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-50 text-white rounded-md transition-colors"
          >
            Select This Folder
          </button>
        </div>
      </div>
    </div>
  )
}

export function Settings() {
  const [status, setStatus] = useState<Status | null>(null)
  const [cfg, setCfg] = useState<RuntimeConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveResult, setSaveResult] = useState<'ok' | string | null>(null)
  const [encoders, setEncoders] = useState<EncoderOption[]>([])
  const [selectedEncoder, setSelectedEncoder] = useState('')
  const [hardwareDecode, setHardwareDecode] = useState<'auto' | 'off'>('auto')

  // Editable form state — initialised from cfg once loaded
  const [concurrency, setConcurrency] = useState(1)
  const [intervalHours, setIntervalHours] = useState(6)
  const [processedDirName, setProcessedDirName] = useState('.processed')
  const [retentionDays, setRetentionDays] = useState(10)
  const [failThreshold, setFailThreshold] = useState(1)
  const [systemFailThreshold, setSystemFailThreshold] = useState(5)
  const [deleteConfirmSingle, setDeleteConfirmSingle] = useState(false)
  const [plexEnabled, setPlexEnabled] = useState(false)
  const [plexURL, setPlexURL] = useState('')
  const [plexToken, setPlexToken] = useState('')
  const [rootDirs, setRootDirs] = useState<string[]>([])
  const [newRootInput, setNewRootInput] = useState('')
  const [showRootBrowser, setShowRootBrowser] = useState(false)
  const [plexChecking, setPlexChecking] = useState(false)
  const [plexResult, setPlexResult] = useState<{ ok: boolean; message: string } | null>(null)
  const plexDirty = !!cfg && (plexEnabled !== cfg.plex_enabled || plexURL !== cfg.plex_base_url || plexToken !== '')

  useEffect(() => { setPlexResult(null) }, [plexEnabled, plexURL, plexToken])

  const handleVerifyPlex = async () => {
    setPlexChecking(true)
    setPlexResult(null)
    try {
      const result = await api.verifyPlex()
      setPlexResult({ ok: true, message: result.library_count > 0
        ? `Connected to Plex. Library access confirmed (${result.library_count} libraries).`
        : 'Connected to Plex, but no libraries are accessible. Check the server libraries and token permissions.' })
    } catch (err) {
      setPlexResult({ ok: false, message: err instanceof Error ? err.message : 'Plex verification failed' })
    } finally {
      setPlexChecking(false)
    }
  }

  // Password change form state
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [pwSaving, setPwSaving] = useState(false)
  const [pwResult, setPwResult] = useState<'ok' | string | null>(null)

  const isDirty = useMemo(() => {
    if (!cfg) return false
    const rootsMatch =
      JSON.stringify([...rootDirs].sort()) ===
      JSON.stringify([...(cfg.root_dirs ?? [])].sort())
    return (
      concurrency !== cfg.worker_concurrency ||
      intervalHours !== cfg.scan_interval_hours ||
      processedDirName !== cfg.processed_dir_name ||
      retentionDays !== cfg.originals_retention_days ||
      failThreshold !== cfg.fail_threshold ||
      systemFailThreshold !== cfg.system_fail_threshold ||
      deleteConfirmSingle !== cfg.delete_confirm_single ||
      plexEnabled !== cfg.plex_enabled ||
      plexURL !== cfg.plex_base_url ||
      plexToken !== '' ||
      selectedEncoder !== cfg.encoder ||
      hardwareDecode !== cfg.hardware_decode ||
      !rootsMatch
    )
  }, [cfg, concurrency, intervalHours, processedDirName, retentionDays, failThreshold,
      systemFailThreshold, deleteConfirmSingle, plexEnabled, plexURL, plexToken, rootDirs, selectedEncoder, hardwareDecode])

  const blocker = useBlocker(isDirty)

  useEffect(() => {
    if (!isDirty) return
    const handler = (e: BeforeUnloadEvent) => { e.preventDefault() }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [isDirty])

  useEffect(() => {
    Promise.all([api.getStatus(), api.getConfig(), api.getEncoders()])
      .then(([s, c, enc]) => {
        setStatus(s)
        setCfg(c)
        setConcurrency(c.worker_concurrency)
        setIntervalHours(c.scan_interval_hours)
        setProcessedDirName(c.processed_dir_name)
        setRetentionDays(c.originals_retention_days)
        setFailThreshold(c.fail_threshold)
        setSystemFailThreshold(c.system_fail_threshold)
        setDeleteConfirmSingle(c.delete_confirm_single)
        setPlexEnabled(c.plex_enabled)
        setPlexURL(c.plex_base_url)
        setRootDirs(c.root_dirs ?? [])
        setEncoders(enc)
        setSelectedEncoder(c.encoder)
        setHardwareDecode(c.hardware_decode)
      })
      .catch(() => {})
  }, [])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setSaveResult(null)
    try {
      const updates: Parameters<typeof api.updateConfig>[0] = {
        worker_concurrency: concurrency,
        scan_interval_hours: intervalHours,
        processed_dir_name: processedDirName,
        originals_retention_days: retentionDays,
        fail_threshold: failThreshold,
        system_fail_threshold: systemFailThreshold,
        delete_confirm_single: deleteConfirmSingle,
        plex_enabled: plexEnabled,
        plex_base_url: plexURL,
        root_dirs: rootDirs,
        encoder: selectedEncoder,
        hardware_decode: hardwareDecode,
      }
      if (plexToken) {
        updates.plex_token = plexToken
      }
      const updated = await api.updateConfig(updates)
      setCfg(updated)
      setSelectedEncoder(updated.encoder)
      setPlexToken('')
      // Refresh encoder list to update active flags.
      api.getEncoders().then(setEncoders).catch(() => {})
      setSaveResult('ok')
      setTimeout(() => setSaveResult(null), 3000)
    } catch (err: any) {
      setSaveResult(err.message || 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="p-6 space-y-6">
      {showRootBrowser && (
        <RootDirBrowser
          onSelect={path => {
            if (!rootDirs.includes(path)) setRootDirs(dirs => [...dirs, path])
            setShowRootBrowser(false)
          }}
          onClose={() => setShowRootBrowser(false)}
        />
      )}

      {/* Navigation blocker dialog */}
      {blocker.state === 'blocked' && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-sm mx-4 p-6 space-y-4">
            <h3 className="text-sm font-semibold text-stone-900">Unsaved changes</h3>
            <p className="text-sm text-stone-600">You have unsaved settings. Leave without saving?</p>
            <div className="flex gap-2 justify-end">
              <button
                onClick={() => blocker.reset()}
                className="px-3 py-1.5 text-sm border border-stone-300 text-stone-700 hover:bg-stone-50 rounded-md transition-colors"
              >
                Stay
              </button>
              <button
                onClick={() => blocker.proceed()}
                className="px-3 py-1.5 text-sm bg-red-500 hover:bg-red-600 text-white rounded-md transition-colors"
              >
                Leave anyway
              </button>
            </div>
          </div>
        </div>
      )}

      <h1 className="text-xl font-semibold text-stone-900">Settings</h1>

      {cfg && (
        <form onSubmit={handleSave} className="space-y-6">
          {/* Hardware Encoder */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <Cpu size={14} /> Hardware
            </h2>
            <Card className="space-y-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <label className="text-sm text-stone-700">Active encoder</label>
                  <p className="text-xs text-stone-400 mt-0.5">Select the encoder used for transcoding jobs</p>
                </div>
                <select
                  value={selectedEncoder}
                  onChange={e => setSelectedEncoder(e.target.value)}
                  className="w-56 text-sm border border-stone-300 rounded-md px-2 py-1.5 focus:outline-hidden focus:ring-2 focus:ring-amber-400 bg-white"
                >
                  {encoders.map(enc => (
                    <option key={enc.type} value={enc.type}>
                      {enc.display_name}
                    </option>
                  ))}
                </select>
              </div>
              {selectedEncoder === 'software' && (
                <p className="text-xs text-amber-600">
                  Software encoding (libx265) is significantly slower than hardware encoding. Use only as a fallback.
                </p>
              )}
              {selectedEncoder === 'vaapi' && (
                <>
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <label htmlFor="hardware-decode" className="text-sm text-stone-700">Hardware decoding</label>
                      <p className="text-xs text-stone-400 mt-0.5">Automatic checks supported H.264 video and falls back when needed.</p>
                    </div>
                    <select id="hardware-decode" value={hardwareDecode}
                      onChange={e => setHardwareDecode(e.target.value as 'auto' | 'off')}
                      className="w-56 text-sm border border-stone-300 rounded-md px-2 py-1.5 focus:outline-hidden focus:ring-2 focus:ring-amber-400 bg-white">
                      <option value="auto">Automatic</option>
                      <option value="off">Off</option>
                    </select>
                  </div>
                  <p className="text-xs text-stone-500">
                    {cfg.low_power_status === 'qualified'
                      ? 'Low-power bitrate-controlled encoding is qualified; each supported pipeline is checked before use.'
                      : cfg.low_power_status === 'off'
                        ? 'Low-power encoding is off. Standard bitrate-controlled encoding is used.'
                        : 'Standard bitrate-controlled encoding. Low-power encoding has not been qualified on this host.'}
                    {' '}
                    The selected pipeline and any fallback are recorded in each job's log.
                  </p>
                </>
              )}
            </Card>
          </section>

          {/* Root Directories */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <FolderOpen size={14} /> Root Directories
            </h2>
            <Card className="space-y-3">
              <p className="text-xs text-stone-400">
                Filesystem roots the scanner and file picker are allowed to access.
                At least one root is required before scan directories can be added.
              </p>
              {rootDirs.length === 0 && (
                <p className="text-xs text-amber-600">No roots configured — add one below.</p>
              )}
              {rootDirs.map((dir, i) => (
                <div key={i} className="flex items-center gap-2">
                  <span className="flex-1 text-sm font-mono text-stone-800 bg-stone-50 border border-stone-200 rounded-md px-3 py-1.5 truncate">
                    {dir}
                  </span>
                  <button
                    type="button"
                    onClick={() => setRootDirs(dirs => dirs.filter((_, j) => j !== i))}
                    className="shrink-0 text-stone-400 hover:text-red-500 transition-colors"
                    title="Remove root"
                  >
                    <X size={15} />
                  </button>
                </div>
              ))}
              <div className="flex gap-2 pt-1">
                <input
                  type="text"
                  value={newRootInput}
                  onChange={e => setNewRootInput(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      const v = newRootInput.trim()
                      if (v && !rootDirs.includes(v)) {
                        setRootDirs(dirs => [...dirs, v])
                        setNewRootInput('')
                      }
                    }
                  }}
                  placeholder="/mnt/Videos"
                  className="flex-1 min-w-0 text-sm border border-stone-300 rounded-md px-3 py-1.5 font-mono focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                />
                <button
                  type="button"
                  onClick={() => setShowRootBrowser(true)}
                  className="shrink-0 px-3 py-1.5 text-sm border border-stone-300 text-stone-600 hover:bg-stone-50 rounded-md transition-colors"
                  title="Browse filesystem"
                >
                  <FolderOpen size={15} />
                </button>
                <button
                  type="button"
                  onClick={() => {
                    const v = newRootInput.trim()
                    if (v && !rootDirs.includes(v)) {
                      setRootDirs(dirs => [...dirs, v])
                      setNewRootInput('')
                    }
                  }}
                  disabled={!newRootInput.trim()}
                  className="shrink-0 flex items-center gap-1 px-3 py-1.5 text-sm bg-stone-800 text-white hover:bg-stone-700 disabled:opacity-40 rounded-md transition-colors"
                >
                  <Plus size={14} /> Add
                </button>
              </div>
            </Card>
          </section>

          {/* Transcoding */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <Cpu size={14} /> Transcoding
            </h2>
            <Card className="space-y-4">
              <div className="flex items-center justify-between">
                <label className="text-sm text-stone-700">Worker concurrency</label>
                <input
                  type="number"
                  min={1}
                  max={8}
                  value={concurrency}
                  onChange={e => setConcurrency(Number(e.target.value))}
                  className="w-20 text-sm border border-stone-300 rounded-md px-2 py-1 text-right focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                />
              </div>
              <p className="text-xs text-stone-400">1–8 parallel workers. Applied live — no restart needed.</p>
            </Card>
          </section>

          {/* Scanning */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <RefreshCw size={14} /> Scanning
            </h2>
            <Card className="space-y-4">
              <div className="flex items-center justify-between">
                <label className="text-sm text-stone-700">Scan interval</label>
                <div className="flex items-center gap-1.5">
                  <input
                    type="number"
                    min={1}
                    value={intervalHours}
                    onChange={e => setIntervalHours(Number(e.target.value))}
                    className="w-20 text-sm border border-stone-300 rounded-md px-2 py-1 text-right focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                  />
                  <span className="text-sm text-stone-500">hours</span>
                </div>
              </div>
            </Card>
          </section>

          {/* Safety */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <Shield size={14} /> Safety
            </h2>
            <Card className="space-y-4">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <label className="text-sm text-stone-700">Processed directory name</label>
                  <p className="text-xs text-stone-400 mt-0.5">Originals are held here after transcoding (relative to each root directory)</p>
                </div>
                <input
                  type="text"
                  value={processedDirName}
                  onChange={e => setProcessedDirName(e.target.value)}
                  className="w-36 text-sm border border-stone-300 rounded-md px-2 py-1 font-mono focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-sm text-stone-700">Originals retention</label>
                  <p className="text-xs text-stone-400 mt-0.5">Days before held originals are automatically deleted</p>
                </div>
                <div className="flex items-center gap-1.5">
                  <input
                    type="number"
                    min={1}
                    value={retentionDays}
                    onChange={e => setRetentionDays(Number(e.target.value))}
                    className="w-20 text-sm border border-stone-300 rounded-md px-2 py-1 text-right focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                  />
                  <span className="text-sm text-stone-500">days</span>
                </div>
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-sm text-stone-700">Per-file fail threshold</label>
                  <p className="text-xs text-stone-400 mt-0.5">Failures before a file is permanently excluded</p>
                </div>
                <input
                  type="number"
                  min={1}
                  value={failThreshold}
                  onChange={e => setFailThreshold(Number(e.target.value))}
                  className="w-20 text-sm border border-stone-300 rounded-md px-2 py-1 text-right focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-sm text-stone-700">System fail threshold</label>
                  <p className="text-xs text-stone-400 mt-0.5">Consecutive failures before the queue auto-pauses</p>
                </div>
                <input
                  type="number"
                  min={1}
                  value={systemFailThreshold}
                  onChange={e => setSystemFailThreshold(Number(e.target.value))}
                  className="w-20 text-sm border border-stone-300 rounded-md px-2 py-1 text-right focus:outline-hidden focus:ring-2 focus:ring-amber-400"
                />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-sm text-stone-700">Confirm before deleting a single original</label>
                  <p className="text-xs text-stone-400 mt-0.5">Bulk deletes always show a confirmation dialog</p>
                </div>
                <button
                  type="button"
                  onClick={() => setDeleteConfirmSingle(v => !v)}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    deleteConfirmSingle ? 'bg-amber-500' : 'bg-stone-300'
                  }`}
                >
                  <span className={`inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform ${
                    deleteConfirmSingle ? 'translate-x-4' : 'translate-x-1'
                  }`} />
                </button>
              </div>
            </Card>
          </section>

          {/* Plex */}
          <section>
            <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
              <Server size={14} /> Plex
            </h2>
            <Card className="space-y-4">
              <div className="flex items-center justify-between">
                <label className="text-sm text-stone-700">Plex integration</label>
                <button
                  type="button"
                  onClick={() => setPlexEnabled(v => !v)}
                  disabled={plexChecking}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                    plexEnabled ? 'bg-amber-500' : 'bg-stone-300'
                  }`}
                >
                  <span className={`inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform ${
                    plexEnabled ? 'translate-x-4' : 'translate-x-1'
                  }`} />
                </button>
              </div>
              <div className="flex items-center justify-between gap-4">
                <label className="text-sm text-stone-700 shrink-0">Plex URL</label>
                <input
                  type="text"
                  value={plexURL}
                  onChange={e => setPlexURL(e.target.value)}
                  disabled={!plexEnabled || plexChecking}
                  placeholder="http://192.168.1.10:32400"
                  className="flex-1 min-w-0 text-sm border border-stone-300 rounded-md px-2 py-1 focus:outline-hidden focus:ring-2 focus:ring-amber-400 disabled:opacity-40"
                />
              </div>
              <div className="flex items-center justify-between gap-4">
                <label className="text-sm text-stone-700 shrink-0">Plex token</label>
                <input
                  type="password"
                  value={plexToken}
                  onChange={e => setPlexToken(e.target.value)}
                  disabled={!plexEnabled || plexChecking}
                  placeholder={cfg.plex_token === 'SET' ? '●●●●●●●● (set)' : 'paste token here'}
                  className="flex-1 min-w-0 text-sm border border-stone-300 rounded-md px-2 py-1 focus:outline-hidden focus:ring-2 focus:ring-amber-400 disabled:opacity-40"
                />
              </div>
              <div className="space-y-2">
                <button
                  type="button"
                  onClick={handleVerifyPlex}
                  disabled={plexChecking || saving || !plexEnabled || plexDirty}
                  className="px-3 py-1.5 text-sm border border-stone-300 text-stone-700 hover:bg-stone-50 rounded-md disabled:opacity-50 transition-colors"
                >
                  {plexChecking ? 'Verifying…' : 'Verify Plex connection'}
                </button>
                {plexDirty && <p className="text-xs text-stone-500">Save Plex settings before verifying.</p>}
                <div role="status" aria-live="polite">
                  {plexResult && <p className={`text-sm ${plexResult.ok ? 'text-green-700' : 'text-red-600'}`}>{plexResult.message}</p>}
                </div>
              </div>
            </Card>
          </section>

          <div className="flex items-center justify-end gap-3">
            {saveResult === 'ok' && (
              <span className="text-sm text-green-700">Settings saved</span>
            )}
            {saveResult && saveResult !== 'ok' && (
              <span className="text-sm text-red-600">{saveResult}</span>
            )}
            <button
              type="submit"
              disabled={saving || plexChecking || !isDirty}
              className="px-4 py-2 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-50 text-white rounded-md transition-colors"
            >
              {saving ? 'Saving…' : 'Save Settings'}
            </button>
          </div>
        </form>
      )}

      {/* Password Change */}
      <section>
        <h2 className="text-sm font-medium text-stone-500 mb-3 flex items-center gap-1.5">
          <Lock size={14} /> {cfg?.has_password ? 'Change Password' : 'Set Password'}
        </h2>
        <Card className="space-y-4">
          <div className="space-y-3">
            {cfg?.has_password && (
            <div className="flex items-center justify-between gap-4">
              <label className="text-sm text-stone-700 shrink-0">Current password</label>
              <input
                type="password"
                value={currentPassword}
                onChange={e => setCurrentPassword(e.target.value)}
                className="flex-1 min-w-0 max-w-xs text-sm border border-stone-300 rounded-md px-2 py-1.5 focus:outline-hidden focus:ring-2 focus:ring-amber-400"
              />
            </div>
            )}
            <div className="flex items-center justify-between gap-4">
              <label className="text-sm text-stone-700 shrink-0">New password</label>
              <input
                type="password"
                value={newPassword}
                onChange={e => setNewPassword(e.target.value)}
                className="flex-1 min-w-0 max-w-xs text-sm border border-stone-300 rounded-md px-2 py-1.5 focus:outline-hidden focus:ring-2 focus:ring-amber-400"
              />
            </div>
            <div className="flex items-center justify-between gap-4">
              <label className="text-sm text-stone-700 shrink-0">Confirm new password</label>
              <input
                type="password"
                value={confirmPassword}
                onChange={e => setConfirmPassword(e.target.value)}
                className="flex-1 min-w-0 max-w-xs text-sm border border-stone-300 rounded-md px-2 py-1.5 focus:outline-hidden focus:ring-2 focus:ring-amber-400"
              />
            </div>
            {confirmPassword && newPassword !== confirmPassword && (
              <p className="text-xs text-red-600">Passwords do not match</p>
            )}
            {newPassword && newPassword.length < 8 && (
              <p className="text-xs text-amber-600">Password must be at least 8 characters</p>
            )}
          </div>
          <div className="flex items-center justify-end gap-3">
            {pwResult === 'ok' && (
              <span className="text-sm text-green-700">Password changed</span>
            )}
            {pwResult && pwResult !== 'ok' && (
              <span className="text-sm text-red-600">{pwResult}</span>
            )}
            {cfg?.has_password && (
              <button
                type="button"
                disabled={pwSaving || !currentPassword}
                onClick={async () => {
                  if (!confirm('Remove password protection? The UI will be accessible without login.')) return
                  setPwSaving(true)
                  setPwResult(null)
                  try {
                    await api.removePassword(currentPassword)
                    setPwResult('ok')
                    setCurrentPassword('')
                    setNewPassword('')
                    setConfirmPassword('')
                    setCfg(prev => prev ? { ...prev, has_password: false } : prev)
                    setTimeout(() => setPwResult(null), 3000)
                  } catch (err: any) {
                    setPwResult(err.message || 'Failed to remove password')
                  } finally {
                    setPwSaving(false)
                  }
                }}
                className="px-4 py-2 text-sm bg-red-500 hover:bg-red-600 disabled:opacity-50 text-white rounded-md transition-colors"
              >
                Remove Password
              </button>
            )}
            <button
              type="button"
              disabled={
                pwSaving ||
                (cfg?.has_password && !currentPassword) ||
                !newPassword ||
                newPassword.length < 8 ||
                newPassword !== confirmPassword
              }
              onClick={async () => {
                setPwSaving(true)
                setPwResult(null)
                try {
                  await api.changePassword(currentPassword, newPassword)
                  setPwResult('ok')
                  setCurrentPassword('')
                  setNewPassword('')
                  setConfirmPassword('')
                  setCfg(prev => prev ? { ...prev, has_password: true } : prev)
                  setTimeout(() => setPwResult(null), 3000)
                } catch (err: any) {
                  setPwResult(err.message || 'Failed to change password')
                } finally {
                  setPwSaving(false)
                }
              }}
              className="px-4 py-2 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-50 text-white rounded-md transition-colors"
            >
              {pwSaving ? 'Saving...' : cfg?.has_password ? 'Change Password' : 'Set Password'}
            </button>
          </div>
        </Card>
      </section>

      {/* About */}
      <Card className="text-center">
        <p className="text-sm font-semibold text-stone-900">SQZARR {status?.version ?? ''}</p>
        <p className="text-xs text-stone-400 mt-1">Self-hosted GPU media transcoder</p>
        <a
          href="https://github.com/danrichardson/sqzarr"
          target="_blank"
          rel="noreferrer"
          className="text-xs text-amber-600 hover:underline mt-2 block"
        >
          github.com/danrichardson/sqzarr
        </a>
      </Card>
    </div>
  )
}
