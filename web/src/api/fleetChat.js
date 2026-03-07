/**
 * API client for Fleet Sessions (fleet v2: autonomous agent team)
 */

const API_BASE = '/api/studio/fleet'
const FLEET_API = '/api/fleets'
const FLEET_PLANS_API = '/api/fleet-plans'

/**
 * Fetch available fleet definitions
 * @returns {Promise<{fleets: Array<{key: string, name: string, description: string, agent_count: number, agent_names: string[]}>}>}
 */
export async function fetchFleets() {
  const response = await fetch(FLEET_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleets: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch available fleet plans
 * @returns {Promise<{plans: Array<{key: string, name: string, description: string, created_from: string, channel_type: string, agent_count: number, agent_names: string[]}>}>}
 */
export async function fetchFleetPlans() {
  const response = await fetch(FLEET_PLANS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet plans: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch active fleet sessions
 * @returns {Promise<{sessions: Array<{id: string, fleet_key: string, fleet_name: string, state: string, active_agent: string}>}>}
 */
export async function fetchFleetSessions() {
  const response = await fetch(`${API_BASE}/sessions`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet sessions: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch a fleet session's current state and thread history
 * @param {string} id - Fleet session ID
 * @returns {Promise<{session_id: string, fleet_key: string, fleet_name: string, state: string, active_agent: string, messages: Array, agents: Array}>}
 */
export async function fetchFleetSession(id) {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch fleet session: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Start a new fleet session. Returns session info as JSON.
 * The caller should then connect to the SSE stream via connectFleetStream().
 * @param {object} params
 * @param {string} [params.fleetKey] - Fleet definition key (e.g., 'software-dev')
 * @param {string} [params.planKey] - Fleet plan key (alternative to fleetKey)
 * @param {string} [params.message] - Optional initial message to the team
 * @returns {Promise<{session_id: string, fleet_key: string, fleet_name: string, agents: Array}>}
 */
export async function startFleetSession({ fleetKey, planKey, message }) {
  const body = { message: message || '' }
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

/**
 * Connect to a fleet session's SSE stream for real-time events.
 * Works for both newly created sessions and reconnections after page reload.
 * Returns an AbortController so the caller can cancel.
 * @param {object} params
 * @param {string} params.sessionId - Fleet session ID
 * @param {function} params.onEvent - Callback for each SSE event: (eventType, data) => void
 * @param {function} params.onError - Callback for errors: (error) => void
 * @param {function} params.onDone - Callback when stream completes
 * @returns {AbortController}
 */
export function connectFleetStream({ sessionId, onEvent, onError, onDone }) {
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

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split('\n\n')
        buffer = blocks.pop()

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
      if (err.name === 'AbortError') {
        if (onDone) onDone()
      } else {
        if (onError) onError(err)
      }
    }
  }

  run()
  return controller
}

/**
 * Send a human message to an active fleet session
 * @param {string} sessionId - Fleet session ID
 * @param {string} message - Human message text
 */
export async function sendFleetMessage(sessionId, message) {
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

/**
 * Stop an active fleet session
 * @param {string} sessionId - Fleet session ID
 */
export async function stopFleetSession(sessionId) {
  try {
    await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stop`, {
      method: 'POST',
    })
  } catch (err) {
    console.warn('Failed to stop fleet session:', err)
  }
}

/**
 * Activate a fleet plan (start polling its configured channel)
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<{status: string, key: string}>}
 */
export async function activateFleetPlan(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/activate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Deactivate a fleet plan (stop polling)
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<{status: string, key: string}>}
 */
export async function deactivateFleetPlan(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/deactivate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Get fleet plan activation status
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<{activated: boolean, scheduler_job_id: string, activated_at: string, last_poll_at: string, last_poll_status: string, sessions_started: number}>}
 */
export async function getFleetPlanStatus(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/status`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Fetch fleet session execution trace (merged parent + child events)
 * @param {string} sessionId - Fleet session ID
 * @param {object} [opts] - Optional filters
 * @param {boolean} [opts.toolsOnly] - Only return tool events
 * @param {number} [opts.lastN] - Limit to last N events
 * @returns {Promise<{session_id: string, app: string, user: string, events: Array, summary: {total_events: number, tool_calls: number, errors: number}}>}
 */
export async function fetchFleetTrace(sessionId, opts = {}) {
  const params = new URLSearchParams()
  if (opts.toolsOnly) params.set('tools_only', 'true')
  if (opts.lastN) params.set('last_n', String(opts.lastN))
  const qs = params.toString()
  const url = `${API_BASE}/sessions/${encodeURIComponent(sessionId)}/trace${qs ? '?' + qs : ''}`
  const response = await fetch(url)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Duplicate a fleet plan
 * @param {string} planKey - Fleet plan key to duplicate
 * @returns {Promise<{status: string, key: string}>}
 */
export async function duplicateFleetPlan(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/duplicate`, {
    method: 'POST',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Fetch raw YAML content of a fleet plan
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<string>}
 */
export async function fetchFleetPlanYaml(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}/yaml`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.text()
}

/**
 * Save raw YAML content for a fleet plan
 * @param {string} planKey - Fleet plan key
 * @param {string} yamlContent - Raw YAML content
 * @returns {Promise<{status: string, key: string}>}
 */
export async function saveFleetPlanYaml(planKey, yamlContent) {
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

/**
 * Delete a fleet plan
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<{status: string, key: string}>}
 */
export async function deleteFleetPlan(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

/**
 * Get a single fleet plan detail
 * @param {string} planKey - Fleet plan key
 * @returns {Promise<{key: string, plan: object}>}
 */
export async function fetchFleetPlan(planKey) {
  const response = await fetch(`${FLEET_PLANS_API}/${encodeURIComponent(planKey)}`)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}
