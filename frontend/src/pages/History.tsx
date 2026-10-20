import { useEffect, useState, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, type Job } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'
import { formatBytes, timeAgo, basename } from '../lib/utils'
import { ChevronDown, ChevronUp, RotateCcw, Trash2, ArrowRight } from 'lucide-react'

const PAGE_SIZE = 50
const STATUS_FILTERS = ['all', 'done', 'staged', 'failed', 'cancelled', 'skipped', 'excluded', 'error'] as const

export function History() {
  const [searchParams, setSearchParams] = useSearchParams()
  const statusParam = searchParams.get('status') ?? 'all'

  const [jobs, setJobs] = useState<Job[]>([])
  const [offset, setOffset] = useState(0)
  const [hasMore, setHasMore] = useState(true)
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [clearing, setClearing] = useState(false)

  const activeFilter = STATUS_FILTERS.includes(statusParam as any) ? statusParam : 'all'

  const load = useCallback(async (reset = false) => {
    const off = reset ? 0 : offset
    const statusArg = activeFilter === 'all' ? undefined : activeFilter
    try {
      const results = await api.listJobs(statusArg, PAGE_SIZE, off)
      const list = results ?? []
      if (reset) {
        setJobs(list)
        setOffset(list.length)
      } else {
        setJobs(j => [...j, ...list])
        setOffset(o => o + list.length)
      }
      setHasMore(list.length === PAGE_SIZE)
    } catch {}
  }, [offset, activeFilter])

  useEffect(() => { load(true) }, [activeFilter])

  const retry = async (id: number) => {
    try {
      await api.retryJob(id)
      load(true)
    } catch {}
  }

  const clearHistory = async () => {
    if (!confirm('Clear all completed and terminal jobs from history? Files will not be re-processed.')) return
    setClearing(true)
    try {
      await api.clearHistory()
      load(true)
    } catch {} finally {
      setClearing(false)
    }
  }

  const toggle = (id: number) =>
    setExpanded(s => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold text-stone-900">History</h1>
        {jobs.length > 0 && (
          <button
            onClick={clearHistory}
            disabled={clearing}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm border border-stone-300 text-stone-600 hover:bg-stone-50 rounded-md transition-colors disabled:opacity-50"
          >
            <Trash2 size={14} />
            {clearing ? 'Clearing…' : 'Clear History'}
          </button>
        )}
      </div>

      {/* Status filter pills */}
      <div className="flex flex-wrap gap-1.5 mb-4">
        {STATUS_FILTERS.map(f => (
          <button
            key={f}
            onClick={() => {
              if (f === 'all') {
                setSearchParams({})
              } else {
                setSearchParams({ status: f })
              }
            }}
            className={`px-3 py-1 text-xs font-medium rounded-full border transition-colors ${
              activeFilter === f
                ? 'bg-stone-800 text-white border-stone-800'
                : 'bg-white text-stone-600 border-stone-300 hover:bg-stone-50'
            }`}
          >
            {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
          </button>
        ))}
      </div>

      {jobs.length === 0 ? (
        <p className="text-sm text-stone-400 py-8 text-center">No jobs yet</p>
      ) : (
        <div className="space-y-2">
          {jobs.map(job => {
            const isExpanded = expanded.has(job.ID)
            const saved = job.BytesSaved?.Valid ? job.BytesSaved.Int64 : null
            const errMsg = job.ErrorMessage?.Valid ? job.ErrorMessage.String : null

            return (
              <div
                key={job.ID}
                className="bg-white border border-stone-200 rounded-lg overflow-hidden"
              >
                <div
                  className="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-stone-50"
                  onClick={() => toggle(job.ID)}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-stone-900 truncate">{basename(job.SourcePath)}</p>
                    <p className="text-xs text-stone-400">
                      {timeAgo(job.CreatedAt)}
                      {saved !== null && saved > 0 && (
                        <> &middot; <span className="text-green-600">saved {formatBytes(saved)}</span></>
                      )}
                    </p>
                  </div>
                  <StatusBadge status={job.Status} />
                  {job.EncoderUsed?.Valid && /software|libx265/i.test(job.EncoderUsed.String) && (
                    <span className="text-xs bg-amber-100 text-amber-700 font-medium px-1.5 py-0.5 rounded-sm shrink-0" title="Fell back to software encode — GPU was not used">
                      SW
                    </span>
                  )}
                  {(job.Status === 'failed' || job.Status === 'cancelled' || job.Status === 'error') && (
                    <button
                      onClick={e => { e.stopPropagation(); retry(job.ID) }}
                      className="text-stone-400 hover:text-amber-600 transition-colors"
                      title="Retry"
                    >
                      <RotateCcw size={14} />
                    </button>
                  )}
                  {isExpanded ? <ChevronUp size={14} className="text-stone-400" /> : <ChevronDown size={14} className="text-stone-400" />}
                </div>

                {isExpanded && (
                  <div className="border-t border-stone-100 px-4 py-3 bg-stone-50 text-xs text-stone-600 space-y-1">
                    <p className="text-stone-400 break-all">{job.SourcePath}</p>
                    {(() => {
                      const outputBytes = job.OutputSize?.Valid ? job.OutputSize.Int64 : null
                      const savedBytes  = job.BytesSaved?.Valid ? job.BytesSaved.Int64 : null
                      // Derive source size: stored directly, or reconstruct from output + saved.
                      const sourceBytes = job.SourceSize > 0
                        ? job.SourceSize
                        : (outputBytes !== null && savedBytes !== null ? outputBytes + savedBytes : null)
                      const pct = sourceBytes && savedBytes && sourceBytes > 0
                        ? Math.round((savedBytes / sourceBytes) * 100)
                        : null

                      return (job.Status === 'done' || job.Status === 'staged') ? (
                        <>
                          <div className="flex flex-wrap gap-x-4 gap-y-0.5 mt-1">
                            {sourceBytes !== null && (
                              <span>
                                <span className="text-stone-400">Before: </span>
                                {formatBytes(sourceBytes)}
                                {job.SourceCodec && <span className="text-stone-400"> · {job.SourceCodec}</span>}
                              </span>
                            )}
                            {outputBytes !== null && (
                              <span>
                                <span className="text-stone-400">After: </span>
                                {formatBytes(outputBytes)}
                                {job.EncoderUsed?.Valid && <span className="text-stone-400"> · {job.EncoderUsed.String}</span>}
                              </span>
                            )}
                            {savedBytes !== null && savedBytes > 0 && (
                              <span className="text-green-700 font-medium">
                                Saved {formatBytes(savedBytes)}{pct !== null ? ` (${pct}%)` : ''}
                              </span>
                            )}
                          </div>
                          {job.Status === 'staged' && (
                            <p className="mt-2 text-stone-400">
                              Original held for review.{' '}
                              <a href="/review" className="text-amber-600 hover:text-amber-700 inline-flex items-center gap-0.5">
                                Go to Review <ArrowRight size={11} />
                              </a>
                            </p>
                          )}
                        </>
                      ) : (
                        <>
                          {job.SourceCodec && <p><span className="text-stone-400">Codec: </span>{job.SourceCodec}</p>}
                          {sourceBytes !== null && <p><span className="text-stone-400">Size: </span>{formatBytes(sourceBytes)}</p>}
                          {job.EncoderUsed?.Valid && <p><span className="text-stone-400">Encoder: </span>{job.EncoderUsed.String}</p>}
                        </>
                      )
                    })()}
                    {errMsg && (
                      <div className="mt-1">
                        <pre className="whitespace-pre-wrap break-all text-red-600 bg-red-50 border border-red-100 rounded-sm p-2 font-mono text-xs max-h-48 overflow-y-auto">
                          {errMsg}
                        </pre>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}

          {hasMore && (
            <button
              onClick={() => load()}
              className="w-full py-2 text-sm text-stone-500 hover:text-stone-700 transition-colors"
            >
              Load more
            </button>
          )}
        </div>
      )}
    </div>
  )
}
