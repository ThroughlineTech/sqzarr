import { useEffect, useState, useCallback } from 'react'
import { api, type Job, type FSBrowseResult, type FileEntry } from '../lib/api'
import { useSSE } from '../hooks/useSSE'
import { Card } from '../components/Card'
import { StatusBadge } from '../components/StatusBadge'
import { ProgressBar } from '../components/ProgressBar'
import { formatBytes, formatDuration, basename } from '../lib/utils'
import { X, Check, FolderOpen, ChevronLeft, ArrowUpDown, FolderPlus } from 'lucide-react'

type SortKey = 'name' | 'size' | 'date'

function codecBadge(codec: string | undefined) {
  if (!codec) return null
  const c = codec.toLowerCase()
  const isEfficient = c.includes('hevc') || c.includes('265') || c.includes('av1') || c.includes('vp9')
  const label = codec.length > 8 ? codec.slice(0, 8) : codec
  return (
    <span className={`text-[10px] px-1 py-0.5 rounded font-medium shrink-0 ${
      isEfficient ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'
    }`}>
      {label}
    </span>
  )
}

function jobStatusPill(status: string | undefined) {
  if (!status) return null
  const map: Record<string, string> = {
    done:      'bg-green-100 text-green-700',
    pending:   'bg-stone-100 text-stone-500',
    running:   'bg-amber-100 text-amber-700',
    failed:    'bg-red-100 text-red-600',
    cancelled: 'bg-stone-100 text-stone-400',
    skipped:   'bg-stone-100 text-stone-400',
  }
  return (
    <span className={`text-[10px] px-1 py-0.5 rounded-sm font-medium shrink-0 ${map[status] ?? 'bg-stone-100 text-stone-400'}`}>
      {status}
    </span>
  )
}

function sortFiles(files: FileEntry[], key: SortKey): FileEntry[] {
  return [...files].sort((a, b) => {
    if (key === 'size') return b.size - a.size
    if (key === 'date') return new Date(b.modified).getTime() - new Date(a.modified).getTime()
    return a.name.localeCompare(b.name)
  })
}

function relDate(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const days = Math.floor(diff / 86400000)
  if (days === 0) return 'today'
  if (days === 1) return 'yesterday'
  if (days < 30) return `${days}d ago`
  if (days < 365) return `${Math.floor(days / 30)}mo ago`
  return `${Math.floor(days / 365)}y ago`
}

// ---- File picker modal ----

