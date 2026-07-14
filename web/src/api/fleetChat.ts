/**
 * API client for Fleet Sessions (fleet v2: autonomous agent team)
 */

import { teamFetch } from './teamContext'

const API_BASE = '/api/studio/fleet'
const FLEET_API = '/api/fleets'
const FLEET_PLANS_API = '/api/fleet-plans'
const FLEET_SETUP_PROFILES_API = '/api/fleet-setup-profiles'
const FLEET_SETUP_DRAFTS_API = '/api/fleet-setup/drafts'

// --- Types ---

export interface FleetDefinition {
  key: string
  name: string
  description: string
  agent_count: number
  agent_names: string[]
  source?: 'bundled' | 'custom'
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
  capabilities?: Record<string, boolean>
  execution?: Record<string, unknown>
  memory?: Record<string, unknown>
  task_policy?: Record<string, unknown>
  [key: string]: unknown
}

export interface FleetTask {
  ID?: string
  id?: string
  SessionID?: string
  session_id?: string
  Title?: string
  title?: string
  Description?: string
  description?: string
  RequiredCapabilities?: string[]
  required_capabilities?: string[]
  ClaimedBy?: string
  claimed_by?: string
  Status?: string
  status?: string
  Result?: Record<string, unknown>
  result?: Record<string, unknown>
  CreatedAt?: string
  created_at?: string
  UpdatedAt?: string
  updated_at?: string
}

export interface FleetMailboxMessage {
  ID?: string
  id?: string
  SessionID?: string
  session_id?: string
  Recipient?: string
  recipient?: string
  Sender?: string
  sender?: string
  Body?: string
  body?: string
  Mentions?: string[]
  mentions?: string[]
  Metadata?: Record<string, unknown>
  metadata?: Record<string, unknown>
  DeliveryStatus?: string
  delivery_status?: string
  CreatedAt?: string
  created_at?: string
}

export interface FleetPlanStatus {
  activated: boolean
  scheduler_job_id: string
  activated_at: string
  last_poll_at: string
  last_poll_status: string
  sessions_started: number
  last_start_error?: string
  last_start_error_at?: string
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
  const response = await teamFetch(FLEET_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleets: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleet(key: string): Promise<{ key: string; fleet: Record<string, unknown>; source?: 'bundled' | 'custom' }> {
  const response = await teamFetch(`${FLEET_API}/${encodeURIComponent(key)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet: ${response.statusText}`)
  }
  return response.json()
}

export async function saveFleet(key: string, fleet: Record<string, unknown>): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_API}/${encodeURIComponent(key)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(fleet),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Failed to save fleet: ${response.statusText}`)
  }
  return response.json()
}

export async function deleteFleet(key: string): Promise<void> {
  const response = await teamFetch(`${FLEET_API}/${encodeURIComponent(key)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Failed to delete fleet: ${response.statusText}`)
  }
}

export async function cloneFleet(
  fromKey: string,
  newKey: string,
  name?: string,
): Promise<{ status: string; key: string; source?: string }> {
  const response = await teamFetch(`${FLEET_API}/${encodeURIComponent(fromKey)}/clone`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ new_key: newKey, name }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Failed to clone fleet: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetPlans(): Promise<{ plans: FleetPlanSummary[] }> {
  const response = await teamFetch(FLEET_PLANS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet plans: ${response.statusText}`)
  }
  return response.json()
}

/** Persisted fleet session metadata (same shape as ChatSession + fleet fields). */
export interface FleetSessionMeta {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  messageCount: number
  fleetKey?: string
  fleetName?: string
  issueNumber?: number
  repo?: string
}

export async function fetchFleetSessionsHistory(): Promise<FleetSessionMeta[]> {
  const response = await teamFetch(`${API_BASE}/sessions/history`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet sessions history: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetSessions(): Promise<{ sessions: FleetSession[] }> {
  const response = await teamFetch(`${API_BASE}/sessions`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet sessions: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchFleetSession(id: string): Promise<FleetSessionDetail> {
  const response = await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`)
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

  const response = await teamFetch(`${API_BASE}/start`, {
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
      const response = await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stream`, {
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
  const response = await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/message`, {
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
    await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stop`, {
      method: 'POST',
    })
  } catch (err) {
    console.warn('Failed to stop fleet session:', err)
  }
}

export async function activateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/activate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function deactivateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/deactivate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function getFleetPlanStatus(planKey: string): Promise<FleetPlanStatus> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/status`)
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
  const response = await teamFetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetThreads(sessionId: string): Promise<{ threads: FleetThread[] }> {
  const url = `${API_BASE}/sessions/${encodeURIComponent(sessionId)}/threads`
  const response = await teamFetch(url)
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
  const response = await teamFetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function listFleetSessionTasks(sessionId: string): Promise<FleetTask[]> {
  const response = await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/tasks`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet tasks: ${response.statusText}`)
  }
  return response.json()
}

export async function listFleetSessionMailbox(sessionId: string, recipient: string): Promise<FleetMailboxMessage[]> {
  const response = await teamFetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/mailbox/${encodeURIComponent(recipient)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet mailbox: ${response.statusText}`)
  }
  return response.json()
}

