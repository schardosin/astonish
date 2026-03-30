import { useState } from 'react'
import { Key, ChevronRight, Save, Plus, Trash2, X, AlertCircle, Loader2 } from 'lucide-react'
import { saveSettings, replaceAllProviders } from './settingsApi'
import type { SettingsData, ProviderFieldDef } from './settingsApi'

interface ProvidersSettingsProps {
  settings: SettingsData | null
  providerForms: Record<string, Record<string, string>>
  setProviderForms: (forms: Record<string, Record<string, string>>) => void
  generalForm: {
    default_provider: string
    default_model: string
  }
  setGeneralForm: (form: any) => void
  saving: boolean
  setSaving: (saving: boolean) => void
  setSaveSuccess: (success: boolean) => void
  error: string | null
  setError: (error: string | null) => void
  loadData: () => void
  onSettingsSaved?: () => void
}

export default function ProvidersSettings({
  settings,
  providerForms,
  setProviderForms,
  generalForm,
  setGeneralForm,
  saving,
  setSaving,
  setSaveSuccess,
  error,
  setError,
  loadData,
  onSettingsSaved
}: ProvidersSettingsProps) {
  const [expandedProvider, setExpandedProvider] = useState<string | null>(null)
  const [showAddProvider, setShowAddProvider] = useState(false)
  const [newProviderName, setNewProviderName] = useState('')
  const [newProviderType, setNewProviderType] = useState('openai')
  const [deletingProvider, setDeletingProvider] = useState<string | null>(null)

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

  const getProviderFields = (type: string): ProviderFieldDef[] => {
    const fieldMappings: Record<string, ProviderFieldDef[]> = {
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

  const handleSaveProvider = async (providerName: string) => {
    setSaving(true)
    try {
      const currentProviders = (settings?.providers || []).map(p => {
        const provider: Record<string, unknown> = { name: p.name, type: p.type }
        if (p.fields) {
          for (const [key, val] of Object.entries(p.fields)) {
            provider[key] = val
          }
        }
        return provider
      })

      const existingProvider = settings?.providers?.find(p => p.name === providerName)
      const existingType = existingProvider?.type || providerName
      const newProviderConfig: Record<string, unknown> = { name: providerName, type: existingType }
      for (const [key, value] of Object.entries(providerForms[providerName] || {})) {
        if (value && key !== 'type') {
          newProviderConfig[key] = value
        }
      }

      let updatedProviders: Record<string, unknown>[]
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
    } catch (err: any) {
      setSaving(false)
      setError(err.message)
    }
  }

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
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteProvider = async (providerName: string) => {
    setDeletingProvider(providerName)
    try {
      const currentProviders = (settings?.providers || []).map(p => {
        const provider: Record<string, unknown> = { name: p.name, type: p.type }
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
    } catch (err: any) {
      setError(err.message)
    } finally {
      setDeletingProvider(null)
    }
  }

  return (
    <>
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
    </>
  )
}
