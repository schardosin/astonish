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

/**
 * Fetch all available tools
 * @returns {Promise<{tools: Array<{name: string, description: string, source: string}>}>}
 */
export async function fetchTools() {
  const response = await fetch(`${API_BASE}/tools`)
  if (!response.ok) {
    throw new Error(`Failed to fetch tools: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Check MCP dependencies installation status
 * @param {Array<{server: string, tools: string[], source: string, store_id?: string, config?: object}>} dependencies
 * @returns {Promise<{dependencies: Array, all_installed: boolean, missing: number}>}
 */
export async function checkMcpDependencies(dependencies) {
  const response = await fetch(`${API_BASE}/mcp-dependencies/check`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ dependencies }),
  })
  if (!response.ok) {
    throw new Error(`Failed to check dependencies: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch details for a specific MCP server from the store
 * @param {string} storeId - The MCP store ID
 * @returns {Promise<object>}
 */
export async function getMcpStoreServer(storeId) {
  const encodedId = encodeURIComponent(storeId).replace(/%2F/g, '/')
  const response = await fetch(`${API_BASE}/mcp-store/${encodedId}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch server details (${response.status})`)
  }
  return response.json()
}

/**
 * Install an MCP server from the store
 * @param {string} storeId - The MCP store ID (e.g., "github.com/user/repo")
 * @param {object} env - Optional environment variable overrides
 * @returns {Promise<{status: string, serverName: string, toolsLoaded: number}>}
 */
export async function installMcpServer(storeId, env = {}) {
  const encodedId = encodeURIComponent(storeId).replace(/%2F/g, '/')
  const response = await fetch(`${API_BASE}/mcp-store/${encodedId}/install`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ env }),
  })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `Failed to install MCP server (${response.status})`)
  }
  return response.json()
}

/**
 * Install an inline MCP server (from flow's embedded config)
 * @param {string} serverName - The server name
 * @param {object} config - The server configuration (command, args, env, transport)
 * @returns {Promise<{status: string, serverName: string}>}
 */
export async function installInlineMcpServer(serverName, config) {
  const response = await fetch(`${API_BASE}/mcp/install-inline`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ serverName, config }),
  })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `Failed to install MCP server (${response.status})`)
  }
  return response.json()
}
