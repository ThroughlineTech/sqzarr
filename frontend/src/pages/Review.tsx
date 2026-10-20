import { useEffect, useState, useCallback } from 'react'
import { api, type OriginalRecord } from '../lib/api'
import { Card } from '../components/Card'
import { formatBytes, basename } from '../lib/utils'
import { Trash2, RotateCcw, BanIcon, Check } from 'lucide-react'
import { useLayoutContext } from '../context/LayoutContext'

function daysLabel(days: number): string {
  if (days === 0) return 'expires today'
  if (days === 1) return '1 day left'
  return `${days} days left`
}

function savings(rec: OriginalRecord): string | null {
  if (!rec.original_size || !rec.output_size) return null
  const saved = rec.original_size - rec.output_size
  if (saved <= 0) return null
  const pct = Math.round((saved / rec.original_size) * 100)
  return `${formatBytes(saved)} (${pct}%)`
}

export function Review() {
  const { refreshOriginals } = useLayoutContext()
  const [records, setRecords] = useState<OriginalRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmBulk, setConfirmBulk] = useState(false)

  const load = useCallback(async () => {
    try {
      const recs = await api.listOriginals()
      setRecords(recs ?? [])
    } catch {
      setRecords([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const toggleSelect = (id: number) => {
    setSelected(s => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const selectAll = () => {
    if (selected.size === records.length) setSelected(new Set())
    else setSelected(new Set(records.map(r => r.id)))
  }

  const handleDelete = async (id: number) => {
    try {
      await api.deleteOriginal(id)
      await load()
      refreshOriginals()
      setSelected(s => { const n = new Set(s); n.delete(id); return n })
    } catch (err: any) {
      setActionError(err.message || 'Delete failed')
    }
  }

  const handleRestore = async (id: number, exclude = false) => {
    try {
      await api.restoreOriginal(id, exclude)
      await load()
      refreshOriginals()
      setSelected(s => { const n = new Set(s); n.delete(id); return n })
    } catch (err: any) {
      setActionError(err.message || 'Restore failed')
    }
  }

  const handleBulkDelete = async () => {
    setConfirmBulk(false)
    const ids = Array.from(selected)
    for (const id of ids) {
      try { await api.deleteOriginal(id) } catch {}
    }
    setSelected(new Set())
    await load()
    refreshOriginals()
  }

  if (loading) return <div className="p-6 text-stone-400 text-sm">Loading…</div>

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-stone-900">Review</h1>
        <p className="text-xs text-stone-400">
          Originals held for review — delete to accept the transcoded version, restore to revert.
        </p>
      </div>

      {actionError && (
        <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded-lg px-4 py-2">
          {actionError}
          <button onClick={() => setActionError(null)} className="ml-3 text-red-400 hover:text-red-700">✕</button>
        </div>
      )}

      {records.length === 0 ? (
        <Card>
          <p className="text-sm text-stone-400 text-center py-6">
            Nothing to review — originals appear here after successful transcodes.
          </p>
        </Card>
      ) : (
        <>
          {/* Bulk controls */}
          <div className="flex items-center gap-3">
            <button
              onClick={selectAll}
              className="text-xs text-stone-500 hover:text-stone-800 underline"
            >
              {selected.size === records.length ? 'Deselect all' : `Select all (${records.length})`}
            </button>
            {selected.size > 0 && (
              <>
                <span className="text-stone-300 text-xs">|</span>
                {!confirmBulk ? (
                  <button
                    onClick={() => setConfirmBulk(true)}
                    className="text-xs text-red-600 hover:text-red-800 flex items-center gap-1"
                  >
                    <Trash2 size={11} /> Delete {selected.size} originals
                  </button>
                ) : (
                  <span className="text-xs text-red-600 flex items-center gap-2">
                    Confirm delete {selected.size} originals?
                    <button
                      onClick={handleBulkDelete}
                      className="text-xs bg-red-600 text-white px-2 py-0.5 rounded-sm hover:bg-red-700"
                    >
                      Yes, delete
                    </button>
                    <button
                      onClick={() => setConfirmBulk(false)}
                      className="text-xs text-stone-500 hover:text-stone-700"
                    >
                      Cancel
                    </button>
                  </span>
                )}
              </>
            )}
          </div>

          <div className="space-y-2">
            {records.map(rec => {
              const isSelected = selected.has(rec.id)
              const saved = savings(rec)
              const urgency = rec.days_remaining <= 1
                ? 'text-red-600'
                : rec.days_remaining <= 3
                ? 'text-amber-600'
                : 'text-stone-400'

              return (
                <div
                  key={rec.id}
                  className={`bg-white border rounded-lg px-4 py-3 transition-colors ${
                    isSelected ? 'border-amber-400 bg-amber-50' : 'border-stone-200'
                  }`}
                >
                  <div className="flex items-start gap-3">
                    {/* Checkbox */}
                    <button
                      onClick={() => toggleSelect(rec.id)}
                      className={`mt-0.5 shrink-0 w-4 h-4 rounded border-2 flex items-center justify-center transition-colors ${
                        isSelected
                          ? 'bg-amber-500 border-amber-500'
                          : 'border-stone-300 hover:border-amber-400'
                      }`}
                    >
                      {isSelected && <Check size={10} className="text-white" />}
                    </button>

                    {/* File info */}
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-stone-900 truncate">
                        {basename(rec.original_path)}
                      </p>
                      <p className="text-xs text-stone-400 truncate mt-0.5">{rec.original_path}</p>
                      <div className="flex items-center flex-wrap gap-x-3 gap-y-1 mt-1.5">
                        {rec.original_size > 0 && (
                          <span className="text-xs text-stone-500">
                            {formatBytes(rec.original_size)} → {formatBytes(rec.output_size)}
                          </span>
                        )}
                        {saved && (
                          <span className="text-xs text-green-600 font-medium">saved {saved}</span>
                        )}
                        <span className={`text-xs ${urgency}`}>{daysLabel(rec.days_remaining)}</span>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1 shrink-0">
                      {/* Delete original (accept transcoded) */}
                      <button
                        onClick={() => handleDelete(rec.id)}
                        title="Delete original (keep transcoded)"
                        className="p-1.5 text-stone-400 hover:text-green-600 hover:bg-green-50 rounded-sm transition-colors"
                      >
                        <Trash2 size={14} />
                      </button>
                      {/* Restore original */}
                      <button
                        onClick={() => handleRestore(rec.id, false)}
                        title="Restore original (remove transcoded, re-queue later)"
                        className="p-1.5 text-stone-400 hover:text-amber-600 hover:bg-amber-50 rounded-sm transition-colors"
                      >
                        <RotateCcw size={14} />
                      </button>
                      {/* Restore + exclude */}
                      <button
                        onClick={() => handleRestore(rec.id, true)}
                        title="Restore original and exclude from future scans"
                        className="p-1.5 text-stone-400 hover:text-red-500 hover:bg-red-50 rounded-sm transition-colors"
                      >
                        <BanIcon size={14} />
                      </button>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}
