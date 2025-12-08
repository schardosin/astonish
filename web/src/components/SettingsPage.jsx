import { useState, useEffect } from 'react'
import { Settings, Key, Server, ChevronRight, Save, Plus, Trash2, X, Check, AlertCircle, Code, LayoutGrid, Loader2 } from 'lucide-react'

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

export default function SettingsPage({ onClose, theme }) {
  const [activeSection, setActiveSection] = useState('general')
  const [settings, setSettings] = useState(null)
  const [mcpConfig, setMcpConfig] = useState(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)

  // Form state
  const [generalForm, setGeneralForm] = useState({ default_provider: '', default_model: '' })
  const [providerForms, setProviderForms] = useState({})
  const [mcpServers, setMcpServers] = useState({})
  const [editingMcpServer, setEditingMcpServer] = useState(null)
  const [mcpViewMode, setMcpViewMode] = useState('editor') // 'editor' or 'source'
  const [mcpSourceText, setMcpSourceText] = useState('')
  const [mcpSourceError, setMcpSourceError] = useState(null)
  
  // Model selection state
  const [availableModels, setAvailableModels] = useState([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [modelsError, setModelsError] = useState(null)

  useEffect(() => {
    loadData()
  }, [])

  const loadData = async () => {
    setLoading(true)
    try {
      const [settingsData, mcpData] = await Promise.all([
        fetchSettings(),
        fetchMCPConfig()
      ])
      setSettings(settingsData)
      setMcpConfig(mcpData)
      setGeneralForm({
        default_provider: settingsData.general.default_provider || '',
        default_model: settingsData.general.default_model || ''
      })
      // Initialize provider forms
      const pForms = {}
      settingsData.providers.forEach(p => {
        pForms[p.name] = { ...p.fields }
      })
      setProviderForms(pForms)
      setMcpServers(mcpData.mcpServers || {})
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
      await saveMCPConfig({ mcpServers })
      setSaveSuccess(true)
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
    const newName = `server_${Object.keys(mcpServers).length + 1}`
    setMcpServers({
      ...mcpServers,
      [newName]: { command: '', args: [], env: {}, transport: 'stdio' }
    })
    setEditingMcpServer(newName)
  }

  const handleDeleteMcpServer = (name) => {
    const newServers = { ...mcpServers }
    delete newServers[name]
    setMcpServers(newServers)
  }

  const menuItems = [
    { id: 'general', label: 'General', icon: Settings },
    { id: 'providers', label: 'Providers', icon: Key },
    { id: 'mcp', label: 'MCP Servers', icon: Server },
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
            className="p-1 rounded hover:bg-gray-600/30"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={20} />
          </button>
        </div>
        
        <nav className="flex-1 p-2">
          {menuItems.map(item => {
            const Icon = item.icon
            const isActive = activeSection === item.id
            return (
              <button
                key={item.id}
                onClick={() => setActiveSection(item.id)}
                className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg mb-1 transition-all ${
                  isActive ? 'bg-purple-600/20 text-purple-400' : 'hover:bg-gray-600/20'
                }`}
                style={{ color: isActive ? undefined : 'var(--text-secondary)' }}
              >
                <Icon size={18} />
                <span className="font-medium">{item.label}</span>
                {isActive && <ChevronRight size={16} className="ml-auto" />}
              </button>
            )
          })}
        </nav>
      </div>

      {/* Right Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
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
            <div className="flex items-center gap-2 text-red-400 text-sm">
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
                      <Loader2 size={16} className="animate-spin text-purple-400" />
                    </div>
                  )}
                </div>
                {modelsError && (
                  <p className="text-xs text-red-400 mt-1">{modelsError}</p>
                )}
                <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                  Click the dropdown to load available models from the provider
                </p>
              </div>

              <button
                onClick={handleSaveGeneral}
                disabled={saving}
                className="flex items-center gap-2 px-4 py-2 rounded-lg bg-purple-600 hover:bg-purple-500 text-white font-medium transition-colors disabled:opacity-50"
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
                        <span className="px-2 py-0.5 text-xs rounded bg-green-500/20 text-green-400">Configured</span>
                      ) : (
                        <span className="px-2 py-0.5 text-xs rounded bg-gray-500/20 text-gray-400">Not configured</span>
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
                    className="mt-4 flex items-center gap-2 px-3 py-1.5 rounded bg-purple-600 hover:bg-purple-500 text-white text-sm font-medium transition-colors disabled:opacity-50"
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
                        ? 'bg-purple-600 text-white' 
                        : 'hover:bg-gray-600/20'
                    }`}
                    style={{ color: mcpViewMode !== 'editor' ? 'var(--text-secondary)' : undefined }}
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
                    className={`flex items-center gap-2 px-3 py-1.5 text-sm font-medium transition-colors ${
                      mcpViewMode === 'source' 
                        ? 'bg-purple-600 text-white' 
                        : 'hover:bg-gray-600/20'
                    }`}
                    style={{ color: mcpViewMode !== 'source' ? 'var(--text-secondary)' : undefined }}
                  >
                    <Code size={14} />
                    Source
                  </button>
                </div>
              </div>

              {/* Editor View */}
              {mcpViewMode === 'editor' && (
                <>
                  <button
                    onClick={handleAddMcpServer}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg bg-purple-600 hover:bg-purple-500 text-white font-medium transition-colors"
                  >
                    <Plus size={16} />
                    Add MCP Server
                  </button>

                  {Object.entries(mcpServers).map(([name, server]) => (
                    <div
                      key={name}
                      className="p-4 rounded-lg border"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
                    >
                      <div className="flex items-center justify-between mb-4">
                        <input
                          type="text"
                          value={name}
                          onChange={(e) => {
                            const newServers = {}
                            Object.entries(mcpServers).forEach(([k, v]) => {
                              newServers[k === name ? e.target.value : k] = v
                            })
                            setMcpServers(newServers)
                          }}
                          className="text-lg font-medium bg-transparent border-none outline-none"
                          style={{ color: 'var(--text-primary)' }}
                          placeholder="Server name"
                        />
                        <button
                          onClick={() => handleDeleteMcpServer(name)}
                          className="p-1.5 text-red-400 hover:text-red-300 hover:bg-red-500/20 rounded"
                        >
                          <Trash2 size={16} />
                        </button>
                      </div>

                      <div className="grid grid-cols-2 gap-4">
                        <div>
                          <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Command</label>
                          <input
                            type="text"
                            value={server.command || ''}
                            onChange={(e) => setMcpServers({
                              ...mcpServers,
                              [name]: { ...server, command: e.target.value }
                            })}
                            placeholder="e.g., npx"
                            className="w-full px-3 py-2 rounded border text-sm font-mono"
                            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                          />
                        </div>
                        <div>
                          <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Transport</label>
                          <select
                            value={server.transport || 'stdio'}
                            onChange={(e) => setMcpServers({
                              ...mcpServers,
                              [name]: { ...server, transport: e.target.value }
                            })}
                            className="w-full px-3 py-2 rounded border text-sm"
                            style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                          >
                            <option value="stdio">stdio</option>
                            <option value="sse">sse</option>
                          </select>
                        </div>
                      </div>

                      <div className="mt-3">
                        <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Args (comma-separated)</label>
                        <input
                          type="text"
                          value={(server.args || []).join(', ')}
                          onChange={(e) => setMcpServers({
                            ...mcpServers,
                            [name]: { ...server, args: e.target.value.split(',').map(s => s.trim()).filter(Boolean) }
                          })}
                          placeholder="e.g., -y, @anthropic-ai/mcp-server-github"
                          className="w-full px-3 py-2 rounded border text-sm font-mono"
                          style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                        />
                      </div>

                      <div className="mt-3">
                        <label className="block text-sm mb-1" style={{ color: 'var(--text-muted)' }}>Environment (JSON)</label>
                        <input
                          type="text"
                          value={Object.keys(server.env || {}).length > 0 ? JSON.stringify(server.env) : ''}
                          onChange={(e) => {
                            try {
                              const env = e.target.value ? JSON.parse(e.target.value) : {}
                              setMcpServers({
                                ...mcpServers,
                                [name]: { ...server, env }
                              })
                            } catch {}
                          }}
                          placeholder='{"KEY": "value"}'
                          className="w-full px-3 py-2 rounded border text-sm font-mono"
                          style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                        />
                      </div>
                    </div>
                  ))}

                  {Object.keys(mcpServers).length > 0 && (
                    <button
                      onClick={handleSaveMCP}
                      disabled={saving}
                      className="flex items-center gap-2 px-4 py-2 rounded-lg bg-purple-600 hover:bg-purple-500 text-white font-medium transition-colors disabled:opacity-50"
                    >
                      <Save size={16} />
                      {saving ? 'Saving...' : 'Save MCP Configuration'}
                    </button>
                  )}

                  {Object.keys(mcpServers).length === 0 && (
                    <div className="text-center py-8" style={{ color: 'var(--text-muted)' }}>
                      <Server size={48} className="mx-auto mb-3 opacity-30" />
                      <p>No MCP servers configured.</p>
                      <p className="text-sm mt-1">Click "Add MCP Server" to add one.</p>
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
                    <div className="flex items-center gap-2 text-red-400 text-sm">
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
                    onClick={() => {
                      try {
                        const parsed = JSON.parse(mcpSourceText)
                        if (parsed.mcpServers && typeof parsed.mcpServers === 'object') {
                          setMcpServers(parsed.mcpServers)
                          setMcpSourceError(null)
                          handleSaveMCP()
                        } else {
                          setMcpSourceError('Invalid format: expected { "mcpServers": { ... } }')
                        }
                      } catch (e) {
                        setMcpSourceError(`Invalid JSON: ${e.message}`)
                      }
                    }}
                    disabled={saving}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg bg-purple-600 hover:bg-purple-500 text-white font-medium transition-colors disabled:opacity-50"
                  >
                    <Save size={16} />
                    {saving ? 'Saving...' : 'Apply & Save'}
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
