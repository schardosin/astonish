/**
 * API client for Platform Base Sandbox Configuration.
 * Used by the SandboxBaseTab component (superadmins only).
 */

const BASE = '/api/platform/admin/sandbox/base'

async function adminFetch(input: string, init?: Parameters<typeof fetch>[1]): Promise<Response> {
  const headers = new Headers(init?.headers)
  if (!headers.has('X-Requested-With')) {
    headers.set('X-Requested-With', 'XMLHttpRequest')
  }
  return fetch(input, { credentials: 'include', ...init, headers })
}

// --- Types ---

export interface BaseConfig {
  core: boolean
  optional_tools: string[]
  browser: {
    engine: 'none' | 'default' | 'cloakbrowser'
    fingerprint_platform?: string
    fingerprint_seed?: string
  }
  extra_steps?: string[]
  architecture?: string
}

export interface BaseConfigSummary {
  layer_id: string
  size_bytes: number
  config: BaseConfig | null
  configured_by: string
  configured_at: string | null
  updated_at: string
}

export interface BaseConfigStatus {
  in_progress: boolean
}

export interface OptionalTool {
  id: string
  name: string
  description: string
  url: string
  recommended: boolean
}

export interface ConfigureBuildResult {
  layer_id: string
  size_bytes: number
}

export interface UnsupportedBackendInfo {
  unsupported_backend: true
  backend: string
  message: string
}

// --- API functions ---

export async function getBaseConfig(): Promise<BaseConfigSummary | UnsupportedBackendInfo> {
  const res = await adminFetch(BASE)
  if (!res.ok) {
    const body = await res.json().catch(() => ({})) as Record<string, unknown>
    throw new Error((body.error as string) || `Failed to fetch base config: ${res.statusText}`)
  }
  const data = await res.json()
  if (data.unsupported_backend) {
    return data as UnsupportedBackendInfo
  }
  return data as BaseConfigSummary
}

export async function getBaseStatus(): Promise<BaseConfigStatus> {
  const res = await adminFetch(`${BASE}/status`)
  if (!res.ok) {
    throw new Error(`Failed to fetch build status: ${res.statusText}`)
  }
  return res.json()
}

export async function listOptionalTools(): Promise<OptionalTool[]> {
  const res = await adminFetch(`${BASE}/tools`)
  if (!res.ok) {
    throw new Error(`Failed to fetch optional tools: ${res.statusText}`)
  }
  return res.json()
}

export interface ConfigureBaseCallbacks {
  config: BaseConfig
  onProgress: (msg: string) => void
  onDone: (result: ConfigureBuildResult) => void
  onError: (err: string) => void
}

export function configureBase({ config, onProgress, onDone, onError }: ConfigureBaseCallbacks): { abort: () => void } {
  const controller = new AbortController()

  adminFetch(`${BASE}/configure`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (!res.ok) {
        const text = await res.text()
        try {
          const body = JSON.parse(text)
          onError(body.error || text || res.statusText)
        } catch {
          onError(text || res.statusText)
        }
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
                onDone({ layer_id: data.layer_id, size_bytes: data.size_bytes })
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
