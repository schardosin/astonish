/**
 * API client for Astonish Studio
 */

import { teamFetch } from './teamContext'

const API_BASE = '/api'

// --- Types ---

export interface Agent {
  id: string
  name: string
  description: string
  source: string
  scope?: string // "personal" | "team" (platform mode only)
}

export interface AgentDetail {
  name: string
  source: string
  scope?: string // "personal" | "team" (platform mode only)
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
  const response = await teamFetch(`${API_BASE}/agents`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agents: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchAgent(name: string, scope?: string): Promise<AgentDetail> {
  const params = scope ? `?scope=${encodeURIComponent(scope)}` : ''
  const response = await teamFetch(`${API_BASE}/agents/${encodeURIComponent(name)}${params}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch agent: ${response.statusText}`)
  }
  return response.json()
}

export async function saveAgent(name: string, yaml: string, scope?: string): Promise<{ status: string; path: string }> {
  const params = scope ? `?scope=${encodeURIComponent(scope)}` : ''
  const response = await teamFetch(`${API_BASE}/agents/${encodeURIComponent(name)}${params}`, {
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

export async function deleteAgent(name: string, scope?: string): Promise<{ status: string; deleted: string }> {
  const params = scope ? `?scope=${encodeURIComponent(scope)}` : ''
  const response = await teamFetch(`${API_BASE}/agents/${encodeURIComponent(name)}${params}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete agent: ${response.statusText}`)
  }
  return response.json()
}

export async function publishFlowToTeam(name: string): Promise<{ published: boolean; name: string }> {
  const response = await teamFetch(`${API_BASE}/agents/${encodeURIComponent(name)}/publish`, {
    method: 'POST',
  })
  if (!response.ok) {
    throw new Error(`Failed to publish flow: ${response.statusText}`)
  }
  return response.json()
}

export async function forkFlowToPersonal(name: string): Promise<{ forked: boolean; name: string }> {
  const response = await teamFetch(`${API_BASE}/agents/${encodeURIComponent(name)}/fork`, {
    method: 'POST',
  })
  if (!response.ok) {
    throw new Error(`Failed to fork flow: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchTools(): Promise<{ tools: Tool[] }> {
  const response = await teamFetch(`${API_BASE}/tools`)
  if (!response.ok) {
    throw new Error(`Failed to fetch tools: ${response.statusText}`)
  }
  return response.json()
}

export async function checkMcpDependencies(dependencies: McpDependency[]): Promise<McpDependencyCheckResult> {
  const response = await teamFetch(`${API_BASE}/mcp-dependencies/check`, {
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
  const response = await teamFetch(`${API_BASE}/mcp-store/${encodedId}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch server details (${response.status})`)
  }
  return response.json()
}

export async function installMcpServer(storeId: string, env: Record<string, string> = {}): Promise<McpInstallResult> {
  const encodedId = encodeURIComponent(storeId).replace(/%2F/g, '/')
  const response = await teamFetch(`${API_BASE}/mcp-store/${encodedId}/install?scope=team`, {
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
  const response = await teamFetch(`${API_BASE}/mcp/install-inline?scope=team`, {
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
  const response = await teamFetch(`${API_BASE}/standard-servers`)
  if (!response.ok) {
    throw new Error(`Failed to fetch standard servers: ${response.statusText}`)
  }
  return response.json()
}

export async function installStandardServer(id: string, env: Record<string, string> = {}): Promise<McpInstallResult> {
  const response = await teamFetch(`${API_BASE}/standard-servers/${encodeURIComponent(id)}/install`, {
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
