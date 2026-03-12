import { useState, useEffect } from 'react'
import { Settings, Key, Server, ChevronRight, Save, Plus, Trash2, X, Check, AlertCircle, Code, LayoutGrid, Loader2, Package, Store, GitBranch, RefreshCw, Search, Play, Download, MessageSquare, Globe, Radio, Database, Brain, GitFork, Wand2, Clock, Shield, User, KeyRound, Terminal } from 'lucide-react'
import MCPStoreModal from './MCPStoreModal'
import FlowStorePanel from './FlowStorePanel'
import ProviderModelSelector from './ProviderModelSelector'
import MCPInspector from './MCPInspector'
import CodeMirror from '@uiw/react-codemirror'
import { json } from '@codemirror/lang-json'
import { search, searchKeymap, highlightSelectionMatches } from '@codemirror/search'
import { keymap, EditorView } from '@codemirror/view'
import { fetchFullConfig } from './settings/settingsApi'
import ChatSettings from './settings/ChatSettings'
import BrowserSettings from './settings/BrowserSettings'
import ChannelsSettings from './settings/ChannelsSettings'
import SessionsSettings from './settings/SessionsSettings'
import MemorySettings from './settings/MemorySettings'
import SubAgentsSettings from './settings/SubAgentsSettings'
import SkillsSettings from './settings/SkillsSettings'
import SchedulerSettings from './settings/SchedulerSettings'
import DaemonSettings from './settings/DaemonSettings'
import IdentitySettings from './settings/IdentitySettings'
import CredentialsSettings from './settings/CredentialsSettings'
import OpenCodeSettings from './settings/OpenCodeSettings'

// API functions
const fetchSettings = async () => {
  const res = await fetch('/api/settings/config')
  if (!res.ok) throw new Error('Failed to fetch settings')
  return res.json()
}

const saveSettings = async (data) => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  })
  if (!res.ok) throw new Error('Failed to save settings')
  return res.json()
}

const replaceAllProviders = async (providers) => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ providers: { '__replace_all__': { '__array__': JSON.stringify(providers) } } })
  })
  if (!res.ok) throw new Error('Failed to replace providers')
  return res.json()
}

const fetchMCPConfig = async () => {
  const res = await fetch('/api/settings/mcp')
  if (!res.ok) throw new Error('Failed to fetch MCP config')
  return res.json()
}

const saveMCPConfig = async (data) => {
  const res = await fetch('/api/settings/mcp', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  })
  if (!res.ok) throw new Error('Failed to save MCP config')
  return res.json()
}

const fetchProviderModels = async (providerId) => {
  const res = await fetch(`/api/providers/${providerId}/models`)
  if (!res.ok) throw new Error('Failed to fetch models')
  return res.json()
}

// Fetch tools that have 'websearch' or 'webextract' in their name
const fetchWebCapableTools = async () => {
  const res = await fetch('/api/tools/web-capable')
  if (!res.ok) throw new Error('Failed to fetch web-capable tools')
  return res.json()
}

// Taps API functions
const fetchTaps = async () => {
  const res = await fetch('/api/taps')
  if (!res.ok) throw new Error('Failed to fetch taps')
  return res.json()
}

