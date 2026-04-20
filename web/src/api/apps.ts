/**
 * API client for Visual Apps (generative UI persistence)
 */

const API_BASE = '/api'

// --- Types ---

export interface AppListItem {
  name: string
  description: string
  version: number
  updatedAt: string
}

export interface VisualApp {
  name: string
  description: string
  code: string
  version: number
  dataSources?: DataSource[]
  createdAt: string
  updatedAt: string
  sessionId?: string
}

export interface DataSource {
  id: string
  type: string
  config: Record<string, unknown>
  interval?: string
}

// --- API functions ---

export async function fetchApps(): Promise<{ apps: AppListItem[] }> {
  const res = await fetch(`${API_BASE}/apps`)
  if (!res.ok) throw new Error(`Failed to list apps: ${res.statusText}`)
  return res.json()
}

export async function fetchApp(name: string): Promise<VisualApp> {
  const res = await fetch(`${API_BASE}/apps/${encodeURIComponent(name)}`)
  if (!res.ok) throw new Error(`Failed to load app: ${res.statusText}`)
  return res.json()
}

export async function saveApp(
  name: string,
  data: { description: string; code: string; version: number; sessionId?: string }
): Promise<{ status: string; path: string; name: string }> {
  const res = await fetch(`${API_BASE}/apps/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) throw new Error(`Failed to save app: ${res.statusText}`)
  return res.json()
}

export async function deleteApp(name: string): Promise<{ status: string }> {
  const res = await fetch(`${API_BASE}/apps/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!res.ok) throw new Error(`Failed to delete app: ${res.statusText}`)
  return res.json()
}
