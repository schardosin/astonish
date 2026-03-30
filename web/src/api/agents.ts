/**
 * API client for Astonish Studio
 */

const API_BASE = '/api'

// --- Types ---

export interface Agent {
  id: string
  name: string
  description: string
  source: string
}

export interface AgentDetail {
  name: string
  source: string
  yaml: string
  config: Record<string, unknown>
}

export interface Tool {
  name: string
  description: string
  source: string
}

export interface McpDependency {
  server: string
  tools: string[]
  source: string
  installed?: boolean
  store_id?: string
  config?: Record<string, unknown>
}

export interface McpDependencyCheckResult {
  dependencies: McpDependency[]
  all_installed: boolean
  missing: number
}

export interface McpInstallResult {
  status: string
  serverName: string
  toolsLoaded: number
}

export interface StandardServer {
  id: string
  name: string
  description: string
  installed: boolean
  env_keys?: string[]
}

// --- API Functions ---

export async function fetchAgents(): Promise<{ agents: Agent[] }> {
  const response = await fetch(`${API_BASE}/agents`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agents: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchAgent(name: string): Promise<AgentDetail> {
  const response = await fetch(`${API_BASE}/agents/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agent: ${response.statusText}`)
  }
  return response.json()
}

export async function saveAgent(name: string, yaml: string): Promise<{ status: string; path: string }> {
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

export async function deleteAgent(name: string): Promise<{ status: string; deleted: string }> {
  const response = await fetch(`${API_BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete agent: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchTools(): Promise<{ tools: Tool[] }> {
  const response = await fetch(`${API_BASE}/tools`)
  if (!response.ok) {
    throw new Error(`Failed to fetch tools: ${response.statusText}`)
  }
  return response.json()
}

export async function checkMcpDependencies(dependencies: McpDependency[]): Promise<McpDependencyCheckResult> {
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

export async function getMcpStoreServer(storeId: string): Promise<Record<string, unknown>> {
  const encodedId = encodeURIComponent(storeId).replace(/%2F/g, '/')
  const response = await fetch(`${API_BASE}/mcp-store/${encodedId}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch server details (${response.status})`)
  }
  return response.json()
}

export async function installMcpServer(storeId: string, env: Record<string, string> = {}): Promise<McpInstallResult> {
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

export async function installInlineMcpServer(
  serverName: string,
  config: Record<string, unknown>
): Promise<{ status: string; serverName: string }> {
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

export async function fetchStandardServers(): Promise<{ servers: StandardServer[] }> {
  const response = await fetch(`${API_BASE}/standard-servers`)
  if (!response.ok) {
    throw new Error(`Failed to fetch standard servers: ${response.statusText}`)
  }
  return response.json()
}

export async function installStandardServer(id: string, env: Record<string, string> = {}): Promise<McpInstallResult> {
  const response = await fetch(`${API_BASE}/standard-servers/${encodeURIComponent(id)}/install`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ env }),
  })
  if (!response.ok) {
    const errorText = await response.text()
    throw new Error(errorText || `Failed to install standard server (${response.status})`)
  }
  return response.json()
}
