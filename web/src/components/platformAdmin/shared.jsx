import { AlertCircle, CheckCircle2 } from 'lucide-react'

// ---------------------------------------------------------------------------
// Style constants (matching UserManagement)
// ---------------------------------------------------------------------------

export const gradientAmber = { background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' }
export const inputStyle = { background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }
export const errorBg = { background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }
export const successBg = { background: 'rgba(34, 197, 94, 0.1)', color: '#22c55e', border: '1px solid rgba(34, 197, 94, 0.2)' }

// ---------------------------------------------------------------------------
// Shared UI components
// ---------------------------------------------------------------------------

export function InlineError({ msg }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={errorBg}>
      <AlertCircle size={14} /><span>{msg}</span>
    </div>
  )
}

export function InlineSuccess({ msg }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={successBg}>
      <CheckCircle2 size={14} /><span>{msg}</span>
    </div>
  )
}

export function StatusBadge({ status }) {
  const isActive = status === 'active'
  const isSuspended = status === 'suspended'
  const color = isActive ? '#22c55e' : isSuspended ? '#f59e0b' : '#ef4444'
  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{ background: `${color}20`, color }}
    >
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: color }} />
      {status}
    </span>
  )
}

export function RoleBadge({ role }) {
  const colors = {
    superadmin: { bg: 'rgba(234, 179, 8, 0.15)', fg: '#eab308' },
    owner: { bg: 'rgba(168, 85, 247, 0.15)', fg: '#a855f7' },
    admin: { bg: 'rgba(59, 130, 246, 0.15)', fg: '#3b82f6' },
    member: { bg: 'rgba(107, 114, 128, 0.15)', fg: '#6b7280' },
  }
  const c = colors[role] || colors.member
  return (
    <span className="px-2 py-0.5 rounded-full text-xs font-medium" style={{ background: c.bg, color: c.fg }}>
      {role}
    </span>
  )
}
