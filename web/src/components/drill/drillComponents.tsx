import { CheckCircle2, XCircle, AlertCircle, MinusCircle } from 'lucide-react'
import { statusColor } from './drillUtils'

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
