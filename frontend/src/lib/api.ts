const BASE = '/api/v1'

function token(): string | null {
  return localStorage.getItem('sqzarr_token')
}

function headers(): HeadersInit {
  const t = token()
  return {
    'Content-Type': 'application/json',
    ...(t ? { Authorization: `Bearer ${t}` } : {}),
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers: headers(),
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    localStorage.removeItem('sqzarr_token')
    if (window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

// ---- Types ----

export interface Status {
  version: string
  encoder: string
  paused: boolean
  total_saved_gb: number
  jobs_done: number
  jobs_failed: number
  disk_free_gb: number
  disk_path: string
  next_scan_at: string | null
  last_scan_at: string | null
  cpu_percent: number
  gpu_mhz: number
  gpu_percent: number  // -1 = unavailable; use gpu_mhz as proxy instead
}

export interface Job {
  ID: number
  SourcePath: string
  SourceSize: number
  SourceCodec: string
  SourceDuration: number
  SourceBitrate: number
  Status: 'pending' | 'running' | 'done' | 'staged' | 'failed' | 'cancelled' | 'skipped' | 'excluded' | 'error' | 'restored'
  Progress: number
  BytesSaved: { Int64: number; Valid: boolean } | null
  OutputSize: { Int64: number; Valid: boolean } | null
  ErrorMessage: { String: string; Valid: boolean } | null
  EncoderUsed: { String: string; Valid: boolean } | null
  StartedAt: { Time: string; Valid: boolean } | null
  FinishedAt: { Time: string; Valid: boolean } | null
  CreatedAt: string
}

export interface Directory {
  ID: number
  Path: string
  Enabled: boolean
  MinAgeDays: number
  MaxBitrate: number
  MinSizeMB: number
  BitrateSkipMargin: number
  CreatedAt: string
  UpdatedAt: string
}

// Input type for create/update — matches Go JSON struct tags (snake_case)
export interface DirectoryInput {
  path?: string
  enabled?: boolean
  min_age_days?: number
  max_bitrate?: number
  min_size_mb?: number
  bitrate_skip_margin?: number
}

export interface Stats {
  TotalBytesSaved: number
  TotalJobsDone: number
  TotalJobsFailed: number
  UpdatedAt: string
}

export interface OriginalRecord {
  id: number
  job_id: number
  original_path: string
  held_path: string
  output_path: string
  original_size: number
  output_size: number
  expires_at: string
  created_at: string
  days_remaining: number
}

export interface RuntimeConfig {
  root_dirs: string[]
  worker_concurrency: number
  scan_interval_hours: number
  processed_dir_name: string
  originals_retention_days: number
  fail_threshold: number
  system_fail_threshold: number
  delete_confirm_single: boolean
  plex_enabled: boolean
  plex_base_url: string
  plex_token: string  // empty string = not set; "SET" = already configured
  encoder: string     // active encoder type: "vaapi", "nvenc", "videotoolbox", "software"
  hardware_decode: 'auto' | 'off'
  low_power: 'auto' | 'off'
  low_power_status: string
  has_password: boolean
}

export interface EncoderOption {
  type: string
  display_name: string
  active: boolean
}

export interface LastScanRun {
  ID: number
  FilesScanned: number
  FilesQueued: number
  FilesSkipped: number
  DurationMS: { Int64: number; Valid: boolean } | null
  StartedAt: string
  FinishedAt: { Time: string; Valid: boolean } | null
}

export interface FileEntry {
  path: string
  name: string
  size: number       // bytes
  modified: string   // ISO timestamp
  codec?: string
  bitrate?: number   // bps
  duration?: number  // seconds
  job_status?: string
  bytes_saved?: number
}

export interface FSBrowseResult {
  current: string
  parent: string
  dirs: string[]
  files: FileEntry[]
}

export interface SavingsEntry {
  id: number
  source_path: string
  source_size: number
  output_size: number
  bytes_saved: number
  finished_at: { Time: string; Valid: boolean } | null
}

// ---- API calls ----

export const api = {
  getStatus: () => request<Status>('GET', '/status'),
  getStats: () => request<Stats>('GET', '/stats'),

  listJobs: (status?: string, limit = 50, offset = 0) =>
    request<Job[]>('GET', `/jobs?${new URLSearchParams({
      ...(status ? { status } : {}),
      limit: String(limit),
      offset: String(offset),
    })}`),
  getJob: (id: number) => request<Job>('GET', `/jobs/${id}`),
  listSavings: () => request<SavingsEntry[]>('GET', '/jobs/savings'),
  createJob: (path: string) => request<Job>('POST', '/jobs', { path }),
  enqueueDir: (path: string) => request<{ queued: number; skipped: number }>('POST', '/jobs/enqueue-dir', { path }),
  cancelJob: (id: number) => request<void>('DELETE', `/jobs/${id}`),
  retryJob: (id: number) => request<void>('POST', `/jobs/${id}/retry`),

  listDirectories: () => request<Directory[]>('GET', '/directories'),
  createDirectory: (d: DirectoryInput) => request<Directory>('POST', '/directories', d),
  batchCreateDirectories: (req: {
    paths: string[]
    enabled: boolean
    min_age_days: number
    max_bitrate: number
    min_size_mb: number
    bitrate_skip_margin: number
  }) => request<Directory[]>('POST', '/directories/batch', req),
  updateDirectory: (id: number, d: DirectoryInput) =>
    request<Directory>('PUT', `/directories/${id}`, d),
  deleteDirectory: (id: number) => request<void>('DELETE', `/directories/${id}`),

  triggerScan: () => request<{ status: string }>('POST', '/scan'),
  lastScan: () => request<LastScanRun | null>('GET', '/scan/last'),
  pauseQueue: () => request<{ paused: boolean }>('POST', '/queue/pause'),
  resumeQueue: () => request<{ paused: boolean }>('POST', '/queue/resume'),

  getConfig: () => request<RuntimeConfig>('GET', '/config'),
  verifyPlex: () => request<{ library_count: number }>('POST', '/plex/verify'),
  updateConfig: (updates: Partial<RuntimeConfig> & { plex_token?: string; root_dirs?: string[]; encoder?: string }) =>
    request<RuntimeConfig>('PUT', '/config', updates),

  getEncoders: () => request<EncoderOption[]>('GET', '/encoders'),

  listOriginals: () => request<OriginalRecord[]>('GET', '/originals'),
  deleteOriginal: (id: number) => request<void>('DELETE', `/originals/${id}`),
  restoreOriginal: (id: number, exclude = false) =>
    request<void>('POST', `/originals/${id}/restore`, { exclude }),

  clearHistory: () => request<{ cleared: number }>('POST', '/jobs/clear'),

  getJobLog: (id: number, n = 20) =>
    request<{ lines: string[] }>('GET', `/jobs/${id}/log?n=${n}`),

  getLogs: (n = 1000) =>
    request<{ lines: string[] }>('GET', `/logs?n=${n}`),
  clearLogs: () => request<{ status: string }>('POST', '/logs/clear'),

  browseFS: (path: string | undefined, files = false, unrestricted = false) => {
    const params = new URLSearchParams({ files: files ? '1' : '0' })
    if (path) params.set('path', path)
    if (unrestricted) params.set('unrestricted', '1')
    return request<FSBrowseResult>('GET', `/fs?${params}`)
  },

  login: (password: string) =>
    request<{ token: string }>('POST', '/auth/login', { password }),

  changePassword: (current_password: string, new_password: string) =>
    request<{ status: string }>('POST', '/auth/change-password', { current_password, new_password }),

  removePassword: (current_password: string) =>
    request<{ status: string }>('DELETE', '/auth/password', { current_password }),
}

// ---- SSE ----

export type SSEEvent = {
  Type: 'progress' | 'done' | 'failed' | 'paused'
  JobID: number
  Progress: number
  Speed: number  // encode speed relative to realtime; 0 = not yet known
  FPS: number
  Error?: string
}

export function connectSSE(onEvent: (e: SSEEvent) => void): () => void {
  const t = token()
  const url = `${BASE}/ws${t ? `?token=${encodeURIComponent(t)}` : ''}`
  let es: EventSource
  let closed = false

  function connect() {
    if (closed) return
    es = new EventSource(url)
    es.onmessage = (e) => {
      try {
        onEvent(JSON.parse(e.data))
      } catch {}
    }
    es.onerror = () => {
      es.close()
      if (!closed) setTimeout(connect, 3000)
    }
  }

  connect()
  return () => {
    closed = true
    es?.close()
  }
}
