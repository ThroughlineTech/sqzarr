import { useEffect, useState, useCallback } from 'react'
import { api, type Status, type Job } from '../lib/api'
import { useSSE } from '../hooks/useSSE'
import { Card, CardTitle } from '../components/Card'
import { ProgressBar } from '../components/ProgressBar'
import { StatusBadge } from '../components/StatusBadge'
import { formatBytes, formatDuration, basename } from '../lib/utils'
import { Link } from 'react-router-dom'
import { Play, Pause, RefreshCw, HardDrive, Cpu, Zap, AlertTriangle, X } from 'lucide-react'

function relativeTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function timeUntil(iso: string | null | undefined): string {
  if (!iso) return ''
  const diff = new Date(iso).getTime() - Date.now()
  if (diff <= 0) return 'now'
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins}m`
  const hrs = Math.floor(mins / 60)
  const remMins = mins % 60
  return remMins > 0 ? `${hrs}h ${remMins}m` : `${hrs}h`
}

function formatETA(speed: number, progress: number, sourceDuration: number): string {
  if (speed <= 0 || progress <= 0 || progress >= 1) return ''
  const remainingContent = sourceDuration * (1 - progress) // seconds of content left
  const etaSecs = remainingContent / speed
  if (etaSecs < 60) return `${Math.round(etaSecs)}s`
  const mins = Math.floor(etaSecs / 60)
  if (mins < 60) return `${mins}m`
  return `${Math.floor(mins / 60)}h ${mins % 60}m`
}

function MeterBar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = Math.min(100, max > 0 ? (value / max) * 100 : 0)
  return (
    <div className="h-1.5 bg-stone-100 rounded-full overflow-hidden">
      <div className={`h-full rounded-full transition-all duration-700 ${color}`} style={{ width: `${pct}%` }} />
    </div>
  )
}

export function Dashboard() {
  const [status, setStatus] = useState<Status | null>(null)
  const [runningJob, setRunningJob] = useState<Job | null>(null)
  const [scanning, setScanning] = useState(false)
  const [jobSpeed, setJobSpeed] = useState(0)
  const [jobFPS, setJobFPS] = useState(0)
  const [logLines, setLogLines] = useState<string[]>([])

  const load = useCallback(async () => {
    try {
      const [s, jobs] = await Promise.all([
        api.getStatus(),
        api.listJobs('running', 1, 0),
      ])
      setStatus(s)
      setRunningJob(jobs?.[0] ?? null)
      if (!jobs?.[0]) {
        setJobSpeed(0)
        setJobFPS(0)
      }
    } catch {}
  }, [])

  useEffect(() => { load() }, [load])

  // Poll status every 3s for CPU/GPU updates even when no job is running
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const s = await api.getStatus()
        setStatus(s)
      } catch {}
    }, 3000)
    return () => clearInterval(interval)
  }, [])

  // Poll ffmpeg log every 2s while a job is running
  useEffect(() => {
    if (!runningJob) { setLogLines([]); return }
    const poll = async () => {
      try {
        const res = await api.getJobLog(runningJob.ID)
        setLogLines(res.lines ?? [])
      } catch {}
    }
    poll()
    const interval = setInterval(poll, 2000)
    return () => clearInterval(interval)
  }, [runningJob?.ID])

  useSSE((e) => {
    if (e.Type === 'progress' && runningJob?.ID === e.JobID) {
      setRunningJob(j => j ? { ...j, Progress: e.Progress } : j)
      if (e.Speed > 0) setJobSpeed(e.Speed)
      if (e.FPS > 0) setJobFPS(e.FPS)
    }
    if (e.Type === 'done' || e.Type === 'failed') {
      setJobSpeed(0)
      setJobFPS(0)
      load()
    }
  })

  const handleScanNow = async () => {
    setScanning(true)
    try { await api.triggerScan() } finally {
      setTimeout(() => setScanning(false), 2000)
      setTimeout(() => load(), 3000)
    }
  }

  const handleTogglePause = async () => {
    if (!status) return
    if (status.paused) await api.resumeQueue()
    else await api.pauseQueue()
    await load()
  }

  const hasGPUPercent = status && status.gpu_percent >= 0
  const gpuDisplay = hasGPUPercent
    ? { label: `${status!.gpu_percent.toFixed(0)}%`, value: status!.gpu_percent, max: 100 }
    : { label: status && status.gpu_mhz > 0 ? `${status.gpu_mhz} MHz` : '—', value: status?.gpu_mhz ?? 0, max: 1600 }
  const gpuIdle = runningJob && status && (hasGPUPercent ? status.gpu_percent < 5 : status.gpu_mhz < 100)

  const cancelRunning = async () => {
    if (!runningJob) return
    try {
      await api.cancelJob(runningJob.ID)
      load()
    } catch {}
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-stone-900">Dashboard</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={handleTogglePause}
            disabled={!status}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-stone-300 text-stone-700 hover:bg-stone-50 disabled:opacity-40 transition-colors"
          >
            {status?.paused ? <Play size={14} /> : <Pause size={14} />}
            {status?.paused ? 'Resume' : 'Pause'}
          </button>
          <div className="flex flex-col items-end gap-0.5">
            <button
              onClick={handleScanNow}
              disabled={scanning}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-stone-800 text-white hover:bg-stone-700 disabled:opacity-40 transition-colors"
            >
              <RefreshCw size={14} className={scanning ? 'animate-spin' : ''} />
              Scan Now
            </button>
            {status && (status.next_scan_at || status.last_scan_at) && (
              <p className="text-xs text-stone-400">
                {status.last_scan_at && `Last: ${relativeTime(status.last_scan_at)}`}
                {status.last_scan_at && status.next_scan_at && ' · '}
                {status.next_scan_at && `Next: in ${timeUntil(status.next_scan_at)}`}
              </p>
            )}
          </div>
        </div>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Link to="/savings">
          <Card className="cursor-pointer hover:border-amber-300 transition-colors">
            <CardTitle>Space Saved</CardTitle>
            <p className="text-2xl font-semibold text-stone-900">
              {status ? formatBytes(status.total_saved_gb * 1024 * 1024 * 1024) : '—'}
            </p>
          </Card>
        </Link>
        <Link to="/history?status=done">
          <Card className="cursor-pointer hover:border-stone-300 transition-colors">
            <CardTitle>Jobs Done</CardTitle>
            <p className="text-2xl font-semibold text-stone-900">{status?.jobs_done ?? '—'}</p>
          </Card>
        </Link>
        <Link to="/history?status=failed">
          <Card className="cursor-pointer hover:border-red-300 transition-colors">
            <CardTitle>Failed</CardTitle>
            <p className="text-2xl font-semibold text-red-600">{status?.jobs_failed ?? '—'}</p>
          </Card>
        </Link>
        <div title="Queue processing status — use the Pause/Resume button above to control">
        <Card>
          <CardTitle>Status</CardTitle>
          <div className="flex items-center gap-2 mt-1">
            <span className={`w-2 h-2 rounded-full ${status?.paused ? 'bg-stone-400' : 'bg-green-500'}`} />
            <span className="text-sm font-medium text-stone-700">
              {status?.paused ? 'Paused' : 'Running'}
            </span>
          </div>
        </Card>
        </div>
      </div>

      {/* System health */}
      {status && (
        <Card>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            {/* CPU */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-xs font-medium text-stone-500 flex items-center gap-1">
                  <Cpu size={12} /> CPU
                </span>
                <span className="text-xs font-semibold text-stone-700">
                  {status.cpu_percent.toFixed(0)}%
                </span>
              </div>
              <MeterBar
                value={status.cpu_percent}
                max={100}
                color={status.cpu_percent > 85 ? 'bg-red-400' : status.cpu_percent > 60 ? 'bg-amber-400' : 'bg-stone-400'}
              />
            </div>

            {/* GPU */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-xs font-medium text-stone-500 flex items-center gap-1.5">
                  <Zap size={12} /> GPU
                  {!hasGPUPercent && <span className="text-stone-300 text-[10px]">MHz clock</span>}
                  {gpuIdle && (
                    <span className="flex items-center gap-0.5 text-amber-600 font-medium">
                      <AlertTriangle size={10} /> idle?
                    </span>
                  )}
                </span>
                <span className="text-xs font-semibold text-stone-700">{gpuDisplay.label}</span>
              </div>
              <MeterBar
                value={gpuDisplay.value}
                max={gpuDisplay.max}
                color={gpuDisplay.value > 0 ? 'bg-amber-400' : 'bg-stone-100'}
              />
            </div>

            {/* Disk */}
            <div>
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-xs font-medium text-stone-500 flex items-center gap-1">
                  <HardDrive size={12} /> Disk
                  {status.disk_path && (
                    <span className="font-mono text-stone-400 truncate max-w-[80px]">{status.disk_path}</span>
                  )}
                </span>
                <span className="text-xs font-semibold text-stone-700">
                  {status.disk_free_gb >= 0 ? `${status.disk_free_gb.toFixed(1)} GB free` : '—'}
                </span>
              </div>
              <MeterBar value={1} max={1} color="bg-stone-200" />
            </div>
          </div>

          {/* Encoder */}
          <div className="mt-3 pt-3 border-t border-stone-100 flex items-center gap-1.5">
            <Cpu size={12} className="text-stone-400" />
            <span className="text-xs text-stone-400">{status.encoder}</span>
          </div>
        </Card>
      )}

      {/* Active job */}
      {runningJob && (
        <Card>
          <div className="flex items-start justify-between mb-3">
            <div className="min-w-0 flex-1">
              <p className="text-xs text-stone-500 mb-1 flex items-center gap-1">
                <Cpu size={12} /> Transcoding
              </p>
              <p className="text-sm font-medium text-stone-900 truncate">
                {basename(runningJob.SourcePath)}
              </p>
              <p className="text-xs text-stone-500 mt-0.5 truncate">{runningJob.SourcePath}</p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <StatusBadge status={runningJob.Status} />
              <button
                onClick={cancelRunning}
                className="text-stone-400 hover:text-red-500 transition-colors"
                title="Cancel job"
              >
                <X size={15} />
              </button>
            </div>
          </div>
          {runningJob.SourceDuration > 0
            ? <ProgressBar value={runningJob.Progress} />
            : <div className="h-1.5 bg-stone-100 rounded-full overflow-hidden">
                <div className="h-full bg-stone-300 rounded-full animate-pulse w-full" />
              </div>
          }
          <div className="flex items-center gap-3 mt-1.5 text-xs text-stone-500">
            <span>{runningJob.SourceDuration > 0 ? `${Math.round(runningJob.Progress * 100)}%` : '…'}</span>
            {runningJob.SourceDuration > 0 && <><span>&middot;</span><span>{formatDuration(runningJob.SourceDuration)}</span></>}
            {runningJob.SourceSize > 0 && <><span>&middot;</span><span>{formatBytes(runningJob.SourceSize)}</span></>}
            {jobSpeed > 0 && (
              <>
                <span>&middot;</span>
                <span className={`font-medium ${jobSpeed < 0.8 ? 'text-red-500' : jobSpeed < 1.5 ? 'text-amber-600' : 'text-green-600'}`}>
                  {jobSpeed.toFixed(1)}×
                </span>
              </>
            )}
            {jobFPS > 0 && (
              <>
                <span>&middot;</span>
                <span>{Math.round(jobFPS)} fps</span>
              </>
            )}
            {jobSpeed > 0 && runningJob.Progress > 0 && (
              <>
                <span>&middot;</span>
                <span>ETA {formatETA(jobSpeed, runningJob.Progress, runningJob.SourceDuration)}</span>
              </>
            )}
          </div>
          {jobSpeed > 0 && jobSpeed < 0.8 && (
            <p className="mt-2 text-xs text-red-600 flex items-center gap-1">
              <AlertTriangle size={12} />
              Encode speed below 1× — GPU may not be active. Check encoder selection.
            </p>
          )}
          {logLines.length > 0 && (
            <pre className="mt-3 p-2 bg-stone-50 rounded-sm text-[10px] leading-relaxed text-stone-500 overflow-x-auto max-h-40 overflow-y-auto font-mono whitespace-pre-wrap break-all">
              {logLines.join('\n')}
            </pre>
          )}
        </Card>
      )}
    </div>
  )
}
