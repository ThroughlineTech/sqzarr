import { cn } from '../lib/utils'
import type { Job } from '../lib/api'

const config: Record<Job['Status'], { label: string; className: string }> = {
  pending:   { label: 'Pending',   className: 'bg-stone-100 text-stone-600' },
  running:   { label: 'Running',   className: 'bg-amber-100 text-amber-700' },
  done:      { label: 'Done',      className: 'bg-green-100 text-green-700' },
  staged:    { label: 'Staged',    className: 'bg-teal-100 text-teal-700' },
  failed:    { label: 'Failed',    className: 'bg-red-100 text-red-700' },
  cancelled: { label: 'Cancelled', className: 'bg-stone-100 text-stone-500' },
  skipped:   { label: 'Skipped',   className: 'bg-stone-100 text-stone-500' },
  excluded:  { label: 'Excluded',  className: 'bg-orange-100 text-orange-700' },
  error:     { label: 'Error',     className: 'bg-red-100 text-red-700' },
  restored:  { label: 'Restored',  className: 'bg-blue-100 text-blue-700' },
}

export function StatusBadge({ status }: { status: Job['Status'] }) {
  const { label, className } = config[status] ?? config.pending
  return (
    <span className={cn('inline-flex items-center px-2 py-0.5 rounded-sm text-xs font-medium', className)}>
      {label}
    </span>
  )
}