const addTap = async (url, alias = '') => {
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

const removeTap = async (name) => {
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
const fetchMCPStatus = async () => {
  const res = await fetch('/api/mcp/status')
  if (!res.ok) throw new Error('Failed to fetch MCP status')
  return res.json()
}

// Toggle MCP server enabled/disabled
const toggleMCPServer = async (name, enabled) => {
  const res = await fetch(`/api/mcp/servers/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled })
  })
  if (!res.ok) throw new Error('Failed to toggle server')
  return res.json()
}

const refreshMCPServer = async (serverName) => {
  const res = await fetch(`/api/mcp/${encodeURIComponent(serverName)}/refresh`, {
    method: 'POST'
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || 'Failed to refresh server')
  }
  return res.json()
}

// Section keys that use the full config API
const FULL_CONFIG_SECTIONS = ['chat', 'browser', 'channels', 'sessions', 'memory', 'sub_agents', 'skills', 'scheduler', 'daemon', 'identity', 'open_code']

export default function SettingsPage({ onClose, activeSection = 'general', onSectionChange, onToolsRefresh, onSettingsSaved, updateAvailable = null, onUpdateClick = null, appVersion = 'dev', theme = 'dark' }) {
  // Use prop for active section, default to 'general'
  const [settings, setSettings] = useState(null)
  const [mcpConfig, setMcpConfig] = useState(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)

  // Form state
  const [generalForm, setGeneralForm] = useState({ 
    default_provider: '', 
    default_model: '',
    web_search_tool: '',
    web_extract_tool: ''
  })
  const [providerForms, setProviderForms] = useState({})
  const [mcpServers, setMcpServers] = useState({})
  const [mcpViewMode, setMcpViewMode] = useState('editor') // 'editor' or 'source'
  const [mcpSourceText, setMcpSourceText] = useState('')
  const [mcpSourceError, setMcpSourceError] = useState(null)
  // Track editable server names (key is stable ID, value is display name)
  const [mcpServerNames, setMcpServerNames] = useState({})
  // Track args as raw strings (key is server ID, value is comma-separated string)
  const [mcpServerArgs, setMcpServerArgs] = useState({})
  // Track if MCP config has unsaved changes (e.g., deletions)
  const [mcpHasChanges, setMcpHasChanges] = useState(false)
  // Track which MCP server card is expanded
  const [expandedMcpServer, setExpandedMcpServer] = useState(null)
  // Track which server is being saved
  const [savingServer, setSavingServer] = useState(null)
  // MCP Store modal
  const [showMCPStore, setShowMCPStore] = useState(false)
  // MCP server status (from /api/mcp/status)
  const [mcpServerStatus, setMcpServerStatus] = useState({})
  // MCP Inspector modal - which server to inspect
  const [inspectServer, setInspectServer] = useState(null)
  
  // Provider instance management
  const [expandedProvider, setExpandedProvider] = useState(null)
  const [showAddProvider, setShowAddProvider] = useState(false)
  const [newProviderName, setNewProviderName] = useState('')
  const [newProviderType, setNewProviderType] = useState('openai')
  const [deletingProvider, setDeletingProvider] = useState(null)

  // Model selector state
  const [showModelSelector, setShowModelSelector] = useState(false)
  const [availableModels, setAvailableModels] = useState([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [modelsError, setModelsError] = useState(null)

  // Web-capable tools state
  const [webCapableTools, setWebCapableTools] = useState({ webSearch: [], webExtract: [] })

  // Standard servers state
  const [standardServers, setStandardServers] = useState([])
  const [setupServer, setSetupServer] = useState(null) // Server being set up
  const [setupEnv, setSetupEnv] = useState({}) // Env vars being entered
  const [setupLoading, setSetupLoading] = useState(false)
  const [setupError, setSetupError] = useState(null)

  // Taps state
  const [taps, setTaps] = useState([])
  const [tapsLoading, setTapsLoading] = useState(false)
  const [tapsSuccess, setTapsSuccess] = useState(null) // Success message for refresh
  const [newTapUrl, setNewTapUrl] = useState('')
  const [newTapAlias, setNewTapAlias] = useState('')
  const [tapsError, setTapsError] = useState(null)

  // Full config state for new settings sections
  const [fullConfig, setFullConfig] = useState(null)
  const [fullConfigLoading, setFullConfigLoading] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  // Load taps when the taps section is opened
  useEffect(() => {
    if (activeSection === 'taps') {
      setTapsLoading(true)
      fetchTaps()
        .then(data => setTaps(data.taps || []))
        .catch(err => setTapsError(err.message))
        .finally(() => setTapsLoading(false))
    }
  }, [activeSection])

  // Load full config when a new settings section is opened
  useEffect(() => {
    if (FULL_CONFIG_SECTIONS.includes(activeSection) && !fullConfig) {
      setFullConfigLoading(true)
      fetchFullConfig()
        .then(data => setFullConfig(data))
        .catch(err => setError(err.message))
        .finally(() => setFullConfigLoading(false))
    }
  }, [activeSection, fullConfig])

  const loadMcpServerStatus = async () => {
    try {
      const data = await fetchMCPStatus()
      // Convert array to map by server name for easy lookup
      const statusMap = {}
      for (const server of (data.servers || [])) {
        statusMap[server.name] = server
      }
      setMcpServerStatus(statusMap)
    } catch (err) {
      console.error('Failed to fetch MCP status:', err)
    }
  }

  // Load MCP server status when MCP section is opened
  useEffect(() => {
    if (activeSection === 'mcp') {
      loadMcpServerStatus()
    }
  }, [activeSection])


  const loadData = async () => {
    setLoading(true)
    try {
      const [settingsData, mcpData, webTools, stdServers] = await Promise.all([
        fetchSettings(),
        fetchMCPConfig(),
        fetchWebCapableTools().catch(() => ({ webSearch: [], webExtract: [] })),
        fetch('/api/standard-servers').then(r => r.ok ? r.json() : { servers: [] }).catch(() => ({ servers: [] }))
      ])
      setSettings(settingsData)
      setMcpConfig(mcpData)
      setWebCapableTools(webTools)
      setStandardServers(stdServers.servers || [])
      setGeneralForm({
        default_provider: settingsData.general.default_provider || '',
        default_model: settingsData.general.default_model || '',
        web_search_tool: settingsData.general.web_search_tool || '',
        web_extract_tool: settingsData.general.web_extract_tool || ''
      })
      // Initialize provider forms
      const pForms = {}
      settingsData.providers.forEach(p => {
        pForms[p.name] = { ...p.fields }
      })
      setProviderForms(pForms)
      const servers = mcpData.mcpServers || {}
      setMcpServers(servers)
      // Initialize editable names and args
      const names = {}
      const args = {}
      Object.entries(servers).forEach(([name, server]) => {
        names[name] = name
        args[name] = (server.args || []).join(', ')
      })
      setMcpServerNames(names)
      setMcpServerArgs(args)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const handleSaveGeneral = async () => {
    setSaving(true)
    setSaveSuccess(false)
    try {
      await saveSettings({ general: generalForm })
      setSaveSuccess(true)
      if (onSettingsSaved) onSettingsSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleSaveProvider = async (providerName) => {
    setSaving(true)
    try {
      const currentProviders = (settings?.providers || []).map(p => {
        const provider = { name: p.name, type: p.type }
        if (p.fields) {
          for (const [key, val] of Object.entries(p.fields)) {
            provider[key] = val
          }
        }
        return provider
      })

      const existingProvider = settings?.providers?.find(p => p.name === providerName)
      const existingType = existingProvider?.type || providerName
      const newProviderConfig = { name: providerName, type: existingType }
      for (const [key, value] of Object.entries(providerForms[providerName] || {})) {
        if (value && key !== 'type') {
          newProviderConfig[key] = value
        }
      }

      let updatedProviders
      const existingIndex = currentProviders.findIndex(p => p.name === providerName)
      if (existingIndex >= 0) {
        updatedProviders = [...currentProviders]
        updatedProviders[existingIndex] = newProviderConfig
      } else {
        updatedProviders = [...currentProviders, newProviderConfig]
      }

      await replaceAllProviders(updatedProviders)

      setExpandedProvider(providerName)
      setSaving(false)
      setSaveSuccess(true)
      setTimeout(() => setSaveSuccess(false), 2000)
      loadData()
      if (onSettingsSaved) onSettingsSaved()
    } catch (err) {
      setSaving(false)
      setError(err.message)
    }
  }

  const handleSaveMCP = async () => {
    setSaving(true)
    setSaveSuccess(false)
    try {
      // Build final server config using editable names and args
      const finalServers = {}
      Object.entries(mcpServers).forEach(([id, server]) => {
        const finalName = mcpServerNames[id] || id
        const argsString = mcpServerArgs[id] || ''
        finalServers[finalName] = {
          ...server,
          args: argsString.split(',').map(s => s.trim()).filter(Boolean)
        }
      })
      await saveMCPConfig({ mcpServers: finalServers })
      setSaveSuccess(true)
      setMcpHasChanges(false)
      // Refresh tools cache in the UI
      if (onToolsRefresh) onToolsRefresh()
      // Refresh status to show loading/updated status
      loadMcpServerStatus()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const loadModelsForProvider = async (providerId) => {
    if (!providerId) {
      setAvailableModels([])
      return
    }
    setLoadingModels(true)
    setModelsError(null)
    try {
      const data = await fetchProviderModels(providerId)
      setAvailableModels(data.models || [])
    } catch (err) {
      setModelsError(err.message)
      setAvailableModels([])
    } finally {
      setLoadingModels(false)
    }
  }

  const handleProviderChange = (providerId) => {
    setGeneralForm({ ...generalForm, default_provider: providerId, default_model: '' })
    setAvailableModels([])
    setModelsError(null)
  }

  // Provider instance management
  const providerTypeOptions = [
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'gemini', label: 'Google GenAI' },
    { value: 'groq', label: 'Groq' },
    { value: 'litellm', label: 'LiteLLM' },
    { value: 'lm_studio', label: 'LM Studio' },
    { value: 'ollama', label: 'Ollama' },
    { value: 'openai', label: 'OpenAI' },
    { value: 'openai_compat', label: 'OpenAI Compatible' },
    { value: 'openrouter', label: 'OpenRouter' },
    { value: 'poe', label: 'Poe' },
    { value: 'sap_ai_core', label: 'SAP AI Core' },
    { value: 'xai', label: 'xAI' },
  ]

  const handleAddProvider = async () => {
    if (!newProviderName.trim()) {
      setError('Provider instance name is required')
      return
    }
    if (settings?.providers?.some(p => p.name === newProviderName.trim())) {
      setError('A provider with this name already exists')
      return
    }

    setSaving(true)
    try {
      await saveSettings({
        providers: {
          [newProviderName.trim()]: { type: newProviderType }
        }
      })
      setShowAddProvider(false)
      setNewProviderName('')
      setNewProviderType('openai')
      loadData()
      if (onSettingsSaved) onSettingsSaved()
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteProvider = async (providerName) => {
    setDeletingProvider(providerName)
    try {
      const currentProviders = (settings?.providers || []).map(p => {
        const provider = { name: p.name, type: p.type }
        if (p.fields) {
          for (const [key, val] of Object.entries(p.fields)) {
            provider[key] = val
          }
        }
        return provider
      })
      const updatedProviders = currentProviders.filter(p => p.name !== providerName)
      await replaceAllProviders(updatedProviders)

      if (generalForm.default_provider === providerName) {
        setGeneralForm({ ...generalForm, default_provider: '', default_model: '' })
      }

      loadData()
      if (onSettingsSaved) onSettingsSaved()
    } catch (err) {
      setError(err.message)
    } finally {
      setDeletingProvider(null)
    }
  }

  const getProviderFields = (type) => {
    const fieldMappings = {
      anthropic: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      gemini: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      openai: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      groq: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      litellm: [
        { key: 'api_key', label: 'API Key', type: 'password' },
        { key: 'base_url', label: 'Base URL', type: 'text' }
      ],
      lm_studio: [{ key: 'base_url', label: 'Base URL', type: 'text' }],
      ollama: [{ key: 'base_url', label: 'Base URL', type: 'text' }],
      openai_compat: [
        { key: 'api_key', label: 'API Key', type: 'password' },
        { key: 'base_url', label: 'Base URL', type: 'text' }
      ],
      openrouter: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      poe: [{ key: 'api_key', label: 'API Key', type: 'password' }],
      sap_ai_core: [
        { key: 'client_id', label: 'Client ID', type: 'text' },
        { key: 'client_secret', label: 'Client Secret', type: 'password' },
        { key: 'auth_url', label: 'Auth URL', type: 'text' },
        { key: 'base_url', label: 'Base URL', type: 'text' },
        { key: 'resource_group', label: 'Resource Group', type: 'text' }
      ],
      xai: [{ key: 'api_key', label: 'API Key', type: 'password' }],
    }
    return fieldMappings[type] || []
  }

  const handleAddMcpServer = () => {
    const newName = `server_${Date.now()}`
    // Insert at beginning by putting new key first
    setMcpServers({
      [newName]: { command: '', args: [], env: {}, transport: 'stdio' },
      ...mcpServers
    })
    setMcpServerNames({ [newName]: 'new-server', ...mcpServerNames })
    setMcpServerArgs({ [newName]: '', ...mcpServerArgs })
    // Auto-expand the new card
    setExpandedMcpServer(newName)
  }

  const handleRefreshMcpServer = async (serverName) => {
    // Optimistic update
    setMcpServerStatus(prev => ({
      ...prev,
      [serverName]: { ...(prev?.[serverName] || {}), name: serverName, status: 'loading', error: null }
    }))
    
    try {
      await refreshMCPServer(serverName)
      // Re-fetch status to get latest details
      loadMcpServerStatus()
      
      if (onToolsRefresh) onToolsRefresh()
    } catch (err) {
      console.error("Failed to refresh server:", err)
      setMcpServerStatus(prev => ({
        ...prev,
        [serverName]: { ...(prev?.[serverName] || {}), name: serverName, status: 'error', error: err.message }
      }))
    }
  }

  // Handle toggle enabled/disabled for MCP server
  const handleToggleMcpServer = async (serverId, serverName, currentEnabled) => {
    const newEnabled = !currentEnabled
    
    // Optimistic update in local state
    setMcpServers(prev => ({
      ...prev,
      [serverId]: { ...prev[serverId], enabled: newEnabled }
    }))
    
    try {
      await toggleMCPServer(serverName, newEnabled)
      // Refresh tools if needed
      if (onToolsRefresh) onToolsRefresh()
      // Refresh MCP status to reflect changes
      loadMcpServerStatus()
    } catch (err) {
      // Revert on error
      setMcpServers(prev => ({
        ...prev,
        [serverId]: { ...prev[serverId], enabled: currentEnabled }
      }))
      setError(`Failed to ${newEnabled ? 'enable' : 'disable'} server: ${err.message}`)
    }
  }

  const handleDeleteMcpServer = async (name) => {
    const newServers = { ...mcpServers }
    delete newServers[name]
    setMcpServers(newServers)
    const newNames = { ...mcpServerNames }
    delete newNames[name]
    setMcpServerNames(newNames)
    const newArgs = { ...mcpServerArgs }
    delete newArgs[name]
    setMcpServerArgs(newArgs)
    // Close if this was the expanded server
    if (expandedMcpServer === name) {
      setExpandedMcpServer(null)
    }
    // Save immediately after delete
    try {
      const finalServers = {}
      Object.entries(newServers).forEach(([id, server]) => {
        const finalName = newNames[id] || id
        const argsString = newArgs[id] || ''
        finalServers[finalName] = {
          ...server,
          args: argsString.split(',').map(s => s.trim()).filter(Boolean)
        }
      })
      await saveMCPConfig({ mcpServers: finalServers })
      if (onToolsRefresh) onToolsRefresh()
    } catch (err) {
      setError(err.message)
    }
  }

  // Save a single MCP server immediately
  const handleSaveSingleMcpServer = async (serverId) => {
    setSavingServer(serverId)
    try {
      // Build final server config using all current servers
      const finalServers = {}
      Object.entries(mcpServers).forEach(([id, server]) => {
        const finalName = mcpServerNames[id] || id
        const argsString = mcpServerArgs[id] || ''
        finalServers[finalName] = {
          ...server,
          args: argsString.split(',').map(s => s.trim()).filter(Boolean)
        }
      })
      await saveMCPConfig({ mcpServers: finalServers })
      setMcpHasChanges(false)
      // Refresh tools cache in the UI
      if (onToolsRefresh) onToolsRefresh()
      // Refresh status to show loading/updated status
      loadMcpServerStatus()
      // Collapse the card after successful save
      setExpandedMcpServer(null)
    } catch (err) {
      setError(err.message)
    } finally {
      setSavingServer(null)
    }
  }

  const menuItems = [
    { id: 'general', label: 'General', icon: Settings },
    { id: 'providers', label: 'Providers', icon: Key },
    { id: 'credentials', label: 'Credentials', icon: KeyRound },
    { id: 'chat', label: 'Chat', icon: MessageSquare },
    { id: 'browser', label: 'Browser', icon: Globe },
    { id: 'channels', label: 'Channels', icon: Radio },
    { id: 'mcp', label: 'MCP Servers', icon: Server },
    { id: 'sessions', label: 'Sessions', icon: Database },
    { id: 'memory', label: 'Memory', icon: Brain },
    { id: 'sub_agents', label: 'Sub-Agents', icon: GitFork },
    { id: 'open_code', label: 'OpenCode', icon: Terminal },
    { id: 'skills', label: 'Skills', icon: Wand2 },
    { id: 'scheduler', label: 'Scheduler', icon: Clock },
    { id: 'daemon', label: 'Daemon', icon: Shield },
    { id: 'identity', label: 'Agent Identity', icon: User },
    { id: 'taps', label: 'Repositories', icon: GitBranch },
    { id: 'flows', label: 'Flow Store', icon: Store },
  ]

  if (loading) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.8)' }}>
        <div className="text-white">Loading settings...</div>
      </div>
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex" style={{ background: 'var(--bg-primary)' }}>
      {/* Left Sidebar */}
      <div className="w-64 border-r flex flex-col" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Settings</h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg transition-colors"
            style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)', background: 'var(--bg-tertiary)' }}
          >
            <X size={18} />
          </button>
        </div>

        <nav className="flex-1 p-2 space-y-1 overflow-y-auto">
          {menuItems.map(item => {
            const Icon = item.icon
            const isActive = activeSection === item.id
            return (
              <button
                key={item.id}
                onClick={() => onSectionChange ? onSectionChange(item.id) : null}
                className="w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-all"
                style={{
                  background: isActive ? 'var(--accent-soft)' : 'transparent',
                  color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                  border: `1px solid ${isActive ? 'rgba(95, 79, 178, 0.25)' : 'transparent'}`
                }}
              >
                <Icon size={18} />
                <span className="font-medium text-sm">{item.label}</span>
                {isActive && <ChevronRight size={16} className="ml-auto" />}
              </button>
            )
          })}
        </nav>

        {/* Update Available Indicator */}
        {updateAvailable && onUpdateClick && (
          <div className="p-3 border-t" style={{ borderColor: 'var(--border-color)' }}>
            <button
              onClick={onUpdateClick}
              className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all hover:scale-[1.02]"
              style={{ 
                background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                border: '1px solid rgba(168, 85, 247, 0.3)'
              }}
            >
              <Download size={18} style={{ color: '#a855f7' }} />
              <div className="flex-1 text-left">
                <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Update Available</div>
                <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{updateAvailable.version}</div>
              </div>
            </button>
          </div>
        )}

        {/* Version Info */}
        <div className="p-3 border-t text-xs space-y-1" style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
          <div className="opacity-60">App Version: {appVersion}</div>
          <div className="opacity-60">UI Version: {__UI_VERSION__}</div>
        </div>
      </div>

      {/* Right Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <h3 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>
            {menuItems.find(m => m.id === activeSection)?.label}
          </h3>
          {saveSuccess && (
            <div className="flex items-center gap-2 text-green-400 text-sm">
              <Check size={16} />
              Saved successfully!
            </div>
          )}
          {error && (
            <div className="flex items-center gap-2 text-sm" style={{ color: 'var(--danger)' }}>
              <AlertCircle size={16} />
              {error}
            </div>
          )}
        </div>

        {/* Content */}
        <div className={activeSection === 'mcp' && mcpViewMode === 'source' ? 'flex-1 overflow-hidden' : 'flex-1 overflow-y-auto p-6'}>
          {activeSection === 'general' && (
            <div className="max-w-xl space-y-6">
              <div>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                  Default Provider
                </label>
                <select
                  value={generalForm.default_provider}
                  onChange={(e) => handleProviderChange(e.target.value)}
                  className="w-full px-4 py-2.5 rounded-lg border text-sm"
                  style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                >
                  <option value="">Select a provider...</option>
                  {settings?.providers.slice().sort((a, b) => a.name.localeCompare(b.name)).map(p => (
                    <option key={p.name} value={p.name}>
                      {p.name}{p.name !== p.display_name ? ` (${p.display_name})` : ''}
                      {!p.configured && ' (not configured)'}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                  Default Model
                </label>
                
                {/* Providers with enhanced selector — resolve instance name to type */}
                {(() => {
                  const providerType = settings?.providers?.find(p => p.name === generalForm.default_provider)?.type || ''
                  return ['openrouter', 'anthropic', 'gemini', 'groq', 'litellm', 'openai', 'poe', 'sap_ai_core', 'xai', 'lm_studio', 'ollama', 'openai_compat'].includes(providerType)
                })() ? (
                  <div>
                    <button
                      onClick={() => setShowModelSelector(true)}
                      className="w-full px-4 py-2.5 rounded-lg border text-sm text-left flex items-center justify-between"
                      style={{ 
                        background: 'var(--bg-secondary)', 
                        borderColor: 'var(--border-color)', 
                        color: generalForm.default_model ? 'var(--text-primary)' : 'var(--text-muted)' 
                      }}
                    >
                      <span className="truncate">
                        {generalForm.default_model || 'Click to select a model...'}
                      </span>
                      <Search size={16} style={{ color: 'var(--text-muted)' }} />
                    </button>
                    <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                      {(() => {
                        const pt = settings?.providers?.find(p => p.name === generalForm.default_provider)?.type || ''
                        return pt === 'openrouter'
                          ? 'Click to open model browser with pricing info'
                          : ['gemini', 'groq'].includes(pt)
                            ? 'Click to open model browser with context window'
                            : 'Click to open model browser'
                      })()}
                    </p>
                  </div>
                ) : (
                  /* Other providers: Standard dropdown */
                  <div className="relative">
                    <select
                      value={generalForm.default_model}
                      onChange={(e) => setGeneralForm({ ...generalForm, default_model: e.target.value })}
                      onFocus={() => {
                        if (generalForm.default_provider && availableModels.length === 0 && !loadingModels) {
                          loadModelsForProvider(generalForm.default_provider)
                        }
                      }}
                      className="w-full px-4 py-2.5 rounded-lg border text-sm"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                      disabled={!generalForm.default_provider}
                    >
                      {!generalForm.default_provider && (
                        <option value="">Select a provider first...</option>
                      )}
                      {generalForm.default_provider && availableModels.length === 0 && !loadingModels && (
                        <option value={generalForm.default_model || ''}>
                          {generalForm.default_model || 'Click to load models...'}
                        </option>
                      )}
                      {loadingModels && (
                        <option value="">Loading models...</option>
                      )}
                      {availableModels.length > 0 && (
                        <>
                          <option value="">Select a model...</option>
                          {availableModels.map(model => (
                            <option key={model} value={model}>{model}</option>
                          ))}
                        </>
                      )}
                    </select>
                    {loadingModels && (
                      <div className="absolute right-10 top-1/2 -translate-y-1/2">
                        <Loader2 size={16} className="animate-spin" style={{ color: 'var(--accent)' }} />
                      </div>
                    )}
                    {modelsError && (
                      <p className="text-xs mt-1" style={{ color: 'var(--danger)' }}>{modelsError}</p>
                    )}
                    <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                      Click the dropdown to load available models from the provider
                    </p>
                  </div>
                )}
              </div>

              {/* Web Tools Section */}
              <div className="pt-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
                <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
                  Web Tools
                </h4>
                
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                      Web Search Tool
                    </label>
                    <select
                      value={generalForm.web_search_tool}
                      onChange={(e) => setGeneralForm({ ...generalForm, web_search_tool: e.target.value })}
                      className="w-full px-4 py-2.5 rounded-lg border text-sm"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    >
                      <option value="">None (disabled)</option>
                      {webCapableTools.webSearch.map(t => (
                        <option key={`${t.source}:${t.name}`} value={`${t.source}:${t.name}`}>
                          {t.source} ({t.name})
                        </option>
                      ))}
                    </select>
                    <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                      Used for internet search when finding MCP servers online
                    </p>
                  </div>

                  <div>
                    <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                      Web Extract Tool
                    </label>
                    <select
                      value={generalForm.web_extract_tool}
                      onChange={(e) => setGeneralForm({ ...generalForm, web_extract_tool: e.target.value })}
                      className="w-full px-4 py-2.5 rounded-lg border text-sm"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    >
                      <option value="">None (disabled)</option>
                      {webCapableTools.webExtract.map(t => (
                        <option key={`${t.source}:${t.name}`} value={`${t.source}:${t.name}`}>
                          {t.source} ({t.name})
                        </option>
                      ))}
                    </select>
                    <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                      Used to extract content from URLs when user provides a link
                    </p>
                  </div>

                  {/* Quick setup hint if no web tools configured */}
                  {!generalForm.web_search_tool && !generalForm.web_extract_tool && standardServers.some(s => !s.installed) && (
                    <p className="text-xs p-2 rounded" style={{ 
                      color: 'var(--text-muted)', 
                      background: 'rgba(168, 85, 247, 0.1)',
                      border: '1px solid rgba(168, 85, 247, 0.2)'
                    }}>
                      No web tools configured. Go to the <button 
                        onClick={() => onSectionChange && onSectionChange('mcp')}
                        className="underline font-medium"
                        style={{ color: 'var(--accent)' }}
                      >MCP Servers</button> section to quick-install a web search provider.
                    </p>
                  )}
                </div>
              </div>

              <button
                onClick={handleSaveGeneral}
                disabled={saving}
                className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
              >
                <Save size={16} />
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          )}

          {activeSection === 'providers' && (
            <div className="space-y-6">
              {/* Add Provider Button */}
              <div className="flex items-center justify-between">
                <button
                  onClick={() => setShowAddProvider(true)}
                  className="flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95"
                  style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                >
                  <Plus size={16} />
                  Add Provider Instance
                </button>
                <span className="text-sm" style={{ color: 'var(--text-muted)' }}>
                  {settings?.providers?.length || 0} provider(s) configured
                </span>
              </div>

              {/* Provider Instances */}
              {settings?.providers?.map(provider => {
                const isExpanded = expandedProvider === provider.name
                const isDeleting = deletingProvider === provider.name
                const providerFields = getProviderFields(provider.type)

                return (
                  <div
                    key={provider.name}
                    className={`rounded-lg border transition-all ${
                      isExpanded ? 'border-purple-500 ring-1 ring-purple-500/30' : 
                      'hover:border-purple-500/50'
                    }`}
                    style={{ background: 'var(--bg-secondary)', borderColor: isExpanded ? undefined : 'var(--border-color)' }}
                  >
                    {/* Card Header */}
                    <div 
                      className="p-4 cursor-pointer"
                      onClick={() => setExpandedProvider(isExpanded ? null : provider.name)}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex items-center gap-3">
                          <div 
                            className="w-10 h-10 rounded-lg flex items-center justify-center"
                            style={{ 
                              background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                              border: '1px solid rgba(168, 85, 247, 0.3)'
                            }}
                          >
                            <Key size={18} style={{ color: '#a855f7' }} />
                          </div>
                          <div>
                            <div className="flex items-center gap-2">
                              <span className="text-lg font-medium" style={{ color: 'var(--text-primary)' }}>
                                {provider.name}
                              </span>
                              {provider.configured ? (
                                <span className="px-2 py-0.5 text-xs rounded" 
                                  style={{ background: 'rgba(20,150,71,0.12)', color: '#149647' }}>
                                  Configured
                                </span>
                              ) : (
                                <span className="px-2 py-0.5 text-xs rounded" 
                                  style={{ background: 'rgba(107,114,128,0.15)', color: '#6b7280' }}>
                                  Not configured
                                </span>
                              )}
                            </div>
                            <div className="text-sm" style={{ color: 'var(--text-muted)' }}>
                              {provider.display_name} {provider.name !== provider.display_name && `(${provider.name})`}
                            </div>
                          </div>
                        </div>
                        <div className="flex items-center gap-2">
                          {generalForm.default_provider === provider.name && (
                            <span className="px-2 py-0.5 text-xs rounded"
                              style={{ background: 'rgba(168, 85, 247, 0.2)', color: '#a855f7' }}>
                              Default
                            </span>
                          )}
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              handleDeleteProvider(provider.name)
                            }}
                            disabled={isDeleting}
                            className="p-2 rounded-lg text-red-400 hover:bg-red-500/20 transition-colors disabled:opacity-50"
                            title="Delete provider"
                          >
                            {isDeleting ? <Loader2 size={16} className="animate-spin" /> : <Trash2 size={16} />}
                          </button>
                          <ChevronRight 
                            size={20} 
                            className={`transition-transform ${isExpanded ? 'rotate-90' : ''}`}
                            style={{ color: 'var(--text-muted)' }}
                          />
                        </div>
                      </div>
                    </div>

                    {/* Expanded Form */}
                    {isExpanded && (
                      <div className="px-4 pb-4 pt-0 border-t" style={{ borderColor: 'var(--border-color)' }}>
                        <div className="pt-4 space-y-4">
                          {/* Instance Info */}
                          <div className="grid grid-cols-2 gap-4 p-3 rounded-lg" 
                            style={{ background: 'var(--bg-primary)' }}>
                            <div>
                              <div className="text-xs" style={{ color: 'var(--text-muted)' }}>Instance Name</div>
                              <div className="font-mono text-sm" style={{ color: 'var(--text-primary)' }}>{provider.name}</div>
                            </div>
                            <div>
                              <div className="text-xs" style={{ color: 'var(--text-muted)' }}>Provider Type</div>
                              <div className="text-sm" style={{ color: 'var(--text-primary)' }}>{provider.display_name}</div>
                            </div>
                          </div>

                          {/* Configuration Fields */}
                          <div className="space-y-3">
                            {providerFields.length === 0 ? (
                              <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                                No configuration fields for this provider type.
                              </p>
                            ) : (
                              providerFields.map(field => (
                                <div key={field.key}>
                                  <label className="block text-sm mb-1 capitalize" style={{ color: 'var(--text-secondary)' }}>
                                    {field.label}
                                  </label>
                                  <input
                                    type={field.type || 'text'}
                                    value={providerForms[provider.name]?.[field.key] || ''}
                                    onChange={(e) => setProviderForms({
                                      ...providerForms,
                                      [provider.name]: { ...providerForms[provider.name], [field.key]: e.target.value }
                                    })}
                                    placeholder={`Enter ${field.label.toLowerCase()}...`}
                                    className="w-full px-3 py-2 rounded border text-sm font-mono"
                                    style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                  />
                                </div>
                              ))
                            )}
                          </div>

                          {/* Save Button */}
                          <button
                            onClick={() => handleSaveProvider(provider.name)}
                            disabled={saving}
                            className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                            style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                          >
                            <Save size={16} />
                            {saving ? 'Saving...' : 'Save'}
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                )
              })}

              {(!settings?.providers || settings.providers.length === 0) && (
                <div className="text-center py-12 rounded-lg border border-dashed"
                  style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
                  <Key size={48} className="mx-auto mb-3 opacity-30" />
                  <p className="text-lg font-medium mb-2">No providers configured</p>
                  <p className="text-sm mb-4">Add your first provider instance to get started</p>
                  <button
                    onClick={() => setShowAddProvider(true)}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg font-medium mx-auto transition-all hover:scale-[1.02]"
                    style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                  >
                    <Plus size={16} />
                    Add Provider
                  </button>
                </div>
              )}
            </div>
          )}

          {activeSection === 'mcp' && (
            <div className={mcpViewMode === 'source' ? 'h-full flex flex-col' : 'space-y-4'}>
              {/* View toggle */}
              <div className="flex items-center gap-2">
                <div className="flex rounded-lg overflow-hidden border" style={{ borderColor: 'var(--border-color)' }}>
                  <button
                    onClick={() => {
                      setMcpViewMode('editor')
                      setMcpSourceError(null)
                    }}
                    className={`flex items-center gap-2 px-3 py-1.5 text-sm font-medium transition-colors ${
                      mcpViewMode === 'editor'
                        ? 'text-white shadow-sm'
                        : 'hover:bg-gray-600/20'
                    }`}
                    style={{
                      background: mcpViewMode === 'editor' ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' : 'transparent',
                      color: mcpViewMode !== 'editor' ? 'var(--text-secondary)' : undefined
                    }}
                  >
                    <LayoutGrid size={14} />
                    Editor
                  </button>
                  <button
                    onClick={() => {
                      setMcpViewMode('source')
                      setMcpSourceText(JSON.stringify({ mcpServers }, null, 2))
                      setMcpSourceError(null)
                    }}
                    className={`flex items-center gap-2 px-3 py-1.5 text-sm font-medium transition-all ${
                      mcpViewMode === 'source'
                        ? 'text-white shadow-sm'
                        : 'hover:bg-gray-600/20'
                    }`}
                    style={{
                      background: mcpViewMode === 'source' ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' : undefined,
                      color: mcpViewMode !== 'source' ? 'var(--text-secondary)' : undefined
                    }}
                  >
                    <Code size={14} />
                    Source
                  </button>
                </div>
              </div>

              {/* Editor View */}
              {mcpViewMode === 'editor' && (
                <>
                  {/* Standard Web Servers Section */}
                  {standardServers.length > 0 && (
                    <div className="mb-4 p-4 rounded-lg border" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
                      <h4 className="text-sm font-medium mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                        <Search size={14} style={{ color: '#a855f7' }} />
                        Web Search Providers
                      </h4>
                      <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
                        Configure a web search provider to enable web search and content extraction.
                      </p>
                      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                        {standardServers.map(srv => (
                          <div key={srv.id} className="p-3 rounded-lg border transition-all" style={{ 
                            borderColor: srv.installed ? 'rgba(34, 197, 94, 0.3)' : 'var(--border-color)',
                            background: srv.installed ? 'rgba(34, 197, 94, 0.05)' : 'var(--bg-tertiary)'
                          }}>
                            <div className="flex items-center justify-between mb-1">
                              <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                                {srv.displayName}
                                {srv.isDefault && !srv.installed && (
                                  <span className="ml-1 text-xs px-1.5 py-0.5 rounded-full" style={{ background: 'rgba(168, 85, 247, 0.15)', color: '#a855f7' }}>
                                    recommended
                                  </span>
                                )}
                              </span>
                              {srv.installed && <Check size={14} style={{ color: '#22c55e' }} />}
                            </div>
                            <p className="text-xs mb-2" style={{ color: 'var(--text-muted)' }}>
                              {srv.envVars?.length === 0 ? 'Browser Automation' : srv.capabilities.webSearch && srv.capabilities.webExtract ? 'Search + Extract' : 'Search only'}
                            </p>
                            {srv.envVars?.length === 0 && srv.installed ? (
                              <div className="flex items-center gap-2">
                                <span className="text-xs" style={{ color: '#22c55e' }}>Active</span>
                                <span className="text-xs" style={{ color: 'var(--text-muted)' }}>No setup required</span>
                              </div>
                            ) : setupServer === srv.id ? (
                              <div className="space-y-2">
                                {srv.envVars.map(ev => (
                                  <input
                                    key={ev.name}
                                    type="password"
                                    placeholder={ev.name}
                                    value={setupEnv[ev.name] || ''}
                                    onChange={(e) => setSetupEnv({ ...setupEnv, [ev.name]: e.target.value })}
                                    className="w-full px-2 py-1.5 rounded border text-xs"
                                    style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                  />
                                ))}
                                {setupError && (
                                  <p className="text-xs" style={{ color: '#ef4444' }}>{setupError}</p>
                                )}
                                <div className="flex gap-2">
                                  <button
                                    onClick={async () => {
                                      setSetupLoading(true)
                                      setSetupError(null)
                                      try {
                                        const res = await fetch(`/api/standard-servers/${srv.id}/install`, {
                                          method: 'POST',
                                          headers: { 'Content-Type': 'application/json' },
                                          body: JSON.stringify({ env: setupEnv })
                                        })
                                        if (!res.ok) {
                                          const text = await res.text()
                                          throw new Error(text)
                                        }
                                        const result = await res.json()
                                        setSetupServer(null)
                                        setSetupEnv({})
                                        await loadData()
                                        if (onToolsRefresh) onToolsRefresh()
                                        if (result.webSearchTool) {
                                          setGeneralForm(prev => ({
                                            ...prev,
                                            web_search_tool: result.webSearchTool,
                                            web_extract_tool: result.webExtractTool || prev.web_extract_tool
                                          }))
                                        }
                                      } catch (err) {
                                        setSetupError(err.message)
                                      } finally {
                                        setSetupLoading(false)
                                      }
                                    }}
                                    disabled={setupLoading || srv.envVars.some(ev => ev.required && !setupEnv[ev.name])}
                                    className="flex items-center gap-1 px-2 py-1 rounded text-xs font-medium text-white disabled:opacity-50"
                                    style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                                  >
                                    {setupLoading ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
                                    {srv.installed ? 'Reconfigure' : 'Install'}
                                  </button>
                                  <button
                                    onClick={() => { setSetupServer(null); setSetupEnv({}); setSetupError(null) }}
                                    className="px-2 py-1 rounded text-xs"
                                    style={{ color: 'var(--text-muted)' }}
                                  >
                                    Cancel
                                  </button>
                                </div>
                              </div>
                            ) : srv.installed ? (
                              <div className="flex items-center gap-2">
                                <span className="text-xs" style={{ color: '#22c55e' }}>Configured</span>
                                <button
                                  onClick={() => { setSetupServer(srv.id); setSetupEnv({}); setSetupError(null) }}
                                  className="text-xs px-1.5 py-0.5 rounded transition-colors"
                                  style={{ color: 'var(--text-muted)' }}
                                >
                                  Reconfigure
                                </button>
                                {srv.envVars?.length > 0 && (
                                  <button
                                    onClick={async () => {
                                      try {
                                        const res = await fetch(`/api/standard-servers/${srv.id}`, { method: 'DELETE' })
                                        if (!res.ok) throw new Error('Failed to remove server')
                                        await loadData()
                                        if (onToolsRefresh) onToolsRefresh()
                                      } catch (err) {
                                        console.error('Failed to remove standard server:', err)
                                      }
                                    }}
                                    className="p-0.5 rounded transition-colors hover:bg-red-500/10"
                                    style={{ color: 'var(--text-muted)' }}
                                    title="Remove configuration"
                                  >
                                    <Trash2 size={12} />
                                  </button>
                                )}
                              </div>
                            ) : (
                              <button
                                onClick={() => { setSetupServer(srv.id); setSetupEnv({}); setSetupError(null) }}
                                className="text-xs font-medium px-2 py-1 rounded transition-colors"
                                style={{ color: '#a855f7', background: 'rgba(168, 85, 247, 0.1)' }}
                              >
                                Setup
                              </button>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="flex items-center gap-3">
                    <button
                      onClick={() => setShowMCPStore(true)}
                      className="flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95"
                      style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                    >
                      <Package size={16} />
                      Browse Store
                    </button>
                    <button
                      onClick={handleAddMcpServer}
                      className="flex items-center gap-2 px-4 py-2 rounded-lg border font-medium transition-colors"
                      style={{ borderColor: 'var(--border-color)', color: 'var(--text-secondary)', background: 'var(--bg-tertiary)' }}
                    >
                      <Plus size={16} />
                      Add Manual
                    </button>
                  </div>

                  {/* Grid of MCP Server Cards (excludes standard web servers) */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {Object.entries(mcpServers)
                      .filter(([name]) => !standardServers.some(s => s.id === name))
                      .map(([name, server]) => {
                      const isExpanded = expandedMcpServer === name
                      const displayName = mcpServerNames[name] || name
                      const isSaving = savingServer === name
                      const serverStatus = mcpServerStatus[displayName]
                      const hasError = serverStatus?.status === 'error'
                      // enabled defaults to true if not explicitly set
                      const isEnabled = server.enabled !== false
                      
                      return (
                        <div
                          key={name}
                          className={`rounded-lg border cursor-pointer transition-all ${
                            isExpanded ? 'border-purple-500 ring-1 ring-purple-500/30 md:col-span-2' : 
                            hasError ? 'border-red-500/50' : 'hover:border-purple-500/50'
                          }`}
                          style={{ 
                            background: 'var(--bg-secondary)', 
                            borderColor: isExpanded ? undefined : 'var(--border-color)' 
                          }}
                        >
                          {/* Card Header - Always Visible */}
                          <div 
                            className="p-4"
                            onClick={() => setExpandedMcpServer(isExpanded ? null : name)}
                          >
                            {/* Top row: icon, title, actions */}
                            <div className="flex items-center gap-3">
                              {/* Server Icon with Status Indicator */}
                              <div className="relative shrink-0">
                                <div 
                                  className="w-10 h-10 rounded-lg flex items-center justify-center"
                                  style={{ 
                                    background: hasError 
                                      ? 'linear-gradient(135deg, rgba(239, 68, 68, 0.2) 0%, rgba(220, 38, 38, 0.2) 100%)'
                                      : !isEnabled
                                        ? 'rgba(107, 114, 128, 0.15)'
                                        : 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                                    border: hasError 
                                      ? '1px solid rgba(239, 68, 68, 0.3)'
                                      : !isEnabled
                                        ? '1px solid var(--border-color)'
                                        : '1px solid rgba(168, 85, 247, 0.3)'
                                  }}
                                >
                                  <Server size={18} style={{ color: hasError ? '#ef4444' : !isEnabled ? 'var(--text-muted)' : '#a855f7' }} />
                                </div>
                                {/* Status dot */}
                                {serverStatus && isEnabled && (
                                  <div 
                                    className="absolute -top-1 -right-1 w-3 h-3 rounded-full border-2"
                                    style={{ 
                                      background: serverStatus.status === 'healthy' ? '#22c55e' : 
                                                  serverStatus.status === 'error' ? '#ef4444' : '#f59e0b',
                                      borderColor: 'var(--bg-secondary)'
                                    }}
                                    title={serverStatus.status === 'healthy' 
                                      ? `Healthy - ${serverStatus.tool_count} tools` 
                                      : serverStatus.error || 'Unknown status'}
                                  />
                                )}
                              </div>
                              
                              {/* Title */}
                              <div className="flex-1 min-w-0">
                                <h3 className="font-semibold text-base truncate" style={{ color: isEnabled ? 'var(--text-primary)' : 'var(--text-muted)' }}>
                                  {displayName}
                                </h3>
                              </div>

                              {/* Actions row - vertically centered */}
                              <div className="flex items-center gap-2 shrink-0">
                                {/* Test button */}
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    setInspectServer(displayName)
                                  }}
                                  className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs font-medium transition-all hover:scale-[1.02]"
                                  style={{ 
                                    background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.15) 0%, rgba(124, 58, 237, 0.15) 100%)',
                                    color: '#a855f7',
                                    border: '1px solid rgba(168, 85, 247, 0.3)',
                                    opacity: isEnabled ? 1 : 0.4
                                  }}
                                  title="Test tools from this server"
                                  disabled={!isEnabled}
                                >
                                  <Play size={12} />
                                  Test
                                </button>

                                {/* Enable/Disable Toggle */}
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    handleToggleMcpServer(name, displayName, isEnabled)
                                  }}
                                  className="relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus:outline-none"
                                  style={{ 
                                    background: isEnabled 
                                      ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' 
                                      : 'var(--bg-tertiary)',
                                    border: isEnabled ? 'none' : '1px solid var(--border-color)'
                                  }}
                                  title={isEnabled ? 'Disable server' : 'Enable server'}
                                >
                                  <span
                                    className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform ${
                                      isEnabled ? 'translate-x-[18px]' : 'translate-x-[3px]'
                                    }`}
                                  />
                                </button>

                                <ChevronRight 
                                  size={18} 
                                  className={`transition-transform ${isExpanded ? 'rotate-90' : ''}`}
                                  style={{ color: 'var(--text-muted)' }}
                                />
                              </div>
                            </div>
                            
                            {/* Details row below title */}
                            <div className="mt-2 ml-[52px]" style={{ opacity: isEnabled ? 1 : 0.5 }}>
                              {/* Command/URL line */}
                              <div className="flex items-center gap-2">
                                <code 
                                  className="text-xs font-mono px-2 py-1 rounded truncate max-w-[200px]"
                                  style={{ 
                                    background: 'var(--bg-primary)', 
                                    color: 'var(--text-secondary)',
                                    border: '1px solid var(--border-color)'
                                  }}
                                >
                                  {(server.transport || 'stdio') === 'stdio' 
                                    ? server.command || 'no command' 
                                    : server.url || 'no url'}
                                </code>
                                
                                {/* Transport Badge */}
                                <span 
                                  className="shrink-0 text-xs font-medium px-2 py-1 rounded flex items-center gap-1"
                                  style={{ 
                                    background: (server.transport || 'stdio') === 'stdio' 
                                      ? 'rgba(34, 197, 94, 0.15)' 
                                      : 'rgba(59, 130, 246, 0.15)',
                                    color: (server.transport || 'stdio') === 'stdio' 
                                      ? '#22c55e' 
                                      : '#3b82f6',
                                    border: `1px solid ${(server.transport || 'stdio') === 'stdio' ? 'rgba(34, 197, 94, 0.3)' : 'rgba(59, 130, 246, 0.3)'}`
                                  }}
                                >
                                  {server.transport || 'stdio'}
                                </span>
                              </div>
                              
                              {/* Environment Variables - show as subtle tags */}
                              {server.env && Object.keys(server.env).length > 0 && !isExpanded && (
                                <div className="flex items-center gap-1.5 mt-2">
                                  <Key size={12} style={{ color: 'var(--text-muted)' }} />
                                  <div className="flex flex-wrap gap-1">
                                    {Object.keys(server.env).slice(0, 2).map(key => (
                                      <span 
                                        key={key}
                                        className="text-xs px-1.5 py-0.5 rounded"
                                        style={{ 
                                          background: 'rgba(168, 85, 247, 0.1)', 
                                          color: 'var(--text-muted)',
                                          border: '1px solid rgba(168, 85, 247, 0.2)'
                                        }}
                                      >
                                        {key}
                                      </span>
                                    ))}
                                    {Object.keys(server.env).length > 2 && (
                                      <span 
                                        className="text-xs px-1.5 py-0.5 rounded"
                                        style={{ color: 'var(--text-muted)' }}
                                      >
                                        +{Object.keys(server.env).length - 2} more
                                      </span>
                                    )}
                                  </div>
                                </div>
                              )}

                              {/* Error message display */}
                              {hasError && !isExpanded && (
                                <div 
                                  className="flex items-start gap-2 mt-2 p-2 rounded text-xs"
                                  style={{ 
                                    background: 'rgba(239, 68, 68, 0.1)', 
                                    border: '1px solid rgba(239, 68, 68, 0.2)',
                                    color: '#f87171'
                                  }}
                                >
                                  <AlertCircle size={14} className="shrink-0 mt-0.5" />
                                  <div className="flex-1">
                                    <div className="font-medium">Failed to load</div>
                                    <div className="opacity-80 mt-0.5">{serverStatus.error}</div>
                                  </div>
                                  {serverStatus?.status === 'loading' ? (
                                    <Loader2 size={14} className="animate-spin shrink-0 mt-0.5 opacity-50" />
                                  ) : (
                                    <button 
                                      onClick={(e) => { e.stopPropagation(); handleRefreshMcpServer(displayName) }}
                                      className="p-1 hover:bg-white/10 rounded transition-colors"
                                      title="Retry"
                                    >
                                      <RefreshCw size={14} />
                                    </button>
                                  )}
                                </div>
                              )}
                            </div>
                          </div>
                          
                          {/* Expanded Form */}
                          {isExpanded && (
                            <div className="px-4 pb-4 pt-0 border-t" style={{ borderColor: 'var(--border-color)' }}>
                              <div className="pt-4 space-y-4">
                                {/* Server Name */}
                                <div>
                                  <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Server Name</label>
                                  <input
                                    type="text"
                                    value={displayName}
                                    onChange={(e) => setMcpServerNames({ ...mcpServerNames, [name]: e.target.value })}
                                    onClick={(e) => e.stopPropagation()}
                                    className="w-full px-3 py-2 rounded border text-sm"
                                    style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                    placeholder="Server name"
                                  />
                                </div>
                                
                                {/* Transport & Command/URL */}
                                <div className="grid grid-cols-2 gap-4">
                                  <div>
                                    <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Transport</label>
                                    <select
                                      value={server.transport || 'stdio'}
                                      onChange={(e) => {
                                        e.stopPropagation()
                                        setMcpServers({
                                          ...mcpServers,
                                          [name]: { ...server, transport: e.target.value }
                                        })
                                      }}
                                      onClick={(e) => e.stopPropagation()}
                                      className="w-full px-3 py-2 rounded border text-sm"
                                      style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                    >
                                      <option value="stdio">stdio</option>
                                      <option value="sse">sse</option>
                                    </select>
                                  </div>
                                  {(server.transport || 'stdio') === 'stdio' ? (
                                    <div>
                                      <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Command</label>
                                      <input
                                        type="text"
                                        value={server.command || ''}
                                        onChange={(e) => {
                                          e.stopPropagation()
                                          setMcpServers({
                                            ...mcpServers,
                                            [name]: { ...server, command: e.target.value }
                                          })
                                        }}
                                        onClick={(e) => e.stopPropagation()}
                                        placeholder="e.g., npx"
                                        className="w-full px-3 py-2 rounded border text-sm font-mono"
                                        style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                      />
                                    </div>
                                  ) : (
                                    <div>
                                      <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>URL</label>
                                      <input
                                        type="text"
                                        value={server.url || ''}
                                        onChange={(e) => {
                                          e.stopPropagation()
                                          setMcpServers({
                                            ...mcpServers,
                                            [name]: { ...server, url: e.target.value }
                                          })
                                        }}
                                        onClick={(e) => e.stopPropagation()}
                                        placeholder="e.g., http://localhost:8080/sse"
                                        className="w-full px-3 py-2 rounded border text-sm font-mono"
                                        style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                      />
                                    </div>
                                  )}
                                </div>
                                
                                {/* Args (for stdio only) */}
                                {(server.transport || 'stdio') === 'stdio' && (
                                  <div>
                                    <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Args (comma-separated)</label>
                                    <input
                                      type="text"
                                      value={mcpServerArgs[name] !== undefined ? mcpServerArgs[name] : (server.args || []).join(', ')}
                                      onChange={(e) => {
                                        e.stopPropagation()
                                        setMcpServerArgs({ ...mcpServerArgs, [name]: e.target.value })
                                      }}
                                      onClick={(e) => e.stopPropagation()}
                                      placeholder="e.g., -y, @anthropic-ai/mcp-server-github"
                                      className="w-full px-3 py-2 rounded border text-sm font-mono"
                                      style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                    />
                                  </div>
                                )}
                                
                                {/* Environment Variables */}
                                <div>
                                  <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Environment (JSON)</label>
                                  <textarea
                                    value={Object.keys(server.env || {}).length > 0 ? JSON.stringify(server.env, null, 2) : ''}
                                    onChange={(e) => {
                                      e.stopPropagation()
                                      try {
                                        const env = e.target.value ? JSON.parse(e.target.value) : {}
                                        setMcpServers({
                                          ...mcpServers,
                                          [name]: { ...server, env }
                                        })
                                      } catch {}
                                    }}
                                    onClick={(e) => e.stopPropagation()}
                                    placeholder={'{\n  "KEY": "value"\n}'}
                                    rows={4}
                                    className="w-full px-3 py-2 rounded border text-sm font-mono resize-y"
                                    style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                                  />
                                </div>
                                
                                {/* Action Buttons */}
                                <div className="flex items-center justify-between pt-2">
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      handleDeleteMcpServer(name)
                                    }}
                                    className="flex items-center gap-2 px-3 py-1.5 rounded text-sm text-red-400 hover:text-red-300 hover:bg-red-500/20 transition-colors"
                                  >
                                    <Trash2 size={14} />
                                    Delete
                                  </button>
                                  <button
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      handleSaveSingleMcpServer(name)
                                    }}
                                    disabled={isSaving}
                                    className="flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                                    style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                                  >
                                    {isSaving ? (
                                      <>
                                        <Loader2 size={14} className="animate-spin" />
                                        Saving...
                                      </>
                                    ) : (
                                      <>
                                        <Save size={14} />
                                        Save
                                      </>
                                    )}
                                  </button>
                                </div>
                              </div>
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>

                  {Object.keys(mcpServers).length === 0 && (
                    <div className="text-center py-8" style={{ color: 'var(--text-muted)' }}>
                      <Server size={48} className="mx-auto mb-3 opacity-30" />
                      <p>No MCP servers configured.</p>
                      <p className="text-sm mt-1">Click "Browse Store" or "Add Manual" to add one.</p>
                    </div>
                  )}
                </>
              )}

              {/* Source View */}
              {mcpViewMode === 'source' && (
                <div className="flex flex-col h-full">
                  <div className="flex items-center justify-between px-6 pt-4 mb-4 flex-shrink-0">
                    <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                      Edit the raw JSON configuration below. Changes will be synced when you save or switch back to Editor view.
                    </p>
                    {mcpSourceError && (
                      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg" style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)' }}>
                        <AlertCircle size={14} />
                        <span className="text-sm">{mcpSourceError}</span>
                      </div>
                    )}
                  </div>
                  <div className="flex-1 overflow-hidden mx-6 mb-4" style={{ maxHeight: 'calc(100vh - 220px)' }}>
                    <div className="h-full rounded-lg border" style={{ borderColor: 'var(--border-color)' }}>
                      <CodeMirror
                        value={mcpSourceText}
                        onChange={(value) => {
                          setMcpSourceText(value)
                          try {
                            JSON.parse(value)
                            setMcpSourceError(null)
                          } catch {}
                        }}
                        height="100%"
                        className="h-full"
                        extensions={[
                          json(),
                          search({ scrollToMatch: (range) => EditorView.scrollIntoView(range, { y: 'center', yMargin: 100 }) }),
                          highlightSelectionMatches(),
                          keymap.of(searchKeymap),
                        ]}
                        theme={theme === 'dark' ? 'dark' : 'light'}
                        basicSetup={{
                          lineNumbers: true,
                          highlightActiveLineGutter: true,
                          highlightActiveLine: true,
                          foldGutter: true,
                        }}
                      />
                    </div>
                  </div>
                  <div className="flex items-center justify-end gap-3 px-6 pb-6 flex-shrink-0">
                    <button
                      onClick={async () => {
                        try {
                          const parsed = JSON.parse(mcpSourceText)
                          if (parsed.mcpServers && typeof parsed.mcpServers === 'object') {
                            setSaving(true)
                            await saveMCPConfig({ mcpServers: parsed.mcpServers })
                            setMcpServers(parsed.mcpServers)
                            const names = {}
                            const args = {}
                            Object.entries(parsed.mcpServers).forEach(([name, server]) => {
                              names[name] = name
                              args[name] = Array.isArray(server.args) ? server.args.join(', ') : ''
                            })
                            setMcpServerNames(names)
                            setMcpServerArgs(args)
                            setMcpSourceError(null)
                            setMcpHasChanges(false)
                            setSaveSuccess(true)
                            if (onToolsRefresh) onToolsRefresh()
                            setTimeout(() => setSaveSuccess(false), 2000)
                            setSaving(false)
                          } else {
                            setMcpSourceError('Invalid format: expected { "mcpServers": { ... } }')
                          }
                        } catch (e) {
                          setMcpSourceError(`Invalid JSON: ${e.message}`)
                          setSaving(false)
                        }
                      }}
                      disabled={saving}
                      className="flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                      style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                    >
                      <Save size={16} />
                      {saving ? 'Saving...' : 'Apply & Save'}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          {activeSection === 'taps' && (
            <div className="max-w-2xl space-y-6">
              <p style={{ color: 'var(--text-muted)' }}>
                Manage extension repositories (taps) that provide flows and MCP servers.
              </p>

              {/* Add Tap Form */}
              <div className="p-4 rounded-lg border" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
                <h4 className="font-medium mb-3" style={{ color: 'var(--text-primary)' }}>Add Repository</h4>
                <div className="space-y-3">
                  <div>
                    <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary)' }}>Repository URL or owner/repo</label>
                    <input
                      type="text"
                      value={newTapUrl}
                      onChange={(e) => setNewTapUrl(e.target.value)}
                      placeholder="schardosin/astonish-flows"
                      className="w-full px-3 py-2 rounded border"
                      style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    />
                  </div>
                  <div>
                    <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary)' }}>Alias (optional)</label>
                    <input
                      type="text"
                      value={newTapAlias}
                      onChange={(e) => setNewTapAlias(e.target.value)}
                      placeholder="my-flows"
                      className="w-full px-3 py-2 rounded border"
                      style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    />
                  </div>
                  {tapsError && (
                    <div className="text-red-400 text-sm flex items-center gap-2">
                      <AlertCircle size={14} />
                      {tapsError}
                    </div>
                  )}
                  <button
                    onClick={async () => {
                      if (!newTapUrl) return
                      setTapsError(null)
                      setTapsLoading(true)
                      try {
                        await addTap(newTapUrl, newTapAlias)
                        setNewTapUrl('')
                        setNewTapAlias('')
                        const data = await fetchTaps()
                        setTaps(data.taps || [])
                      } catch (err) {
                        setTapsError(err.message)
                      } finally {
                        setTapsLoading(false)
                      }
                    }}
                    disabled={tapsLoading || !newTapUrl}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                    style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
                  >
                    {tapsLoading ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
                    Add Repository
                  </button>
                </div>
              </div>

              {/* Tap List */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h4 className="font-medium" style={{ color: 'var(--text-primary)' }}>Configured Repositories</h4>
                  <div className="flex items-center gap-2">
                    {tapsSuccess && (
                      <span className="flex items-center gap-1 text-sm text-green-400">
                        <Check size={14} />
                        {tapsSuccess}
                      </span>
                    )}
                    <button
                      onClick={async () => {
                        setTapsLoading(true)
                        setTapsError(null)
                        setTapsSuccess(null)
                        try {
                          // First refresh manifests from remote
                          await fetch('/api/taps/update', { method: 'POST' })
                          // Then fetch updated taps list
                          const data = await fetchTaps()
                          setTaps(data.taps || [])
                          setTapsSuccess('Updated!')
                          setTimeout(() => setTapsSuccess(null), 3000)
                        } catch (err) {
                          setTapsError(err.message)
                        } finally {
                          setTapsLoading(false)
                        }
                      }}
                      disabled={tapsLoading}
                      className="flex items-center gap-1.5 px-2 py-1 rounded text-sm hover:bg-gray-600/30 transition-colors disabled:opacity-50"
                      style={{ color: 'var(--text-muted)' }}
                      title="Refresh manifests from remote"
                    >
                      <RefreshCw size={14} className={tapsLoading ? 'animate-spin' : ''} />
                      {tapsLoading ? 'Refreshing...' : 'Refresh'}
                    </button>
                  </div>
                </div>
                {taps.length === 0 ? (
                  <div className="text-sm p-4 rounded border border-dashed" 
                       style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
                    No repositories configured. Add one above or click refresh.
                  </div>
                ) : (
                  taps.map((tap) => (
                    <div 
                      key={tap.name} 
                      className="flex items-center justify-between p-3 rounded-lg border"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
                    >
                      <div className="flex-1">
                        <div className="flex items-center gap-2">
                          <span className="font-medium" style={{ color: 'var(--text-primary)' }}>{tap.name}</span>
                          {tap.name === 'official' && (
                            <span className="text-xs px-2 py-0.5 rounded bg-purple-600/20 text-purple-400">official</span>
                          )}
                        </div>
                        <div className="text-sm" style={{ color: 'var(--text-muted)' }}>{tap.url}</div>
                      </div>
                      {tap.name !== 'official' && (
                        <button
                          onClick={async () => {
                            try {
                              await removeTap(tap.name)
                              const data = await fetchTaps()
                              setTaps(data.taps || [])
                            } catch (err) {
                              setTapsError(err.message)
                            }
                          }}
                          className="p-2 text-red-400 hover:bg-red-600/20 rounded"
                        >
                          <Trash2 size={16} />
                        </button>
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
          )}

          {activeSection === 'flows' && (
            <FlowStorePanel />
          )}

          {/* New settings sections */}
          {FULL_CONFIG_SECTIONS.includes(activeSection) && fullConfigLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
              <span className="ml-2 text-sm" style={{ color: 'var(--text-muted)' }}>Loading settings...</span>
            </div>
          )}

          {activeSection === 'chat' && fullConfig && (
            <ChatSettings config={fullConfig.chat} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'browser' && fullConfig && (
            <BrowserSettings config={fullConfig.browser} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'channels' && fullConfig && (
            <ChannelsSettings config={fullConfig.channels} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'sessions' && fullConfig && (
            <SessionsSettings config={fullConfig.sessions} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'memory' && fullConfig && (
            <MemorySettings config={fullConfig.memory} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'sub_agents' && fullConfig && (
            <SubAgentsSettings config={fullConfig.sub_agents} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'skills' && fullConfig && (
            <SkillsSettings config={fullConfig.skills} onSaved={() => setFullConfig(null)} theme={theme} />
          )}

          {activeSection === 'scheduler' && fullConfig && (
            <SchedulerSettings config={fullConfig.scheduler} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'daemon' && fullConfig && (
            <DaemonSettings config={fullConfig.daemon} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'identity' && fullConfig && (
            <IdentitySettings config={fullConfig.agent_identity} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'open_code' && fullConfig && (
            <OpenCodeSettings config={fullConfig.open_code} onSaved={() => setFullConfig(null)} />
          )}

          {activeSection === 'credentials' && (
            <CredentialsSettings />
          )}
        </div>
      </div>

      {/* MCP Store Modal */}
      <MCPStoreModal
        isOpen={showMCPStore}
        onClose={() => setShowMCPStore(false)}
        onInstall={() => {
          // Close modal and refresh data to show new server
          setShowMCPStore(false)
          loadData()
          loadMcpServerStatus()
          if (onToolsRefresh) onToolsRefresh()
        }}
      />

      {/* Enhanced Model Selector */}
      <ProviderModelSelector
        isOpen={showModelSelector}
        onClose={() => setShowModelSelector(false)}
        onSelect={(modelId) => setGeneralForm({ ...generalForm, default_model: modelId })}
        currentModel={generalForm.default_model}
        provider={generalForm.default_provider}
      />

      {/* MCP Inspector Modal */}
      {inspectServer && (
        <MCPInspector
          serverName={inspectServer}
          onClose={() => setInspectServer(null)}
        />
      )}

      {/* Add Provider Modal */}
      {showAddProvider && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ background: 'rgba(0,0,0,0.7)' }}>
          <div 
            className="rounded-xl w-full max-w-md p-6 shadow-2xl"
            style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', border: '1px solid var(--border-color)' }}
          >
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>
                Add Provider Instance
              </h2>
              <button
                onClick={() => {
                  setShowAddProvider(false)
                  setNewProviderName('')
                  setNewProviderType('openai')
                  setError(null)
                }}
                className="p-1.5 rounded-lg hover:bg-gray-600/30 transition-colors"
                style={{ color: 'var(--text-muted)' }}
              >
                <X size={20} />
              </button>
            </div>

            <div className="space-y-4">
              {/* Instance Name */}
              <div>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                  Instance Name
                </label>
                <input
                  type="text"
                  value={newProviderName}
                  onChange={(e) => setNewProviderName(e.target.value)}
                  placeholder="e.g., openai-prod, anthropic-dev"
                  className="w-full px-4 py-2.5 rounded-lg border text-sm font-mono"
                  style={{ 
                    background: 'var(--bg-primary)', 
                    borderColor: 'var(--border-color)', 
                    color: 'var(--text-primary)' 
                  }}
                  autoFocus
                />
                <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                  Unique identifier for this provider instance
                </p>
              </div>

              {/* Provider Type */}
              <div>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                  Provider Type
                </label>
                <div className="relative">
                  <select
                    value={newProviderType}
                    onChange={(e) => setNewProviderType(e.target.value)}
                    className="w-full px-4 py-2.5 rounded-lg border text-sm appearance-none cursor-pointer"
                    style={{ 
                      background: 'var(--bg-primary)', 
                      borderColor: 'var(--border-color)', 
                      color: 'var(--text-primary)' 
                    }}
                  >
                    {providerTypeOptions.map(opt => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                  <ChevronRight 
                    size={16} 
                    className="absolute right-4 top-1/2 -translate-y-1/2 pointer-events-none rotate-90"
                    style={{ color: 'var(--text-muted)' }}
                  />
                </div>
              </div>

              {/* Preview of fields */}
              <div className="p-3 rounded-lg" style={{ background: 'var(--bg-primary)' }}>
                <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-muted)' }}>
                  This provider type requires:
                </div>
                <div className="space-y-1">
                  {getProviderFields(newProviderType).map(field => (
                    <div key={field.key} className="text-sm flex items-center gap-2" style={{ color: 'var(--text-secondary)' }}>
                      <Key size={14} style={{ color: 'var(--accent)' }} />
                      {field.label}
                    </div>
                  ))}
                  {getProviderFields(newProviderType).length === 0 && (
                    <div className="text-sm" style={{ color: 'var(--text-muted)' }}>
                      No required fields - just the type
                    </div>
                  )}
                </div>
              </div>

              {error && (
                <div className="flex items-center gap-2 p-3 rounded-lg text-sm" 
                  style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
                  <AlertCircle size={16} />
                  {error}
                </div>
              )}

              {/* Actions */}
              <div className="flex items-center justify-end gap-3 pt-2">
                <button
                  onClick={() => {
                    setShowAddProvider(false)
                    setNewProviderName('')
                    setNewProviderType('openai')
                    setError(null)
                  }}
                  className="px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                  style={{ 
                    color: 'var(--text-secondary)',
                    background: 'var(--bg-tertiary)',
                    border: '1px solid var(--border-color)'
                  }}
                >
                  Cancel
                </button>
                <button
                  onClick={handleAddProvider}
                  disabled={saving || !newProviderName.trim()}
                  className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50 disabled:hover:scale-100"
                  style={{ 
                    background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', 
                    color: '#fff' 
                  }}
                >
                  {saving ? (
                    <>
                      <Loader2 size={16} className="animate-spin" />
                      Creating...
                    </>
                  ) : (
                    <>
                      <Plus size={16} />
                      Add Provider
                    </>
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
