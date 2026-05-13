import { useState, useEffect, useCallback } from 'react'
import { fetchFullConfig, fetchSettings, fetchMCPConfig, fetchWebCapableTools } from '../components/settings/settingsApi'
import type { FullConfig, SettingsData, MCPConfigData, WebCapableTools, StandardServer } from '../components/settings/settingsApi'
import { FULL_CONFIG_SECTIONS } from '../components/settings/settingsMenuItems'

export interface UseSettingsDataReturn {
  settings: SettingsData | null
  mcpConfig: MCPConfigData | null
  webCapableTools: WebCapableTools
  standardServers: StandardServer[]
  fullConfig: FullConfig | null
  loading: boolean
  fullConfigLoading: boolean
  error: string | null
  loadData: () => Promise<void>
  invalidateFullConfig: () => void
  setError: (err: string | null) => void
}

/**
 * Shared hook that manages the loading of settings data (settings API, MCP config, full config).
 * Used by both the User Settings page and the Workspace Administration panel.
 */
export function useSettingsData(activeSection: string): UseSettingsDataReturn {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [mcpConfig, setMcpConfig] = useState<MCPConfigData | null>(null)
  const [webCapableTools, setWebCapableTools] = useState<WebCapableTools>({ webSearch: [], webExtract: [] })
  const [standardServers, setStandardServers] = useState<StandardServer[]>([])
  const [fullConfig, setFullConfig] = useState<FullConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [fullConfigLoading, setFullConfigLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [settingsData, mcpData, webTools, stdServers] = await Promise.all([
        fetchSettings(),
        fetchMCPConfig(),
        fetchWebCapableTools().catch(() => ({ webSearch: [], webExtract: [] } as WebCapableTools)),
        fetch('/api/standard-servers').then(r => r.ok ? r.json() : { servers: [] }).catch(() => ({ servers: [] }))
      ])
      setSettings(settingsData)
      setMcpConfig(mcpData)
      setWebCapableTools(webTools)
      setStandardServers(stdServers.servers || [])
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  const invalidateFullConfig = useCallback(() => {
    setFullConfig(null)
  }, [])

  // Initial data load
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const [settingsData, mcpData, webTools, stdServers] = await Promise.all([
          fetchSettings(),
          fetchMCPConfig(),
          fetchWebCapableTools().catch(() => ({ webSearch: [], webExtract: [] } as WebCapableTools)),
          fetch('/api/standard-servers').then(r => r.ok ? r.json() : { servers: [] }).catch(() => ({ servers: [] }))
        ])
        if (cancelled) return
        setSettings(settingsData)
        setMcpConfig(mcpData)
        setWebCapableTools(webTools)
        setStandardServers(stdServers.servers || [])
      } catch (err: any) {
        if (!cancelled) setError(err.message)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [])

  // Load full config when a FULL_CONFIG_SECTIONS section is active and not yet loaded
  useEffect(() => {
    if (!FULL_CONFIG_SECTIONS.includes(activeSection) || fullConfig) return
    let cancelled = false
    const load = async () => {
      setFullConfigLoading(true)
      try {
        const data = await fetchFullConfig()
        if (!cancelled) setFullConfig(data)
      } catch (err: any) {
        if (!cancelled) setError(err.message)
      } finally {
        if (!cancelled) setFullConfigLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [activeSection, fullConfig])

  return {
    settings,
    mcpConfig,
    webCapableTools,
    standardServers,
    fullConfig,
    loading,
    fullConfigLoading,
    error,
    loadData,
    invalidateFullConfig,
    setError,
  }
}