export async function duplicateFleetPlan(planKey: string): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/duplicate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetPlanYaml(planKey: string): Promise<string> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/yaml`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.text()
}

export async function saveFleetPlanYaml(planKey: string, yamlContent: string): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/yaml`, {
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
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchFleetPlan(planKey: string): Promise<{ key: string; plan: Record<string, unknown> }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function saveFleetPlan(planKey: string, plan: Record<string, unknown>): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(plan),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function patchFleetPlanAgent(planKey: string, agentKey: string, patch: Partial<FleetAgent>): Promise<Record<string, unknown>> {
  const response = await teamFetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/agents/${encodeURIComponent(agentKey)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Failed to patch fleet plan agent: ${response.statusText}`)
  }
  return response.json()
}

export async function retryFleetIssue(planKey: string, issueNumber: number): Promise<{ status: string; session_id: string; issue: number }> {
  const response = await teamFetch(
    `${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/retry/${issueNumber}`,
    { method: 'POST' }
  )
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

// --- Setup profiles ---

export interface SetupFieldOption {
  value: string
  label: string
}

export interface SetupField {
  id: string
  label: string
  type: string
  required?: boolean
  when?: string
  options?: SetupFieldOption[]
  maps_to: string
  default?: unknown
  hint?: string
}

export interface SetupStep {
  id: string
  title: string
  type: string
  icon?: string
  summary?: string
  required?: boolean
  when?: string
  fields?: SetupField[]
  defaults?: Record<string, unknown>
  provisioner?: string
  prompt?: string
  content?: string
  guidance?: string
  pinned_tool_groups?: string[]
  tools?: string[]
}

export interface ChannelTypeDef {
  label: string
  description?: string
  requires_credentials?: string[]
  pinned_tool_groups?: string[]
}

export interface SetupProfile {
  key: string
  name: string
  description?: string
  domain?: string
  intro_prompt?: string
  pinned_tool_groups?: string[]
  channel_types?: Record<string, ChannelTypeDef>
  steps: SetupStep[]
}

export interface SetupProfileSummary {
  key: string
  name: string
  description?: string
  domain?: string
  step_count: number
  source?: string
}

export interface SetupDraft {
  id: string
  template_key: string
  setup_profile_key: string
  collected: Record<string, Record<string, unknown>>
  current_step?: string
}

export async function fetchSetupProfiles(): Promise<{ profiles: SetupProfileSummary[] }> {
  const response = await teamFetch(FLEET_SETUP_PROFILES_API)
  if (!response.ok) throw new Error(`Failed to fetch setup profiles: ${response.statusText}`)
  return response.json()
}

export async function fetchSetupToolCatalog(): Promise<{ tools: Array<{ name: string; group: string; label: string; description: string }> }> {
  const response = await teamFetch('/api/fleet-setup/tool-catalog')
  if (!response.ok) throw new Error(`Failed to fetch tool catalog: ${response.statusText}`)
  return response.json()
}

export async function fetchSetupProfileStep(key: string, stepId: string): Promise<{ step: SetupStep; tools: string[]; pinned_tool_groups: string[] }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}/steps/${encodeURIComponent(stepId)}`)
  if (!response.ok) throw new Error(`Failed to fetch setup step: ${response.statusText}`)
  return response.json()
}

export async function fetchSetupProfile(key: string): Promise<{ key: string; profile: SetupProfile; source?: string }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}`)
  if (!response.ok) throw new Error(`Failed to fetch setup profile: ${response.statusText}`)
  return response.json()
}

export async function saveSetupProfile(key: string, profile: SetupProfile): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(profile),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function deleteSetupProfile(key: string): Promise<{ status: string }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function cloneSetupProfile(
  sourceKey: string,
  newKey: string,
  name?: string,
): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(sourceKey)}/clone`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ new_key: newKey, name }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchSetupProfileYaml(key: string): Promise<string> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}/yaml`)
  if (!response.ok) throw new Error(`Failed to fetch setup profile YAML: ${response.statusText}`)
  return response.text()
}

export async function saveSetupProfileYaml(key: string, yamlContent: string): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_SETUP_PROFILES_API}/${encodeURIComponent(key)}/yaml`, {
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

export async function createSetupDraft(templateKey: string): Promise<{ draft: SetupDraft }> {
  const response = await teamFetch(FLEET_SETUP_DRAFTS_API, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ template_key: templateKey }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function patchSetupDraft(id: string, body: { collected?: Record<string, unknown>; current_step?: string }): Promise<{ draft: SetupDraft }> {
  const response = await teamFetch(`${FLEET_SETUP_DRAFTS_API}/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function validateSetupStep(id: string, stepId: string): Promise<{ status: string }> {
  const response = await teamFetch(`${FLEET_SETUP_DRAFTS_API}/${encodeURIComponent(id)}/validate-step`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ step_id: stepId }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function finalizeSetupDraft(id: string, validationPassed = false): Promise<{ status: string; key: string }> {
  const response = await teamFetch(`${FLEET_SETUP_DRAFTS_API}/${encodeURIComponent(id)}/finalize`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ validation_passed: validationPassed }),
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}
