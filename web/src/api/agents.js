/**
 * API client for Astonish Studio
 */

const API_BASE = '/api'

/**
 * Fetch all available agents
 * @returns {Promise<{agents: Array<{id: string, name: string, description: string, source: string}>}>}
 */
export async function fetchAgents() {
  const response = await fetch(`${API_BASE}/agents`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agents: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch a single agent's details and YAML
 * @param {string} name - Agent name
 * @returns {Promise<{name: string, source: string, yaml: string, config: object}>}
 */
export async function fetchAgent(name) {
  const response = await fetch(`${API_BASE}/agents/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agent: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Save an agent's YAML content
 * @param {string} name - Agent name
 * @param {string} yaml - YAML content
 * @returns {Promise<{status: string, path: string}>}
 */
export async function saveAgent(name, yaml) {
  const response = await fetch(`${API_BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ yaml }),
  })
  if (!response.ok) {
    throw new Error(`Failed to save agent: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Delete an agent
 * @param {string} name - Agent name
 * @returns {Promise<{status: string, deleted: string}>}
 */
export async function deleteAgent(name) {
  const response = await fetch(`${API_BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete agent: ${response.statusText}`)
  }
  return response.json()
}

