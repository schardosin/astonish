/**
 * API client for Fleet Sessions (fleet v2: autonomous agent team)
 */

const API_BASE = '/api/studio/fleet'
const FLEET_API = '/api/fleets'
const FLEET_PLANS_API = '/api/fleet-plans'

// --- Types ---

export interface FleetDefinition {
  key: string
  name: string
  description: string
  agent_count: number
  agent_names: string[]
}

export interface FleetPlanSummary {
  key: string
  name: string
  description: string
  created_from: string
  channel_type: string
  agent_count: number
  agent_names: string[]
}

export interface FleetSession {
  id: string
  fleet_key: string
  fleet_name: string
  state: string
  active_agent: string
}

export interface FleetSessionDetail {
  session_id: string
  fleet_key: string
  fleet_name: string
  state: string
  active_agent: string
  messages: FleetMessage[]
  agents: FleetAgent[]
}

export interface FleetMessage {
  id: string
  sender: string
  text: string
  memory_keys: string[]
  artifacts: Record<string, unknown>
  mentions: string[]
  timestamp: string
  metadata: Record<string, unknown>
}

export interface FleetAgent {
  key: string
  name: string
  role: string
  [key: string]: unknown
}

export interface FleetPlanStatus {
  activated: boolean
  scheduler_job_id: string
  activated_at: string
  last_poll_at: string
  last_poll_status: string
  sessions_started: number
}

export interface FleetTrace {
  session_id: string
  app: string
  user: string
  events: unknown[]
  summary: {
    total_events: number
    tool_calls: number
    errors: number
  }
}

export interface FleetThread {
  thread_key: string
  participants: string[]
  message_count: number
  first_timestamp: string
  last_timestamp: string
}

export type SSEEventCallback = (eventType: string, data: Record<string, unknown>) => void
export type ErrorCallback = (error: Error) => void
export type DoneCallback = () => void

export interface ConnectFleetStreamParams {
  sessionId: string
  onEvent: SSEEventCallback
  onError?: ErrorCallback
  onDone?: DoneCallback
}

export interface StartFleetSessionParams {
  fleetKey?: string
  planKey?: string
  message?: string
}

export interface FetchFleetTraceOpts {
  toolsOnly?: boolean
  lastN?: number
  agent?: string
}

export interface FetchFleetMessagesOpts {
  agent?: string
}

// --- API Functions ---

export async function fetchFleets(): Promise<{ fleets: FleetDefinition[] }> {
  const response = await fetch(FLEET_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleets: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleet(key: string): Promise<{ key: string; fleet: Record<string, unknown> }> {
  const response = await fetch(`${FLEET_API}/${encodeURIComponent(key)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetPlans(): Promise<{ plans: FleetPlanSummary[] }> {
  const response = await fetch(FLEET_PLANS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet plans: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetSessions(): Promise<{ sessions: FleetSession[] }> {
  const response = await fetch(`${API_BASE}/sessions`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet sessions: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetSession(id: string): Promise<FleetSessionDetail> {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet session: ${response.statusText}`)
  }
  return response.json()
}

export async function startFleetSession({ fleetKey, planKey, message }: StartFleetSessionParams): Promise<{ session_id: string; fleet_key: string; fleet_name: string; agents: FleetAgent[] }> {
  const body: Record<string, unknown> = { message: message || '' }
  if (planKey) {
    body.plan_key = planKey
  } else {
    body.fleet_key = fleetKey
  }

  const response = await fetch(`${API_BASE}/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }

  return response.json()
}

export function connectFleetStream({ sessionId, onEvent, onError, onDone }: ConnectFleetStreamParams): AbortController {
  const controller = new AbortController()

  const run = async () => {
    try {
      const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stream`, {
        signal: controller.signal,
      })

      if (!response.ok) {
        const text = await response.text()
        throw new Error(text || `HTTP ${response.status}`)
      }

      const reader = response.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split('\n\n')
        buffer = blocks.pop()!

        for (const block of blocks) {
          if (!block.trim()) continue
          const lines = block.split('\n')
          let eventType = 'message'
          let dataStr = ''

          for (const line of lines) {
            if (line.startsWith('event: ')) {
              eventType = line.slice(7).trim()
            } else if (line.startsWith('data: ')) {
              dataStr = line.slice(6)
            }
          }

          if (dataStr) {
            try {
              const data = JSON.parse(dataStr)
              onEvent(eventType, data)
            } catch (e) {
              console.error('Failed to parse fleet SSE data:', e, dataStr)
            }
          }
        }
      }

      if (onDone) onDone()
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        if (onDone) onDone()
      } else {
        if (onError) onError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  run()
  return controller
}

export async function sendFleetMessage(sessionId: string, message: string): Promise<Record<string, unknown>> {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/message`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function stopFleetSession(sessionId: string): Promise<void> {
  try {
    await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stop`, {
      method: 'POST',
    })
  } catch (err) {
    console.warn('Failed to stop fleet session:', err)
  }
}

export async function activateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/activate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function deactivateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/deactivate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function getFleetPlanStatus(planKey: string): Promise<FleetPlanStatus> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/status`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetTrace(sessionId: string, opts: FetchFleetTraceOpts = {}): Promise<FleetTrace> {
  const params = new URLSearchParams()
  if (opts.toolsOnly) params.set('tools_only', 'true')
  if (opts.lastN) params.set('last_n', String(opts.lastN))
  if (opts.agent) params.set('agent', opts.agent)
  const qs = params.toString()
  const url = `${API_BASE}/sessions/${encodeURIComponent(sessionId)}/trace${qs ? '?' + qs : ''}`
  const response = await fetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetThreads(sessionId: string): Promise<{ threads: FleetThread[] }> {
  const url = `${API_BASE}/sessions/${encodeURIComponent(sessionId)}/threads`
  const response = await fetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetMessages(sessionId: string, opts: FetchFleetMessagesOpts = {}): Promise<{ messages: FleetMessage[] }> {
  const params = new URLSearchParams()
  if (opts.agent) params.set('agent', opts.agent)
  const qs = params.toString()
  const url = `${API_BASE}/sessions/${encodeURIComponent(sessionId)}/messages${qs ? '?' + qs : ''}`
  const response = await fetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function duplicateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/duplicate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetPlanYaml(planKey: string): Promise<string> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/yaml`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.text()
}

export async function saveFleetPlanYaml(planKey: string, yamlContent: string): Promise<{ status: string; key: string }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/yaml`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/yaml' },
    body: yamlContent,
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function deleteFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetPlan(planKey: string): Promise<{ key: string; plan: Record<string, unknown> }> {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function retryFleetIssue(planKey: string, issueNumber: number): Promise<{ status: string; session_id: string; issue: number }> {
  const response = await fetch(
    `${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/retry/${issueNumber}`,
    { method: 'POST' }
  )
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}
