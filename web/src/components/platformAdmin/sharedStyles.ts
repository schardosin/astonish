import type { CSSProperties } from 'react'

// ---------------------------------------------------------------------------
// Style constants (matching UserManagement)
// ---------------------------------------------------------------------------

export const gradientAmber: CSSProperties = { background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' }
export const inputStyle: CSSProperties = { background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }
export const errorBg: CSSProperties = { background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }
export const successBg: CSSProperties = { background: 'rgba(34, 197, 94, 0.1)', color: '#22c55e', border: '1px solid rgba(34, 197, 94, 0.2)' }

// ---------------------------------------------------------------------------
// Shared prop interfaces
// ---------------------------------------------------------------------------

export interface InlineErrorProps {
  msg: string
}

export interface InlineSuccessProps {
  msg: string
}

export interface StatusBadgeProps {
  status: string
}

export interface RoleBadgeProps {
  role: string
}
