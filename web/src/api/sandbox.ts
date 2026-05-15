// Sandbox API client functions for the Studio UI.

import { teamFetch } from './teamContext'

// --- Types ---

export interface SandboxStatus {
  backend: string           // "incus" | "k8s"
  platform: string
  available: boolean
  reason?: string
  sandboxEnabled?: boolean
  runtimeAvailable?: boolean
  baseTemplateExists?: boolean
  // Deprecated: use runtimeAvailable
  incusAvailable?: boolean
  [key: string]: unknown
}

export interface OptionalTool {
  id: string
  name: string
  description: string
  installed: boolean
}

export interface InitSandboxParams {
  installTools: string[]
  onProgress: (message: string) => void
  onDone: () => void
  onError: (error: string) => void
}

export interface SandboxDetails {
  backend: string           // "incus" | "k8s"
  incus_version: string
  storage_pool: string
  template_count: number
  container_count: number
  // K8s-specific
  server_version?: string
  namespace?: string
  overlay_mode?: string
  [key: string]: unknown
}

export interface Container {
  id: string
  name: string
  status: string
  session_id: string
  pinned: boolean
  exposed_ports: ExposedPort[]
  [key: string]: unknown
}

export interface ExposedPort {
  port: number
  url: string
  [key: string]: unknown
}

export interface Template {
  name: string
  description: string
  status: string
  [key: string]: unknown
}

// --- Setup Wizard ---

export async function fetchSandboxStatus(): Promise<SandboxStatus> {
  const res = await teamFetch('/api/sandbox/status')
  if (!res.ok) throw new Error(`Failed to fetch sandbox status: ${res.statusText}`)
  return res.json()
}

export async function fetchOptionalTools(): Promise<OptionalTool[]> {
  const res = await teamFetch('/api/sandbox/optional-tools')
  if (!res.ok) throw new Error(`Failed to fetch optional tools: ${res.statusText}`)
  return res.json()
}

export function initSandbox({ installTools, onProgress, onDone, onError }: InitSandboxParams): { abort: () => void } {
  const controller = new AbortController()

  fetch('/api/sandbox/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ installTools }),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (!res.ok) {
        const text = await res.text()
        onError(text || res.statusText)
        return
      }

      const reader = res.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop()!

        let currentEvent = ''
        for (const line of lines) {
          if (line.startsWith('event: ')) {
            currentEvent = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const dataStr = line.slice(6)
            try {
              const data = JSON.parse(dataStr)
              if (currentEvent === 'progress') {
                onProgress(data.message || '')
              } else if (currentEvent === 'done') {
                onDone()
              } else if (currentEvent === 'error') {
                onError(data.error || 'Unknown error')
              }
            } catch {
              // ignore parse errors for incomplete data
            }
            currentEvent = ''
          }
        }
      }
    })
    .catch((err: Error) => {
      if (err.name !== 'AbortError') {
        onError(err.message || 'Connection failed')
      }
    })

  return { abort: () => controller.abort() }
}

// --- Settings: Container & Template Management ---

export async function fetchSandboxDetails(): Promise<SandboxDetails> {
  const res = await teamFetch('/api/sandbox/details')
  if (!res.ok) throw new Error(`Failed to fetch sandbox details: ${res.statusText}`)
  return res.json()
}

export async function fetchContainers(): Promise<{ containers: Container[], orphans?: string[] }> {
  const res = await teamFetch('/api/sandbox/containers')
  if (!res.ok) throw new Error(`Failed to fetch containers: ${res.statusText}`)
  return res.json()
}

export async function deleteContainer(id: string): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/containers/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function pruneOrphans(): Promise<Record<string, unknown>> {
  const res = await teamFetch('/api/sandbox/prune', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function fetchTemplates(): Promise<{ templates: Template[] }> {
  const res = await teamFetch('/api/sandbox/templates')
  if (!res.ok) throw new Error(`Failed to fetch templates: ${res.statusText}`)
  return res.json()
}

export async function fetchTemplateInfo(name: string): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/templates/${encodeURIComponent(name)}`)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function createTemplate(name: string, description: string): Promise<Record<string, unknown>> {
  const res = await teamFetch('/api/sandbox/templates', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, description }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function deleteTemplate(name: string): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/templates/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function snapshotTemplate(name: string): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/templates/${encodeURIComponent(name)}/snapshot`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function promoteTemplate(name: string): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/templates/${encodeURIComponent(name)}/promote`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function refreshTemplates(): Promise<Record<string, unknown>> {
  const res = await teamFetch('/api/sandbox/refresh', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

// --- Port Exposure ---

export async function exposePort(containerId: string, port: number): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ port, base_domain: window.location.hostname }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function unexposePort(containerId: string, port: number): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose/${port}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function pinContainer(containerId: string, pinned: boolean): Promise<Record<string, unknown>> {
  const res = await teamFetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/pin`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinned }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

// --- Team Template API ---

export interface TeamTemplateStatus {
  exists: boolean
  running: boolean
  templateName: string
  saved: boolean
}

export async function fetchTeamTemplateStatus(teamSlug?: string): Promise<TeamTemplateStatus> {
  const res = await teamFetch('/api/team/template/status', undefined, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function createTeamTemplate(teamSlug?: string): Promise<{ status: string; templateName: string; created: boolean }> {
  const res = await teamFetch('/api/team/template/create', { method: 'POST' }, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function saveTeamTemplate(teamSlug?: string): Promise<{ status: string; templateName: string }> {
  const res = await teamFetch('/api/team/template/save', { method: 'POST' }, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function restoreTeamTemplate(teamSlug?: string): Promise<{ status: string; templateName: string; restored: boolean }> {
  const res = await teamFetch('/api/team/template/restore', { method: 'POST' }, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function deleteTeamTemplate(teamSlug?: string): Promise<{ status: string; deleted: boolean }> {
  const res = await teamFetch('/api/team/template', { method: 'DELETE' }, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function startTeamTemplate(teamSlug?: string): Promise<{ status: string; started?: boolean; alreadyRunning?: boolean }> {
  const res = await teamFetch('/api/team/template/start', { method: 'POST' }, teamSlug)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/**
 * Returns the WebSocket URL for connecting to the team template terminal.
 * The URL uses the same host as the current page.
 * The team slug is passed as a query parameter since WebSocket doesn't support custom headers.
 */
export function getTeamTerminalWsUrl(teamSlug: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/api/sandbox/terminal?team=${encodeURIComponent(teamSlug)}`
}
