// Shared API and UI utilities for settings components

// Fetch full config from backend
export const fetchFullConfig = async () => {
  const res = await fetch('/api/settings/full')
  if (!res.ok) throw new Error('Failed to fetch config')
  return res.json()
}

// Save a single section of the full config
export const saveFullConfigSection = async (sectionKey, data) => {
  const res = await fetch('/api/settings/full', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ [sectionKey]: data })
  })
  if (!res.ok) throw new Error('Failed to save settings')
  return res.json()
}

// Common input styles
export const inputClass = 'w-full px-4 py-2.5 rounded-lg border text-sm'
export const inputStyle = {
  background: 'var(--bg-secondary)',
  borderColor: 'var(--border-color)',
  color: 'var(--text-primary)'
}

// Common label component style
export const labelStyle = { color: 'var(--text-secondary)' }
export const hintStyle = { color: 'var(--text-muted)' }
export const sectionBorderStyle = { borderColor: 'var(--border-color)' }

// Save button gradient
export const saveButtonStyle = {
  background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)'
}
