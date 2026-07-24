import { useEffect, useRef, useState, useCallback } from 'react'
import { api } from '../lib/api'
import { Trash2, RefreshCw } from 'lucide-react'

const POLL_MS = 5000
const FETCH_LINES = 1000

interface ParsedLine {
  raw: string
  time?: string
  level?: string
  msg?: string
  fields: [string, unknown][]
}

function parseLine(raw: string): ParsedLine {
  try {
    const { time, level, msg, ...rest } = JSON.parse(raw)
    return { raw, time, level, msg, fields: Object.entries(rest) }
  } catch {
    return { raw, fields: [] }
  }
}

function levelClass(level?: string): string {
  switch (level) {
    case 'ERROR': return 'text-red-600 font-semibold'
    case 'WARN':  return 'text-amber-600 font-semibold'
    case 'DEBUG': return 'text-stone-400'
    default:      return 'text-stone-500'
  }
}

function formatTime(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleTimeString([], { hour12: false })
}

export function Logs() {
  const [lines, setLines] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [clearing, setClearing] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)

  const load = useCallback(async () => {
    try {
      const res = await api.getLogs(FETCH_LINES)
      setLines(res.lines ?? [])
      setError('')
    } catch (e: any) {
      setError(e.message || 'Failed to load logs')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const interval = setInterval(load, POLL_MS)
    return () => clearInterval(interval)
  }, [load])

  useEffect(() => {
    if (stickToBottom.current) bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [lines])

  const clearLogs = async () => {
    if (!confirm('Clear the application log? This cannot be undone.')) return
    setClearing(true)
    try {
      await api.clearLogs()
      await load()
    } catch (e: any) {
      setError(e.message || 'Failed to clear logs')
    } finally {
      setClearing(false)
    }
  }

  const parsed = lines.map(parseLine)

  return (
    <div className="p-6 space-y-4 flex flex-col h-screen">
      <div className="flex items-center justify-between shrink-0">
        <div>
          <h1 className="text-xl font-semibold text-stone-900">Logs</h1>
          <p className="text-xs text-stone-400 mt-0.5">
            Live application log &middot; refreshes every {POLL_MS / 1000}s
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={load}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm border border-stone-300 text-stone-600 hover:bg-stone-50 rounded-md transition-colors"
          >
            <RefreshCw size={14} /> Refresh
          </button>
          <button
            onClick={clearLogs}
            disabled={clearing || lines.length === 0}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm border border-stone-300 text-stone-600 hover:bg-stone-50 rounded-md transition-colors disabled:opacity-50"
          >
            <Trash2 size={14} />
            {clearing ? 'Clearing…' : 'Clear'}
          </button>
        </div>
      </div>

      {error && <p className="text-sm text-red-600 shrink-0">{error}</p>}

      <div
        className="flex-1 min-h-0 bg-white border border-stone-200 rounded-lg overflow-y-auto font-mono text-[11px] leading-relaxed"
        onScroll={e => {
          const el = e.currentTarget
          stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        }}
      >
        {loading ? (
          <p className="text-sm text-stone-400 text-center py-8">Loading…</p>
        ) : parsed.length === 0 ? (
          <p className="text-sm text-stone-400 text-center py-8">No log entries</p>
        ) : (
          <>
            {parsed.map((l, i) => (
              <div
                key={i}
                className="px-3 py-1 border-b border-stone-100 last:border-0 hover:bg-stone-50 whitespace-pre-wrap break-all"
              >
                {l.time || l.level || l.msg ? (
                  <>
                    {l.time && <span className="text-stone-400">{formatTime(l.time)} </span>}
                    {l.level && <span className={levelClass(l.level)}>{l.level.padEnd(5)} </span>}
                    <span className="text-stone-800">{l.msg}</span>
                    {l.fields.length > 0 && (
                      <span className="text-stone-400">
                        {' '}
                        {l.fields.map(([k, v]) => `${k}=${typeof v === 'object' ? JSON.stringify(v) : v}`).join(' ')}
                      </span>
                    )}
                  </>
                ) : (
                  <span className="text-stone-800">{l.raw}</span>
                )}
              </div>
            ))}
            <div ref={bottomRef} />
          </>
        )}
      </div>
    </div>
  )
}
