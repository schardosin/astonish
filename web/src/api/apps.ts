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

// --- Data proxy API ---

export interface AppDataResponse {
  requestId: string
  data?: unknown
  error?: string
}

/** Send a one-shot data request through the backend proxy. */
export async function fetchAppData(
  sourceId: string,
  args: Record<string, unknown> = {},
  requestId: string = '',
  appName: string = '',
): Promise<AppDataResponse> {
  const res = await fetch(`${API_BASE}/apps/data`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sourceId, args, requestId, appName }),
  })
  if (!res.ok) {
    return { requestId, error: `HTTP ${res.status}: ${res.statusText}` }
  }
  return res.json()
}

/** Send an action (mutation) request through the backend proxy. */
export async function fetchAppAction(
  actionId: string,
  payload: Record<string, unknown> = {},
  requestId: string = '',
): Promise<AppDataResponse> {
  const res = await fetch(`${API_BASE}/apps/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ actionId, payload, requestId }),
  })
  if (!res.ok) {
    return { requestId, error: `HTTP ${res.status}: ${res.statusText}` }
  }
  return res.json()
}

// --- AI proxy API ---

export interface AppAIResponse {
  requestId: string
  text?: string
  error?: string
}

/** Send a one-shot AI (LLM) request through the backend proxy. */
export async function fetchAppAI(
  prompt: string,
  system: string = '',
  context: unknown = null,
  requestId: string = '',
): Promise<AppAIResponse> {
  const res = await fetch(`${API_BASE}/apps/ai`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ prompt, system, context, requestId }),
  })
  if (!res.ok) {
    return { requestId, error: `HTTP ${res.status}: ${res.statusText}` }
  }
  return res.json()
}

// --- App State (SQLite) API ---

export interface AppStateResponse {
  requestId: string
  data?: unknown
  error?: string
}

/** Execute a read-only SQL query against an app's persistent state database. */
export async function fetchAppStateQuery(
  appName: string,
  sql: string,
  params: unknown[] = [],
  requestId: string = '',
): Promise<AppStateResponse> {
  const res = await fetch(`${API_BASE}/apps/state/query`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ appName, sql, params, requestId }),
  })
  if (!res.ok) {
    return { requestId, error: `HTTP ${res.status}: ${res.statusText}` }
  }
  return res.json()
}

/** Execute a write/DDL SQL statement against an app's persistent state database. */
export async function fetchAppStateExec(
  appName: string,
  sql: string,
  params: unknown[] = [],
  requestId: string = '',
): Promise<AppStateResponse> {
  const res = await fetch(`${API_BASE}/apps/state/exec`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ appName, sql, params, requestId }),
  })
  if (!res.ok) {
    return { requestId, error: `HTTP ${res.status}: ${res.statusText}` }
  }
  return res.json()
}

/**
 * Open an SSE connection for streaming data updates from a saved app.
 * Returns a cleanup function to close the connection.
 */
export function connectAppStream(
  appName: string,
  sourceId: string,
  onUpdate: (data: unknown, error?: string) => void,
): () => void {
  const url = `${API_BASE}/apps/${encodeURIComponent(appName)}/stream?sourceId=${encodeURIComponent(sourceId)}`
  const eventSource = new EventSource(url)

  eventSource.addEventListener('data_update', (event: MessageEvent) => {
    try {
      const msg = JSON.parse(event.data)
      onUpdate(msg.data, msg.error)
    } catch {
      onUpdate(null, 'Failed to parse stream event')
    }
  })

  eventSource.onerror = () => {
    onUpdate(null, 'Stream connection error')
  }

  return () => {
    eventSource.close()
  }
}
