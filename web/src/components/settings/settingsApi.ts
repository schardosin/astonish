// Shared API and UI utilities for settings components
import type { CSSProperties } from 'react'

// --- Types ---

export interface FullConfig {
  [sectionKey: string]: Record<string, unknown>
}

export interface ProviderInfo {
  name: string
  type: string
  display_name: string
  configured: boolean
  fields: Record<string, string>
}

export interface SettingsData {
  general: {
    default_provider: string
    default_model: string
    web_search_tool: string
    web_extract_tool: string
    timezone: string
  }
  providers: ProviderInfo[]
}

export interface MCPServerConfig {
  command?: string
  args?: string[]
  env?: Record<string, string>
  transport?: string
  url?: string
  enabled?: boolean
}

export interface MCPConfigData {
  mcpServers: Record<string, MCPServerConfig>
}

export interface MCPServerStatusEntry {
  name: string
  status: string
  error?: string | null
  tool_count?: number
}

export interface WebTool {
  source: string
  name: string
}

export interface WebCapableTools {
  webSearch: WebTool[]
  webExtract: WebTool[]
}

export interface StandardServerEnvVar {
  name: string
  required: boolean
}

export interface StandardServer {
  id: string
  displayName: string
  isDefault: boolean
  installed: boolean
  envVars: StandardServerEnvVar[]
  capabilities: {
    webSearch: boolean
    webExtract: boolean
  }
}

export interface TapEntry {
  name: string
  url: string
}

export interface UpdateInfo {
  version: string
  [key: string]: unknown
}

export interface ProviderFieldDef {
  key: string
  label: string
  type: string
}

// --- API Functions ---

export const fetchFullConfig = async (): Promise<FullConfig> => {
  const res = await fetch('/api/settings/full')
  if (!res.ok) throw new Error('Failed to fetch config')
  return res.json()
}

export const saveFullConfigSection = async (sectionKey: string, data: Record<string, unknown>): Promise<Record<string, unknown>> => {
  const res = await fetch('/api/settings/full', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ [sectionKey]: data })
  })
  if (!res.ok) throw new Error('Failed to save settings')
  return res.json()
}

export const fetchSettings = async (): Promise<SettingsData> => {
  const res = await fetch('/api/settings/config')
  if (!res.ok) throw new Error('Failed to fetch settings')
  return res.json()
}

export const saveSettings = async (data: Record<string, unknown>): Promise<unknown> => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  })
  if (!res.ok) throw new Error('Failed to save settings')
  return res.json()
}

export const replaceAllProviders = async (providers: Record<string, unknown>[]): Promise<unknown> => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ providers: { '__replace_all__': { '__array__': JSON.stringify(providers) } } })
  })
  if (!res.ok) throw new Error('Failed to replace providers')
  return res.json()
}

export const fetchMCPConfig = async (): Promise<MCPConfigData> => {
  const res = await fetch('/api/settings/mcp')
  if (!res.ok) throw new Error('Failed to fetch MCP config')
  return res.json()
}

export const saveMCPConfig = async (data: Record<string, unknown>): Promise<unknown> => {
  const res = await fetch('/api/settings/mcp', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  })
  if (!res.ok) throw new Error('Failed to save MCP config')
  return res.json()
}

export const fetchProviderModels = async (providerId: string): Promise<{ models: string[] }> => {
  const res = await fetch(`/api/providers/${providerId}/models`)
  if (!res.ok) throw new Error('Failed to fetch models')
  return res.json()
}

// Fetch tools that have 'websearch' or 'webextract' in their name
export const fetchWebCapableTools = async (): Promise<WebCapableTools> => {
  const res = await fetch('/api/tools/web-capable')
  if (!res.ok) throw new Error('Failed to fetch web-capable tools')
  return res.json()
}

// Taps API functions
export const fetchTaps = async (): Promise<{ taps: TapEntry[] }> => {
  const res = await fetch('/api/taps')
  if (!res.ok) throw new Error('Failed to fetch taps')
  return res.json()
}

export const addTap = async (url: string, alias: string = ''): Promise<unknown> => {
  const res = await fetch('/api/taps', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, alias })
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to add tap')
  }
  return res.json()
}

export const removeTap = async (name: string): Promise<unknown> => {
  const res = await fetch(`/api/taps/${encodeURIComponent(name)}`, {
    method: 'DELETE'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to remove tap')
  }
  return res.json()
}

// Fetch MCP server status
export const fetchMCPStatus = async (): Promise<{ servers: MCPServerStatusEntry[] }> => {
  const res = await fetch('/api/mcp/status')
  if (!res.ok) throw new Error('Failed to fetch MCP status')
  return res.json()
}

// Toggle MCP server enabled/disabled
export const toggleMCPServer = async (name: string, enabled: boolean): Promise<unknown> => {
  const res = await fetch(`/api/mcp/servers/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  })
  if (!res.ok) throw new Error('Failed to toggle server')
  return res.json()
}

export const refreshMCPServer = async (serverName: string): Promise<unknown> => {
  const res = await fetch(`/api/mcp/${encodeURIComponent(serverName)}/refresh`, {
    method: 'POST'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to refresh server')
  }
  return res.json()
}

// --- Common Styles ---

export const inputClass: string = 'w-full px-4 py-2.5 rounded-lg border text-sm'
export const inputStyle: CSSProperties = {
  background: 'var(--bg-secondary)',
  borderColor: 'var(--border-color)',
  color: 'var(--text-primary)'
}

export const labelStyle: CSSProperties = { color: 'var(--text-secondary)' }
export const hintStyle: CSSProperties = { color: 'var(--text-muted)' }
export const sectionBorderStyle: CSSProperties = { borderColor: 'var(--border-color)' }

export const saveButtonStyle: CSSProperties = {
  background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)'
}