function FilePicker({ onSelect, onQueueDir, onClose }: { onSelect: (paths: string[]) => void; onQueueDir: (dir: string) => void; onClose: () => void }) {
  const [browse, setBrowse] = useState<FSBrowseResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [sort, setSort] = useState<SortKey>('name')
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const navigate = useCallback(async (path: string) => {
    setLoading(true)
    setError('')
    setSelected(new Set())
    try {
      setBrowse(await api.browseFS(path, true))
    } catch (e: any) {
      setError(e.message || 'Cannot read directory')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { navigate('') }, [navigate])

  const sortedFiles = browse ? sortFiles(browse.files, sort) : []
  const allSelected = sortedFiles.length > 0 && sortedFiles.every(f => selected.has(f.path))

  const toggleFile = (path: string) => setSelected(prev => {
    const next = new Set(prev)
    if (next.has(path)) next.delete(path)
    else next.add(path)
    return next
  })

  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(sortedFiles.map(f => f.path)))
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl mx-4 flex flex-col max-h-[80vh]">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-stone-200">
          <h3 className="text-sm font-semibold text-stone-900">Browse for file</h3>
          <button onClick={onClose} className="text-stone-400 hover:text-stone-700"><X size={16} /></button>
        </div>
        {/* Path + sort + select-all */}
        <div className="flex items-center justify-between px-4 py-2 bg-stone-50 border-b border-stone-100 gap-3">
          <p className="text-xs font-mono text-stone-600 truncate flex-1">{browse?.current ?? '/'}</p>
          {sortedFiles.length > 0 && (
            <button
              onClick={toggleAll}
              className="text-xs text-stone-500 hover:text-stone-800 shrink-0 transition-colors"
            >
              {allSelected ? 'Deselect all' : 'Select all'}
            </button>
          )}
          {browse && browse.files.length > 1 && (
            <div className="flex items-center gap-1 shrink-0">
              <ArrowUpDown size={11} className="text-stone-400" />
              {(['name', 'size', 'date'] as SortKey[]).map(k => (
                <button
                  key={k}
                  onClick={() => setSort(k)}
                  className={`text-xs px-2 py-0.5 rounded-sm transition-colors ${sort === k ? 'bg-stone-800 text-white' : 'text-stone-500 hover:bg-stone-100'}`}
                >
                  {k}
                </button>
              ))}
            </div>
          )}
        </div>
        {/* List */}
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
              {browse?.dirs.map(dir => (
                <div
                  key={dir}
                  className="w-full flex items-center border-b border-stone-100"
                >
                  <button
                    onClick={() => navigate(dir)}
                    className="flex-1 flex items-center gap-2 px-4 py-2.5 text-sm text-stone-700 hover:bg-stone-50 min-w-0"
                  >
                    <FolderOpen size={14} className="text-amber-500 shrink-0" />
                    <span className="truncate">{basename(dir)}</span>
                  </button>
                  <button
                    onClick={() => onQueueDir(dir)}
                    title="Queue all files in this folder recursively"
                    className="shrink-0 px-3 py-2.5 text-stone-400 hover:text-amber-600 hover:bg-amber-50 transition-colors"
                  >
                    <FolderPlus size={14} />
                  </button>
                </div>
              ))}
              {sortedFiles.map(file => {
                const isSelected = selected.has(file.path)
                return (
                  <button
                    key={file.path}
                    onClick={() => toggleFile(file.path)}
                    className={`w-full flex items-start gap-2.5 px-4 py-2.5 border-b border-stone-100 last:border-0 text-left transition-colors ${
                      isSelected ? 'bg-amber-50' : 'hover:bg-stone-50'
                    }`}
                  >
                    <div className={`mt-0.5 shrink-0 w-3.5 h-3.5 rounded border flex items-center justify-center transition-colors ${
                      isSelected ? 'bg-amber-500 border-amber-500' : 'border-stone-300'
                    }`}>
                      {isSelected && <Check size={9} className="text-white" strokeWidth={3} />}
                    </div>
                    <div className="flex-1 min-w-0">
                      <p className="text-xs font-mono text-stone-900 truncate">{file.name}</p>
                      <div className="flex items-center flex-wrap gap-1.5 mt-0.5">
                        <span className="text-[11px] text-stone-500">{formatBytes(file.size)}</span>
                        <span className="text-stone-300 text-[10px]">·</span>
                        <span className="text-[11px] text-stone-400">{relDate(file.modified)}</span>
                        {file.duration ? (
                          <><span className="text-stone-300 text-[10px]">·</span>
                          <span className="text-[11px] text-stone-400">{formatDuration(file.duration)}</span></>
                        ) : null}
                        {file.bitrate ? (
                          <><span className="text-stone-300 text-[10px]">·</span>
                          <span className="text-[11px] text-stone-400">{Math.round(file.bitrate / 1000)} kbps</span></>
                        ) : null}
                        {file.bytes_saved && file.bytes_saved > 0 ? (
                          <><span className="text-stone-300 text-[10px]">·</span>
                          <span className="text-[11px] text-green-600">saved {formatBytes(file.bytes_saved)}</span></>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex flex-col items-end gap-1 shrink-0">
                      {codecBadge(file.codec)}
                      {jobStatusPill(file.job_status)}
                    </div>
                  </button>
                )
              })}
              {browse && browse.dirs.length === 0 && browse.files.length === 0 && (
                <p className="text-sm text-stone-400 text-center py-6">No video files or subdirectories</p>
              )}
            </>
          )}
        </div>
        {/* Footer */}
        {sortedFiles.length > 0 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-stone-200">
            <span className="text-xs text-stone-400">
              {selected.size > 0 ? `${selected.size} of ${sortedFiles.length} selected` : 'Click files to select'}
            </span>
            <button
              onClick={() => onSelect([...selected])}
              disabled={selected.size === 0}
              className="px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-40 text-white rounded-md transition-colors"
            >
              Queue {selected.size > 1 ? `${selected.size} files` : selected.size === 1 ? '1 file' : '…'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

// ---- Queue page ----

export function Queue() {
  const [running, setRunning] = useState<Job[]>([])
  const [pending, setPending] = useState<Job[]>([])
  const [manualPath, setManualPath] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitSuccess, setSubmitSuccess] = useState<string | false>(false)
  const [showPicker, setShowPicker] = useState(false)

  const load = useCallback(async () => {
    try {
      const [r, p] = await Promise.all([
        api.listJobs('running', 10, 0),
        api.listJobs('pending', 50, 0),
      ])
      setRunning(r ?? [])
      setPending(p ?? [])
    } catch {}
  }, [])

  useEffect(() => { load() }, [load])

  const handleManualSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!manualPath.trim()) return
    setSubmitting(true)
    setSubmitError(null)
    setSubmitSuccess(false)
    try {
      await api.createJob(manualPath.trim())
      setManualPath('')
      setSubmitSuccess('Added to queue')
      setTimeout(() => setSubmitSuccess(false), 3000)
      load()
    } catch (err: any) {
      setSubmitError(err.message || 'Failed to queue file')
    } finally {
      setSubmitting(false)
    }
  }

  useSSE((e) => {
    if (e.Type === 'progress') {
      setRunning(jobs => jobs.map(j =>
        j.ID === e.JobID ? { ...j, Progress: e.Progress } : j,
      ))
    }
    if (e.Type === 'done' || e.Type === 'failed') {
      load()
    }
  })

  const cancel = async (id: number) => {
    try {
      await api.cancelJob(id)
      await load()
    } catch {}
  }

  return (
    <div className="p-6 space-y-6">
      {showPicker && (
        <FilePicker
          onSelect={async paths => {
            setShowPicker(false)
            setSubmitting(true)
            setSubmitError(null)
            setSubmitSuccess(false)
            const failures: string[] = []
            for (const path of paths) {
              try { await api.createJob(path) }
              catch (err: any) { failures.push(err.message || 'unknown error') }
            }
            setSubmitting(false)
            if (failures.length > 0) {
              const reasons = [...new Set(failures)]
              const reasonText = reasons.length === 1 ? reasons[0] : reasons.join('; ')
              setSubmitError(`${failures.length} file${failures.length > 1 ? 's' : ''} failed to queue: ${reasonText}`)
            } else {
              setSubmitSuccess('Added to queue')
              setTimeout(() => setSubmitSuccess(false), 3000)
            }
            load()
          }}
          onQueueDir={async dir => {
            setShowPicker(false)
            setSubmitting(true)
            setSubmitError(null)
            setSubmitSuccess(false)
            try {
              const result = await api.enqueueDir(dir)
              if (result.queued > 0) {
                setSubmitSuccess(`Queued ${result.queued} file${result.queued !== 1 ? 's' : ''}${result.skipped > 0 ? ` (${result.skipped} already queued)` : ''}`)
                setTimeout(() => setSubmitSuccess(false), 4000)
              } else {
                setSubmitError('No new files to queue (all already queued or no video files found)')
              }
            } catch (err: any) {
              setSubmitError(err.message || 'Failed to queue directory')
            } finally {
              setSubmitting(false)
            }
            load()
          }}
          onClose={() => setShowPicker(false)}
        />
      )}

      <h1 className="text-xl font-semibold text-stone-900">Queue</h1>

      {/* Manual queue form */}
      <Card>
        <h2 className="text-sm font-medium text-stone-700 mb-3">Queue a file</h2>
        <form onSubmit={handleManualSubmit} className="flex gap-2">
          <input
            type="text"
            value={manualPath}
            onChange={e => setManualPath(e.target.value)}
            placeholder="/absolute/path/to/file.mkv"
            className="flex-1 min-w-0 text-sm border border-stone-300 rounded-md px-3 py-1.5 font-mono focus:outline-hidden focus:ring-2 focus:ring-amber-400"
          />
          <button
            type="button"
            onClick={() => setShowPicker(true)}
            className="shrink-0 px-3 py-1.5 text-sm border border-stone-300 text-stone-600 hover:bg-stone-50 rounded-md transition-colors"
            title="Browse for file"
          >
            <FolderOpen size={15} />
          </button>
          <button
            type="submit"
            disabled={submitting || !manualPath.trim()}
            className="shrink-0 px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 disabled:opacity-50 text-white rounded-md transition-colors"
          >
            Queue
          </button>
        </form>
        {submitSuccess && (
          <p className="mt-2 text-xs text-green-700 flex items-center gap-1">
            <Check size={12} /> {submitSuccess}
          </p>
        )}
        {submitError && (
          <p className="mt-2 text-xs text-red-600">{submitError}</p>
        )}
      </Card>

      {running.length > 0 && (
        <section>
          <h2 className="text-sm font-medium text-stone-500 mb-3">Running</h2>
          <div className="space-y-3">
            {running.map(job => (
              <Card key={job.ID}>
                <div className="flex items-start justify-between mb-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-stone-900 truncate">{basename(job.SourcePath)}</p>
                    <p className="text-xs text-stone-400 truncate">{job.SourcePath}</p>
                  </div>
                  <StatusBadge status={job.Status} />
                </div>
                <ProgressBar value={job.Progress} />
                <p className="text-xs text-stone-500 mt-1.5">
                  {Math.round(job.Progress * 100)}% &middot; {formatDuration(job.SourceDuration)} &middot; {formatBytes(job.SourceSize)}
                </p>
              </Card>
            ))}
          </div>
        </section>
      )}

      <section>
        <h2 className="text-sm font-medium text-stone-500 mb-3">
          Pending ({pending.length})
        </h2>
        {pending.length === 0 ? (
          <p className="text-sm text-stone-400 py-8 text-center">Queue is empty</p>
        ) : (
          <div className="space-y-2">
            {pending.map(job => (
              <div
                key={job.ID}
                className="flex items-center gap-3 bg-white border border-stone-200 rounded-lg px-4 py-3"
              >
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-stone-900 truncate">{basename(job.SourcePath)}</p>
                  <p className="text-xs text-stone-400 truncate">{job.SourcePath}</p>
                </div>
                <span className="text-xs text-stone-400 shrink-0">
                  {formatBytes(job.SourceSize)}
                </span>
                <button
                  onClick={() => cancel(job.ID)}
                  className="text-stone-400 hover:text-red-500 transition-colors shrink-0"
                  title="Cancel"
                >
                  <X size={15} />
                </button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
