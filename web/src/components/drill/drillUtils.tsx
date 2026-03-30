import { CheckCircle2, XCircle, AlertCircle, MinusCircle } from 'lucide-react'

// ─── Helpers ───

export function formatTimeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays}d ago`
}

export function formatDuration(ms: number): string {
  if (!ms) return '—'
  if (ms < 1000) return `${ms}ms`
  const secs = (ms / 1000).toFixed(1)
  if (Number(secs) < 60) return `${secs}s`
  const mins = Math.floor(ms / 60000)
  const remSecs = Math.floor((ms % 60000) / 1000)
  return `${mins}m ${remSecs}s`
}

export interface StatusColors {
  dot: string
  bg: string
  border: string
  text: string
}

export function statusColor(status: string): StatusColors {
  switch (status) {
    case 'passed': return { dot: '#22c55e', bg: 'rgba(34, 197, 94, 0.1)', border: 'rgba(34, 197, 94, 0.3)', text: '#4ade80' }
    case 'failed': return { dot: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)', border: 'rgba(239, 68, 68, 0.3)', text: '#f87171' }
    case 'error':  return { dot: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)', border: 'rgba(245, 158, 11, 0.3)', text: '#fbbf24' }
    default:       return { dot: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)', border: 'rgba(107, 114, 128, 0.3)', text: '#9ca3af' }
  }
}

export function StatusDot({ status, size = 8 }: { status: string; size?: number }) {
  const color = statusColor(status)
  return (
    <span
      className="inline-block rounded-full flex-shrink-0"
      style={{ width: size, height: size, background: color.dot }}
      title={status || 'unknown'}
    />
  )
}

export function StatusBadge({ status }: { status: string }) {
  const color = statusColor(status)
  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium"
      style={{ background: color.bg, border: `1px solid ${color.border}`, color: color.text }}
    >
      {status === 'passed' && <CheckCircle2 size={10} />}
      {status === 'failed' && <XCircle size={10} />}
      {status === 'error' && <AlertCircle size={10} />}
      {!['passed', 'failed', 'error'].includes(status) && <MinusCircle size={10} />}
      {status || 'unknown'}
    </span>
  )
}

export interface SelectedItem {
  type: string
  key: string
}
