import { useState, useEffect } from 'react'
import { Sparkles, ChevronRight, ChevronLeft, Check, Loader2, Key, Zap, AlertCircle, Plus, Folder } from 'lucide-react'

const PROVIDERS = [
  { id: 'gemini', name: 'Google Gemini', description: "Google's most capable AI models", icon: '✨', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'Enter your Gemini API key' }], defaultModel: 'gemini-2.0-flash' },
  { id: 'openai', name: 'OpenAI', description: 'GPT-4o and other powerful models', icon: '🤖', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-...' }], defaultModel: 'gpt-4o' },
  { id: 'anthropic', name: 'Anthropic Claude', description: 'Claude models with strong reasoning', icon: '🧠', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-ant-...' }], defaultModel: 'claude-sonnet-4-20250514' },
  { id: 'ollama', name: 'Ollama (Local)', description: 'Run models locally on your machine', icon: '🦙', fields: [{ key: 'base_url', label: 'Base URL', placeholder: 'http://localhost:11434' }], defaultModel: 'llama3.2' },
  { id: 'groq', name: 'Groq', description: 'Ultra-fast inference', icon: '⚡', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'gsk_...' }], defaultModel: 'llama-3.3-70b-versatile' },
  { id: 'openrouter', name: 'OpenRouter', description: 'Access multiple providers via one API', icon: '🔀', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-or-...' }], defaultModel: 'google/gemini-2.0-flash-001' },
  { id: 'poe', name: 'Poe', description: 'Access multiple AI models via Poe', icon: '💬', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'Enter your Poe API key' }], defaultModel: 'gpt-4o' },
  { id: 'xai', name: 'xAI Grok', description: 'Grok models from xAI', icon: '🚀', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'xai-...' }], defaultModel: 'grok-2-latest' },
  { id: 'lm_studio', name: 'LM Studio', description: 'Local models via LM Studio', icon: '💻', fields: [{ key: 'base_url', label: 'Base URL', placeholder: 'http://localhost:1234/v1' }], defaultModel: 'local-model' },
  { id: 'litellm', name: 'LiteLLM', description: 'Unified interface for 100+ LLM providers', icon: '🌐', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-...' }, { key: 'base_url', label: 'Base URL', placeholder: 'http://localhost:4000/v1' }], defaultModel: 'gpt-4' },
  { id: 'sap_ai_core', name: 'SAP AI Core', description: 'Enterprise AI from SAP Business AI', icon: '🏢', fields: [{ key: 'auth_url', label: 'Auth URL', placeholder: 'https://your-tenant.authentication.sap.hana.ondemand.com' }, { key: 'client_id', label: 'Client ID', placeholder: 'sb-xxx' }, { key: 'client_secret', label: 'Client Secret', placeholder: 'Your client secret' }, { key: 'base_url', label: 'Base URL', placeholder: 'https://api.ai.prod.region.aws.ml.hana.ondemand.com/v2' }, { key: 'resource_group', label: 'Resource Group', placeholder: 'default' }], defaultModel: 'gpt-4o' }
]

const fetchSettings = async () => {
  const res = await fetch('/api/settings/config')
  if (!res.ok) throw new Error('Failed to fetch settings')
  return res.json()
}

