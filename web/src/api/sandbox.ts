// Sandbox API client functions for the Studio UI.

// --- Types ---

export interface SandboxStatus {
  platform: string
  available: boolean
  reason?: string
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
  incus_version: string
  storage_pool: string
  template_count: number
  container_count: number
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
  const res = await fetch('/api/sandbox/status')
  if (!res.ok) throw new Error(`Failed to fetch sandbox status: ${res.statusText}`)
  return res.json()
}

export async function fetchOptionalTools(): Promise<OptionalTool[]> {
  const res = await fetch('/api/sandbox/optional-tools')
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
  const res = await fetch('/api/sandbox/details')
  if (!res.ok) throw new Error(`Failed to fetch sandbox details: ${res.statusText}`)
  return res.json()
}

export async function fetchContainers(): Promise<Container[]> {
  const res = await fetch('/api/sandbox/containers')
  if (!res.ok) throw new Error(`Failed to fetch containers: ${res.statusText}`)
  return res.json()
}

export async function deleteContainer(id: string): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function pruneOrphans(): Promise<Record<string, unknown>> {
  const res = await fetch('/api/sandbox/prune', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function fetchTemplates(): Promise<Template[]> {
  const res = await fetch('/api/sandbox/templates')
  if (!res.ok) throw new Error(`Failed to fetch templates: ${res.statusText}`)
  return res.json()
}

export async function fetchTemplateInfo(name: string): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}`)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function createTemplate(name: string, description: string): Promise<Record<string, unknown>> {
  const res = await fetch('/api/sandbox/templates', {
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
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function snapshotTemplate(name: string): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}/snapshot`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function promoteTemplate(name: string): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}/promote`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function refreshTemplates(): Promise<Record<string, unknown>> {
  const res = await fetch('/api/sandbox/refresh', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

// --- Port Exposure ---

export async function exposePort(containerId: string, port: number): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose`, {
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
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose/${port}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

export async function pinContainer(containerId: string, pinned: boolean): Promise<Record<string, unknown>> {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/pin`, {
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
