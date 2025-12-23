import { useState, useEffect } from 'react'
import { Settings, Key, Server, ChevronRight, Save, Plus, Trash2, X, Check, AlertCircle, Code, LayoutGrid, Loader2, Package, Store, GitBranch, RefreshCw } from 'lucide-react'
import MCPStoreModal from './MCPStoreModal'
import FlowStorePanel from './FlowStorePanel'

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

export default function SettingsPage({ onClose, activeSection = 'general', onSectionChange, onToolsRefresh }) {
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
  
  // Model selection state
  const [availableModels, setAvailableModels] = useState([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [modelsError, setModelsError] = useState(null)

  // Web-capable tools state
  const [webCapableTools, setWebCapableTools] = useState({ webSearch: [], webExtract: [] })

  // Taps state
  const [taps, setTaps] = useState([])
  const [tapsLoading, setTapsLoading] = useState(false)
  const [tapsSuccess, setTapsSuccess] = useState(null) // Success message for refresh
  const [newTapUrl, setNewTapUrl] = useState('')
  const [newTapAlias, setNewTapAlias] = useState('')
  const [tapsError, setTapsError] = useState(null)

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


  const loadData = async () => {
    setLoading(true)
    try {
      const [settingsData, mcpData, webTools] = await Promise.all([
        fetchSettings(),
        fetchMCPConfig(),
        fetchWebCapableTools().catch(() => ({ webSearch: [], webExtract: [] }))
      ])
      setSettings(settingsData)
      setMcpConfig(mcpData)
      setWebCapableTools(webTools)
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
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleSaveProvider = async (providerName) => {
    setSaving(true)
    setSaveSuccess(false)
    try {
      await saveSettings({ providers: { [providerName]: providerForms[providerName] } })
      setSaveSuccess(true)
      setTimeout(() => setSaveSuccess(false), 2000)
      // Reload to get masked values
      loadData()
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
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
    { id: 'mcp', label: 'MCP Servers', icon: Server },
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

        <nav className="flex-1 p-2 space-y-1">
          {menuItems.map(item => {
            const Icon = item.icon
            const isActive = activeSection === item.id
            return (
              <button
                key={item.id}
                onClick={() => onSectionChange ? onSectionChange(item.id) : null}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all"
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

        {/* UI Version */}
        <div className="p-3 border-t text-xs" style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
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
        <div className="flex-1 overflow-y-auto p-6">
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
                  {settings?.providers.map(p => (
                    <option key={p.name} value={p.name}>
                      {p.display_name || p.name}
                      {!p.configured && ' (not configured)'}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                  Default Model
                </label>
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
                </div>
                {modelsError && (
                  <p className="text-xs mt-1" style={{ color: 'var(--danger)' }}>{modelsError}</p>
                )}
                <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                  Click the dropdown to load available models from the provider
                </p>
              </div>

              {/* Web Tools Section */}
              <div className="pt-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
                <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
                  AI Assist Web Tools
                </h4>
                <p className="text-xs mb-4 p-2 rounded" style={{ 
                  color: 'var(--text-muted)', 
                  background: 'rgba(168, 85, 247, 0.1)',
                  border: '1px solid rgba(168, 85, 247, 0.2)'
                }}>
                  ℹ️ Only MCP servers with <code style={{ color: 'var(--accent)' }}>websearch</code> in their name are shown below. 
                  Rename your tool following this convention (e.g., tavily-websearch) to use it with AI Assist.
                </p>
                
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
                        <option key={t.name} value={t.source}>
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
                        <option key={t.name} value={t.source}>
                          {t.source} ({t.name})
                        </option>
                      ))}
                    </select>
                    <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                      Used to extract content from URLs when user provides a link
                    </p>
                  </div>
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
              {settings?.providers.map(provider => (
                <div
                  key={provider.name}
                  className="p-4 rounded-lg border"
                  style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
                >
                  <div className="flex items-center justify-between mb-4">
                    <div className="flex items-center gap-3">
                      <span className="text-lg font-medium" style={{ color: 'var(--text-primary)' }}>
                        {provider.display_name || provider.name}
                      </span>
                      {provider.configured ? (
                        <span className="px-2 py-0.5 text-xs rounded" style={{ background: 'rgba(20,150,71,0.12)', color: '#149647' }}>Configured</span>
                      ) : (
                        <span className="px-2 py-0.5 text-xs rounded" style={{ background: 'rgba(107,114,128,0.15)', color: '#6b7280' }}>Not configured</span>
                      )}
                    </div>
                  </div>

                  <div className="space-y-3">
                    {Object.keys(provider.fields).map(field => (
                      <div key={field}>
                        <label className="block text-sm mb-1 capitalize" style={{ color: 'var(--text-muted)' }}>
                          {field.replace(/_/g, ' ')}
                        </label>
                        <input
                          type="password"
                          value={providerForms[provider.name]?.[field] || ''}
                          onChange={(e) => setProviderForms({
                            ...providerForms,
                            [provider.name]: { ...providerForms[provider.name], [field]: e.target.value }
                          })}
                          placeholder={provider.fields[field] || 'Enter value...'}
                          className="w-full px-3 py-2 rounded border text-sm font-mono"
                          style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                        />
                      </div>
                    ))}
                  </div>

                  <button
                    onClick={() => handleSaveProvider(provider.name)}
                    disabled={saving}
                    className="mt-4 flex items-center gap-2 px-3 py-1.5 rounded text-white text-sm font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                    style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                  >
                    <Save size={14} />
                    Save
                  </button>
                </div>
              ))}
            </div>
          )}

          {activeSection === 'mcp' && (
            <div className="space-y-4">
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

                  {/* Grid of MCP Server Cards */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {Object.entries(mcpServers).map(([name, server]) => {
                      const isExpanded = expandedMcpServer === name
                      const displayName = mcpServerNames[name] || name
                      const isSaving = savingServer === name
                      
                      return (
                        <div
                          key={name}
                          className={`rounded-lg border cursor-pointer transition-all ${
                            isExpanded ? 'border-purple-500 ring-1 ring-purple-500/30 md:col-span-2' : 'hover:border-purple-500/50'
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
                            <div className="flex items-start justify-between gap-3">
                              {/* Server Icon */}
                              <div 
                                className="shrink-0 w-10 h-10 rounded-lg flex items-center justify-center"
                                style={{ 
                                  background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                                  border: '1px solid rgba(168, 85, 247, 0.3)'
                                }}
                              >
                                <Server size={18} style={{ color: '#a855f7' }} />
                              </div>
                              
                              <div className="flex-1 min-w-0">
                                {/* Title */}
                                <h3 className="font-semibold text-base truncate" style={{ color: 'var(--text-primary)' }}>
                                  {displayName}
                                </h3>
                                
                                {/* Command/URL line */}
                                <div className="flex items-center gap-2 mt-1.5">
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
                              </div>
                              
                              <ChevronRight 
                                size={20} 
                                className={`transition-transform shrink-0 ${isExpanded ? 'rotate-90' : ''}`}
                                style={{ color: 'var(--text-muted)' }}
                              />
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
                <div className="space-y-4">
                  <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                    Edit the raw JSON configuration below. Changes will be synced when you save or switch back to Editor view.
                  </p>
                  {mcpSourceError && (
                    <div className="flex items-center gap-2" style={{ color: 'var(--danger)' }}>
                      <AlertCircle size={16} />
                      {mcpSourceError}
                    </div>
                  )}
                  <textarea
                    value={mcpSourceText}
                    onChange={(e) => setMcpSourceText(e.target.value)}
                    className="w-full h-96 px-4 py-3 rounded-lg border text-sm font-mono resize-y"
                    style={{
                      background: 'var(--bg-secondary)',
                      borderColor: mcpSourceError ? '#f87171' : 'var(--border-color)',
                      color: 'var(--text-primary)'
                    }}
                    spellCheck={false}
                  />
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
          if (onToolsRefresh) onToolsRefresh()
        }}
      />
    </div>
  )
}