const saveProviderConfig = async (instanceName, config) => {
  const res = await fetch('/api/settings/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ providers: { [instanceName]: config } }) })
  if (!res.ok) throw new Error('Failed to save provider config')
  return res.json()
}

const saveGeneralSettings = async (provider, model) => {
  const res = await fetch('/api/settings/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ general: { default_provider: provider, default_model: model } }) })
  if (!res.ok) throw new Error('Failed to save general settings')
  return res.json()
}

const fetchProviderModels = async (providerId) => {
  const res = await fetch(`/api/providers/${providerId}/models`)
  if (!res.ok) throw new Error('Failed to fetch models')
  return res.json()
}

const isSensitiveField = (key) => {
  const sensitiveKeys = ['api_key', 'client_secret', 'token', 'password', 'secret', 'key']
  return sensitiveKeys.some(k => key.toLowerCase().includes(k))
}

const maskValue = (value) => {
  if (!value) return ''
  if (value.length <= 4) return '••••'
  return value.slice(0, 2) + '••••' + value.slice(-2)
}

export default function SetupWizard({ onComplete }) {
  const [step, setStep] = useState(0)
  const [settings, setSettings] = useState(null)
  const [selectedInstance, setSelectedInstance] = useState(null)
  const [isNewProvider, setIsNewProvider] = useState(false)
  const [selectedProvider, setSelectedProvider] = useState(null)
  const [instanceName, setInstanceName] = useState('')
  const [credentials, setCredentials] = useState({})
  const [selectedModel, setSelectedModel] = useState('')
  const [availableModels, setAvailableModels] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState(null)
  const [testSuccess, setTestSuccess] = useState(false)

  useEffect(() => { loadSettings() }, [])

  const loadSettings = async () => {
    try {
      const data = await fetchSettings()
      setSettings(data)
    } catch (err) { console.error('Failed to load settings:', err) }
  }

  const getProviderInfo = (providerId) => PROVIDERS.find(p => p.id === providerId) || { name: providerId, icon: '⚙️', fields: [] }

  const handleTestConnection = async () => {
    if (!selectedProvider) return
    setIsLoading(true)
    setError(null)
    setTestSuccess(false)
    try {
      const provider = getProviderInfo(selectedProvider)
      let configToSave = { ...credentials, type: selectedProvider }
      if (provider.id === 'sap_ai_core' && !configToSave.resource_group) configToSave.resource_group = 'default'
      const saveName = '__new_provider__'
      await saveProviderConfig(saveName, configToSave)
      const data = await fetchProviderModels(saveName)
      if (data.models && data.models.length > 0) {
        setAvailableModels(data.models)
        setSelectedModel(provider.defaultModel || data.models[0])
        setTestSuccess(true)
      } else { throw new Error('No models returned - check your credentials') }
    } catch (err) { setError(err.message || 'Connection failed. Please check your credentials.') }
    finally { setIsLoading(false) }
  }

  const handleComplete = async () => {
    setIsLoading(true)
    setError(null)
    try { await saveGeneralSettings(selectedInstance, selectedModel); onComplete() }
    catch (err) { setError(err.message || 'Failed to save settings') }
    finally { setIsLoading(false) }
  }

  const handleAddNewProvider = () => { setIsNewProvider(true); setSelectedInstance(null); setSelectedProvider(null); setInstanceName(''); setCredentials({}); setTestSuccess(false); setStep(1) }

  const handleSelectExisting = (instName) => {
    setIsNewProvider(false)
    setSelectedInstance(instName)
    const providerData = settings.providers.find(p => p.name === instName)
    const providerType = providerData?.type || instName
    setSelectedProvider(providerType)
    setInstanceName(instName)
    // Load existing credentials from settings (mask sensitive fields)
    const existingCredentials = {}
    if (providerData?.fields) {
      for (const [key, value] of Object.entries(providerData.fields)) {
        existingCredentials[key] = isSensitiveField(key) ? maskValue(value) : value
      }
    }
    setCredentials(existingCredentials)
    setTestSuccess(!!providerData?.configured)
    setStep(2)
  }

  const handleProviderTypeSelect = (providerId) => { setSelectedProvider(providerId); setCredentials({}); setTestSuccess(false) }

  const handleSaveProvider = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const provider = getProviderInfo(selectedProvider)
      let configToSave = { ...credentials, type: selectedProvider }
      if (provider.id === 'sap_ai_core' && !configToSave.resource_group) configToSave.resource_group = 'default'
      await saveProviderConfig(instanceName, configToSave)
      setSelectedInstance(instanceName)
      setStep(3)
    } catch (err) { setError(err.message || 'Failed to save provider') }
    finally { setIsLoading(false) }
  }

  const canProceed = () => {
    switch (step) {
      case 0: return true
      case 1: return selectedProvider !== null
      case 2: return Object.values(credentials).some(v => v)
      case 3: return instanceName.trim() !== ''
      case 4: return selectedModel !== ''
      default: return true
    }
  }

  const goNext = () => { if (canProceed()) { setStep(step + 1); setError(null) } }
  const goBack = () => { if (step > 0) { setStep(step - 1); setError(null) } }

  const StepIndicator = () => (
    <div className="flex items-center justify-center gap-2 mb-8">
      {[0, 1, 2, 3, 4, 5].map(i => (
        <div key={i} className={`w-2 h-2 rounded-full transition-all ${i === step ? 'w-8 bg-purple-500' : i < step ? 'bg-purple-400' : 'bg-gray-600'}`} />
      ))}
    </div>
  )

  const renderStep = () => {
    switch (step) {
      case 0:
        return (
          <div className="text-center">
            <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-gradient-to-br from-purple-500 to-blue-500 mb-6"><Sparkles size={40} className="text-white" /></div>
            <h1 className="text-3xl font-bold mb-4" style={{ color: 'var(--text-primary)' }}>Welcome to Astonish Studio</h1>
            <p className="text-lg mb-6 max-w-md mx-auto" style={{ color: 'var(--text-muted)' }}>Build powerful AI agents visually. Let's get you set up with an AI provider in just a few steps.</p>
            <div className="flex flex-col gap-3 max-w-sm mx-auto text-left p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">1</div><span style={{ color: 'var(--text-secondary)' }}>Select or add a provider</span></div>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">2</div><span style={{ color: 'var(--text-secondary)' }}>Enter your API credentials</span></div>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">3</div><span style={{ color: 'var(--text-secondary)' }}>Select your default model</span></div>
            </div>
          </div>
        )
      case 1:
        return (
          <div>
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Select Provider</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Choose an existing provider to edit or add a new one.</p>
            {settings?.providers?.length > 0 && (
              <>
                <p className="text-sm font-medium mb-3" style={{ color: 'var(--text-secondary)' }}>Existing Providers</p>
                <div className="grid grid-cols-2 gap-3 max-w-2xl mx-auto mb-6">
                  {settings.providers.map(p => { const providerInfo = getProviderInfo(p.type || p.name); return (
                    <button key={p.name} onClick={() => handleSelectExisting(p.name)} className="p-4 rounded-xl border-2 text-left transition-all hover:scale-[1.02] border-transparent hover:border-gray-600" style={{ background: 'var(--bg-tertiary)' }}>
                      <div className="flex items-center gap-3"><span className="text-2xl">{providerInfo.icon}</span><div><div className="font-semibold" style={{ color: 'var(--text-primary)' }}>{p.name}</div><div className="text-xs" style={{ color: 'var(--text-muted)' }}>{providerInfo.name}</div></div></div>
                    </button>
                  )})}
                </div>
              </>
            )}
            <div className="border-t pt-6" style={{ borderColor: 'var(--border-color)' }}>
              <p className="text-sm font-medium mb-3" style={{ color: 'var(--text-secondary)' }}>{settings?.providers?.length > 0 ? 'Or add a new provider' : 'Add Your First Provider'}</p>
              <div className="grid grid-cols-4 gap-3 max-w-2xl mx-auto">
                {PROVIDERS.map(p => (
                  <button key={p.id} onClick={() => handleProviderTypeSelect(p.id)} className={`p-3 rounded-xl border-2 text-left transition-all hover:scale-[1.02] ${selectedProvider === p.id ? 'border-purple-500 bg-purple-500/10' : 'border-transparent hover:border-gray-600'}`} style={{ background: selectedProvider === p.id ? undefined : 'var(--bg-tertiary)' }}>
                    <div className="flex items-center gap-2 mb-1"><span className="text-xl">{p.icon}</span><span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>{p.name}</span></div>
                    <p className="text-xs" style={{ color: 'var(--text-muted)' }}>{p.description}</p>
                  </button>
                ))}
              </div>
            </div>
          </div>
        )
      case 2:
        const provider = getProviderInfo(selectedProvider)
        return (
          <div className="max-w-md mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Configure {provider.name}</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Enter your credentials to connect to {provider.name}.</p>
            <div className="space-y-4">
              {provider.fields.map(field => (
                <div key={field.key}>
                  <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>{field.label}{field.key === 'resource_group' ? ' (optional)' : ''}</label>
                  <div className="relative">
                    <Key size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
                    <input type={field.key.includes('secret') || field.key === 'client_secret' ? 'password' : 'text'} value={credentials[field.key] || ''} onChange={e => setCredentials(prev => ({ ...prev, [field.key]: e.target.value }))} onFocus={(e) => { if (isSensitiveField(field.key) && credentials[field.key]?.startsWith('••')) e.target.value = '' }} placeholder={field.placeholder} className="w-full pl-10 pr-4 py-3 rounded-lg border focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all" style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }} />
                  </div>
                </div>
              ))}
            </div>
            {error && <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2"><AlertCircle size={18} className="text-red-400" /><span className="text-sm text-red-400">{error}</span></div>}
            <button onClick={handleTestConnection} disabled={isLoading || !Object.values(credentials).some(v => v)} className="mt-6 w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 transition-all disabled:opacity-50" style={{ background: testSuccess ? 'var(--bg-tertiary)' : 'linear-gradient(to right, #9333ea, #3b82f6)', color: testSuccess ? 'var(--text-secondary)' : 'white' }}>
              {isLoading ? <><Loader2 size={18} className="animate-spin" />Testing Connection...</> : testSuccess ? <><Check size={18} />Connection Verified</> : <><Zap size={18} />Test Connection</>}
            </button>
          </div>
        )
      case 3:
        return (
          <div className="max-w-md mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Provider Instance Name</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Give this provider instance a unique name.</p>
            <div className="relative">
              <Folder size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
              <input type="text" value={instanceName} onChange={e => setInstanceName(e.target.value)} placeholder="e.g., openai-prod, anthropic-dev" className="w-full pl-10 pr-4 py-3 rounded-lg border focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all" style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }} autoFocus />
            </div>
            {error && <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2"><AlertCircle size={18} className="text-red-400" /><span className="text-sm text-red-400">{error}</span></div>}
            <button onClick={handleSaveProvider} disabled={isLoading || !instanceName.trim()} className="mt-6 w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 transition-all disabled:opacity-50" style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}>
              {isLoading ? <><Loader2 size={18} className="animate-spin" />Saving...</> : <><Check size={18} />Save Provider</>}
            </button>
          </div>
        )
      case 4:
        return (
          <div className="max-w-md mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Select Default Model</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Choose the model to use by default. You can change this anytime.</p>
            <div className="space-y-2 max-h-80 overflow-y-auto">
              {availableModels.map(model => (
                <button key={model} onClick={() => setSelectedModel(model)} className={`w-full p-3 rounded-lg text-left transition-all ${selectedModel === model ? 'bg-purple-500/20 border-purple-500' : 'hover:bg-gray-700/50'}`} style={{ background: selectedModel === model ? undefined : 'var(--bg-tertiary)', border: `2px solid ${selectedModel === model ? '#9333ea' : 'transparent'}` }}>
                  <span style={{ color: 'var(--text-primary)' }}>{model}</span>
                </button>
              ))}
            </div>
          </div>
        )
      case 5:
        const finalProvider = getProviderInfo(selectedProvider)
        return (
          <div className="text-center">
            <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-gradient-to-br from-green-500 to-emerald-400 mb-6"><Check size={40} className="text-white" /></div>
            <h2 className="text-2xl font-bold mb-4" style={{ color: 'var(--text-primary)' }}>You're All Set!</h2>
            <p className="text-lg mb-2" style={{ color: 'var(--text-muted)' }}>Astonish Studio is ready to use.</p>
            <div className="p-4 rounded-lg inline-block mb-6" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3"><span className="text-2xl">{finalProvider.icon}</span><div><div className="font-medium" style={{ color: 'var(--text-primary)' }}>{instanceName}</div><div className="text-sm" style={{ color: 'var(--text-muted)' }}>{selectedModel}</div></div></div>
            </div>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>You can add more providers and change settings anytime in the Settings panel.</p>
          </div>
        )
      default: return null
    }
  }

  return (
    <div className="fixed inset-0 flex items-center justify-center p-8 z-50" style={{ background: 'var(--bg-primary)' }}>
      <div className="w-full max-w-3xl p-8 rounded-2xl shadow-2xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
        <StepIndicator />
        <div className="min-h-[400px] flex flex-col justify-center">{renderStep()}</div>
        <div className="flex justify-between mt-8 pt-6" style={{ borderTop: '1px solid var(--border-color)' }}>
          <button onClick={goBack} disabled={step === 0} className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all disabled:opacity-30" style={{ color: 'var(--text-secondary)' }}><ChevronLeft size={18} />Back</button>
          {step === 5 ? (
            <button onClick={handleComplete} disabled={isLoading} className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all" style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}>
              {isLoading ? <><Loader2 size={18} className="animate-spin" />Saving...</> : <><Sparkles size={18} />Get Started</>}
            </button>
          ) : (
            <button onClick={goNext} disabled={!canProceed()} className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all disabled:opacity-50" style={{ background: canProceed() ? 'linear-gradient(to right, #9333ea, #3b82f6)' : 'var(--bg-tertiary)', color: canProceed() ? 'white' : 'var(--text-muted)' }}>
              Continue<ChevronRight size={18} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
