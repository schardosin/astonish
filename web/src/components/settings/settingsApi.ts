// Shared API and UI utilities for settings components
import type { CSSProperties } from 'react'

// --- Types ---

export interface FullConfig {
  [sectionKey: string]: Record<string, unknown>
}

// --- API Functions ---

export const fetchFullConfig = async (): Promise<FullConfig> => {
  const res = await fetch('/api/settings/full')
  if (!res.ok) throw new Error('Failed to fetch config')
  return res.json()
}

export const saveFullConfigSection = async (sectionKey: string, data: Record<string, unknown>): Promise<Record<string, unknown>> => {
  const res = await fetch('/api/settings/full', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ [sectionKey]: data })
  })
  if (!res.ok) throw new Error('Failed to save settings')
  return res.json()
}

// --- Common Styles ---

export const inputClass: string = 'w-full px-4 py-2.5 rounded-lg border text-sm'
export const inputStyle: CSSProperties = {
  background: 'var(--bg-secondary)',
  borderColor: 'var(--border-color)',
  color: 'var(--text-primary)'
}

export const labelStyle: CSSProperties = { color: 'var(--text-secondary)' }
export const hintStyle: CSSProperties = { color: 'var(--text-muted)' }
export const sectionBorderStyle: CSSProperties = { borderColor: 'var(--border-color)' }

export const saveButtonStyle: CSSProperties = {
  background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)'
}
