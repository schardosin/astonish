import { useState } from 'react'
import { Save, Search, Loader2 } from 'lucide-react'
import ProviderModelSelector from '../ProviderModelSelector'
import { fetchProviderModels } from './settingsApi'
import type { SettingsData, WebCapableTools, StandardServer } from './settingsApi'

interface GeneralSettingsProps {
  settings: SettingsData | null
  generalForm: {
    default_provider: string
    default_model: string
    web_search_tool: string
    web_extract_tool: string
    timezone: string
  }
  setGeneralForm: (form: GeneralSettingsProps['generalForm']) => void
  webCapableTools: WebCapableTools
  standardServers: StandardServer[]
  saving: boolean
  onSave: () => void
  onSectionChange?: (section: string) => void
}

export default function GeneralSettings({
  settings,
  generalForm,
  setGeneralForm,
  webCapableTools,
  standardServers,
  saving,
  onSave,
  onSectionChange
}: GeneralSettingsProps) {
  // Model selector state
  const [showModelSelector, setShowModelSelector] = useState(false)
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [loadingModels, setLoadingModels] = useState(false)
  const [modelsError, setModelsError] = useState<string | null>(null)

  const loadModelsForProvider = async (providerId: string) => {
    if (!providerId) {
      setAvailableModels([])
      return
    }
    setLoadingModels(true)
    setModelsError(null)
    try {
      const data = await fetchProviderModels(providerId)
      setAvailableModels(data.models || [])
    } catch (err: any) {
      setModelsError(err.message)
      setAvailableModels([])
    } finally {
      setLoadingModels(false)
    }
  }

  const handleProviderChange = (providerId: string) => {
    setGeneralForm({ ...generalForm, default_provider: providerId, default_model: '' })
    setAvailableModels([])
    setModelsError(null)
  }

  return (
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

      {/* Timezone */}
      <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-primary)' }}>
        <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Timezone</h3>
        <div>
          <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
            IANA Timezone
          </label>
          <input
            type="text"
            value={generalForm.timezone}
            onChange={(e) => setGeneralForm({ ...generalForm, timezone: e.target.value })}
            placeholder="e.g. America/Sao_Paulo (leave empty for system default)"
            className="w-full px-4 py-2.5 rounded-lg border text-sm"
            style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          />
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Used for scheduling and time display. Must be a valid IANA timezone identifier.
          </p>
        </div>
      </div>

      <button
        onClick={onSave}
        disabled={saving}
        className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
        style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
      >
        <Save size={16} />
        {saving ? 'Saving...' : 'Save Changes'}
      </button>

      {/* Enhanced Model Selector */}
      <ProviderModelSelector
        isOpen={showModelSelector}
        onClose={() => setShowModelSelector(false)}
        onSelect={(modelId) => setGeneralForm({ ...generalForm, default_model: modelId })}
        currentModel={generalForm.default_model}
        provider={generalForm.default_provider}
      />
    </div>
  )
}
