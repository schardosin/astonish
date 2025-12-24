import { useState, useEffect } from 'react'
import { Sparkles, ChevronRight, ChevronLeft, Check, Loader2, Key, Zap, AlertCircle } from 'lucide-react'

// Provider metadata with icons and descriptions
const PROVIDERS = [
  {
    id: 'gemini',
    name: 'Google Gemini',
    description: 'Google\'s most capable AI models',
    icon: '✨',
    color: 'from-blue-500 to-cyan-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'Enter your Gemini API key' }],
    defaultModel: 'gemini-2.0-flash'
  },
  {
    id: 'openai',
    name: 'OpenAI',
    description: 'GPT-4o and other powerful models',
    icon: '🤖',
    color: 'from-green-500 to-emerald-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-...' }],
    defaultModel: 'gpt-4o'
  },
  {
    id: 'anthropic',
    name: 'Anthropic Claude',
    description: 'Claude models with strong reasoning',
    icon: '🧠',
    color: 'from-orange-500 to-amber-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-ant-...' }],
    defaultModel: 'claude-sonnet-4-20250514'
  },
  {
    id: 'ollama',
    name: 'Ollama (Local)',
    description: 'Run models locally on your machine',
    icon: '🦙',
    color: 'from-purple-500 to-pink-400',
    fields: [{ key: 'base_url', label: 'Base URL', placeholder: 'http://localhost:11434' }],
    defaultModel: 'llama3.2'
  },
  {
    id: 'groq',
    name: 'Groq',
    description: 'Ultra-fast inference',
    icon: '⚡',
    color: 'from-red-500 to-orange-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'gsk_...' }],
    defaultModel: 'llama-3.3-70b-versatile'
  },
  {
    id: 'openrouter',
    name: 'OpenRouter',
    description: 'Access multiple providers via one API',
    icon: '🔀',
    color: 'from-indigo-500 to-purple-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-or-...' }],
    defaultModel: 'google/gemini-2.0-flash-001'
  },
  {
    id: 'poe',
    name: 'Poe',
    description: 'Access multiple AI models via Poe',
    icon: '💬',
    color: 'from-cyan-500 to-blue-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'Enter your Poe API key' }],
    defaultModel: 'gpt-4o'
  },
  {
    id: 'xai',
    name: 'xAI Grok',
    description: 'Grok models from xAI',
    icon: '🚀',
    color: 'from-gray-600 to-gray-400',
    fields: [{ key: 'api_key', label: 'API Key', placeholder: 'xai-...' }],
    defaultModel: 'grok-2-latest'
  },
  {
    id: 'lm_studio',
    name: 'LM Studio',
    description: 'Local models via LM Studio',
    icon: '💻',
    color: 'from-teal-500 to-cyan-400',
    fields: [{ key: 'base_url', label: 'Base URL', placeholder: 'http://localhost:1234/v1' }],
    defaultModel: 'local-model'
  },
  {
    id: 'sap_ai_core',
    name: 'SAP AI Core',
    description: 'Enterprise AI from SAP Business AI',
    icon: '🏢',
    color: 'from-blue-600 to-blue-400',
    fields: [
      { key: 'auth_url', label: 'Auth URL', placeholder: 'https://your-tenant.authentication.sap.hana.ondemand.com' },
      { key: 'client_id', label: 'Client ID', placeholder: 'sb-xxx' },
      { key: 'client_secret', label: 'Client Secret', placeholder: 'Your client secret' },
      { key: 'base_url', label: 'Base URL', placeholder: 'https://api.ai.prod.region.aws.ml.hana.ondemand.com/v2' },
      { key: 'resource_group', label: 'Resource Group', placeholder: 'default' }
    ],
    defaultModel: 'gpt-4o'
  }
]

// API functions
const saveProviderConfig = async (providerId, config) => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ providers: { [providerId]: config } })
  })
  if (!res.ok) throw new Error('Failed to save provider config')
  return res.json()
}

const saveGeneralSettings = async (provider, model) => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ general: { default_provider: provider, default_model: model } })
  })
  if (!res.ok) throw new Error('Failed to save general settings')
  return res.json()
}

const fetchProviderModels = async (providerId) => {
  const res = await fetch(`/api/providers/${providerId}/models`)
  if (!res.ok) throw new Error('Failed to fetch models')
  return res.json()
}

export default function SetupWizard({ onComplete, theme }) {
  const [step, setStep] = useState(0) // 0: welcome, 1: provider, 2: credentials, 3: model, 4: complete
  const [selectedProvider, setSelectedProvider] = useState(null)
  const [credentials, setCredentials] = useState({})
  const [selectedModel, setSelectedModel] = useState('')
  const [availableModels, setAvailableModels] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState(null)
  const [testSuccess, setTestSuccess] = useState(false)

  const provider = PROVIDERS.find(p => p.id === selectedProvider)

  // When credentials change, reset test status
  useEffect(() => {
    setTestSuccess(false)
    setError(null)
  }, [credentials])

  const handleTestConnection = async () => {
    if (!provider) return
    setIsLoading(true)
    setError(null)
    setTestSuccess(false)

    try {
      // Prepare credentials, adding defaults where needed
      let configToSave = { ...credentials }
      
      // For SAP AI Core, default resource_group to 'default' if empty
      if (provider.id === 'sap_ai_core' && !configToSave.resource_group) {
        configToSave.resource_group = 'default'
      }
      
      // Save the provider config first
      await saveProviderConfig(provider.id, configToSave)

      // Then try to fetch models as a connection test
      const data = await fetchProviderModels(provider.id)
      
      if (data.models && data.models.length > 0) {
        setAvailableModels(data.models)
        setSelectedModel(provider.defaultModel || data.models[0])
        setTestSuccess(true)
      } else {
        throw new Error('No models returned - check your credentials')
      }
    } catch (err) {
      setError(err.message || 'Connection failed. Please check your credentials.')
    } finally {
      setIsLoading(false)
    }
  }

  const handleComplete = async () => {
    setIsLoading(true)
    setError(null)

    try {
      await saveGeneralSettings(provider.id, selectedModel)
      onComplete()
    } catch (err) {
      setError(err.message || 'Failed to save settings')
    } finally {
      setIsLoading(false)
    }
  }

  const canProceed = () => {
    switch (step) {
      case 0: return true
      case 1: return selectedProvider !== null
      case 2: return testSuccess
      case 3: return selectedModel !== ''
      default: return true
    }
  }

  const goNext = () => {
    if (canProceed() && step < 4) {
      setStep(step + 1)
      setError(null)
    }
  }

  const goBack = () => {
    if (step > 0) {
      setStep(step - 1)
      setError(null)
    }
  }

  // Step indicator
  const StepIndicator = () => (
    <div className="flex items-center justify-center gap-2 mb-8">
      {[0, 1, 2, 3, 4].map(i => (
        <div
          key={i}
          className={`w-2 h-2 rounded-full transition-all ${
            i === step
              ? 'w-8 bg-purple-500'
              : i < step
              ? 'bg-purple-400'
              : 'bg-gray-600'
          }`}
        />
      ))}
    </div>
  )

  const renderStep = () => {
    switch (step) {
      case 0:
        return (
          <div className="text-center">
            <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-gradient-to-br from-purple-500 to-blue-500 mb-6">
              <Sparkles size={40} className="text-white" />
            </div>
            <h1 className="text-3xl font-bold mb-4" style={{ color: 'var(--text-primary)' }}>
              Welcome to Astonish Studio
            </h1>
            <p className="text-lg mb-6 max-w-md mx-auto" style={{ color: 'var(--text-muted)' }}>
              Build powerful AI agents visually. Let's get you set up with an AI provider in just a few steps.
            </p>
            <div className="flex flex-col gap-3 max-w-sm mx-auto text-left p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3">
                <div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">1</div>
                <span style={{ color: 'var(--text-secondary)' }}>Choose your AI provider</span>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">2</div>
                <span style={{ color: 'var(--text-secondary)' }}>Enter your API credentials</span>
              </div>
              <div className="flex items-center gap-3">
                <div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">3</div>
                <span style={{ color: 'var(--text-secondary)' }}>Select your default model</span>
              </div>
            </div>
          </div>
        )
      case 1:
        return (
          <div>
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>
              Choose Your AI Provider
            </h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>
              Select the AI provider you'd like to use. You can add more later in Settings.
            </p>
            <div className="grid grid-cols-3 gap-3 max-w-3xl mx-auto">
              {PROVIDERS.map(p => (
                <button
                  key={p.id}
                  onClick={() => {
                    setSelectedProvider(p.id)
                    setCredentials({})
                    setTestSuccess(false)
                  }}
                  className={`p-3 rounded-xl border-2 text-left transition-all hover:scale-[1.02] ${
                    selectedProvider === p.id
                      ? 'border-purple-500 bg-purple-500/10'
                      : 'border-transparent hover:border-gray-600'
                  }`}
                  style={{ background: selectedProvider === p.id ? undefined : 'var(--bg-tertiary)' }}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-xl">{p.icon}</span>
                    <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>{p.name}</span>
                  </div>
                  <p className="text-xs" style={{ color: 'var(--text-muted)' }}>{p.description}</p>
                </button>
              ))}
            </div>
          </div>
        )
      case 2:
        return (
          <div className="max-w-md mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>
              Configure {provider?.name}
            </h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>
              Enter your credentials to connect to {provider?.name}.
            </p>
            <div className="space-y-4">
              {provider?.fields.map(field => (
                <div key={field.key}>
                  <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                    {field.label}{field.key === 'resource_group' ? ' (optional)' : ''}
                  </label>
                  <div className="relative">
                    <Key size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
                    <input
                      type={field.key.includes('secret') ? 'password' : 'text'}
                      value={credentials[field.key] || ''}
                      onChange={e => setCredentials(prev => ({ ...prev, [field.key]: e.target.value }))}
                      placeholder={field.placeholder}
                      className="w-full pl-10 pr-4 py-3 rounded-lg border focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all"
                      style={{
                        background: 'var(--bg-tertiary)',
                        borderColor: 'var(--border-color)',
                        color: 'var(--text-primary)'
                      }}
                    />
                  </div>
                </div>
              ))}
            </div>
            {error && (
              <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2">
                <AlertCircle size={18} className="text-red-400" />
                <span className="text-sm text-red-400">{error}</span>
              </div>
            )}
            {testSuccess && (
              <div className="mt-4 p-3 rounded-lg bg-green-500/10 border border-green-500/30 flex items-center gap-2">
                <Check size={18} className="text-green-400" />
                <span className="text-sm text-green-400">Connection successful! Found {availableModels.length} models.</span>
              </div>
            )}
            <button
              onClick={handleTestConnection}
              disabled={isLoading || !Object.values(credentials).some(v => v)}
              className="mt-6 w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 transition-all disabled:opacity-50"
              style={{
                background: testSuccess ? 'var(--bg-tertiary)' : 'linear-gradient(to right, #9333ea, #3b82f6)',
                color: testSuccess ? 'var(--text-secondary)' : 'white'
              }}
            >
              {isLoading ? (
                <>
                  <Loader2 size={18} className="animate-spin" />
                  Testing Connection...
                </>
              ) : testSuccess ? (
                <>
                  <Check size={18} />
                  Connection Verified
                </>
              ) : (
                <>
                  <Zap size={18} />
                  Test Connection
                </>
              )}
            </button>
          </div>
        )
      case 3:
        return (
          <div className="max-w-md mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>
              Select Default Model
            </h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>
              Choose the model to use by default. You can change this anytime.
            </p>
            <div className="space-y-2 max-h-80 overflow-y-auto">
              {availableModels.map(model => (
                <button
                  key={model}
                  onClick={() => setSelectedModel(model)}
                  className={`w-full p-3 rounded-lg text-left transition-all ${
                    selectedModel === model
                      ? 'bg-purple-500/20 border-purple-500'
                      : 'hover:bg-gray-700/50'
                  }`}
                  style={{
                    background: selectedModel === model ? undefined : 'var(--bg-tertiary)',
                    border: `2px solid ${selectedModel === model ? '#9333ea' : 'transparent'}`
                  }}
                >
                  <span style={{ color: 'var(--text-primary)' }}>{model}</span>
                </button>
              ))}
            </div>
          </div>
        )
      case 4:
        return (
          <div className="text-center">
            <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-gradient-to-br from-green-500 to-emerald-400 mb-6">
              <Check size={40} className="text-white" />
            </div>
            <h2 className="text-2xl font-bold mb-4" style={{ color: 'var(--text-primary)' }}>
              You're All Set!
            </h2>
            <p className="text-lg mb-2" style={{ color: 'var(--text-muted)' }}>
              Astonish Studio is ready to use.
            </p>
            <div className="p-4 rounded-lg inline-block mb-6" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3">
                <span className="text-2xl">{provider?.icon}</span>
                <div className="text-left">
                  <div className="font-medium" style={{ color: 'var(--text-primary)' }}>{provider?.name}</div>
                  <div className="text-sm" style={{ color: 'var(--text-muted)' }}>{selectedModel}</div>
                </div>
              </div>
            </div>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              You can add more providers and change settings anytime in the Settings panel.
            </p>
          </div>
        )
      default:
        return null
    }
  }

  return (
    <div 
      className="fixed inset-0 flex items-center justify-center p-8 z-50"
      style={{ background: 'var(--bg-primary)' }}
    >
      <div 
        className="w-full max-w-3xl p-8 rounded-2xl shadow-2xl"
        style={{ 
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)'
        }}
      >
        <StepIndicator />
        
        <div className="min-h-[400px] flex flex-col justify-center">
          {renderStep()}
        </div>

        {/* Navigation */}
        <div className="flex justify-between mt-8 pt-6" style={{ borderTop: '1px solid var(--border-color)' }}>
          <button
            onClick={goBack}
            disabled={step === 0}
            className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all disabled:opacity-30"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronLeft size={18} />
            Back
          </button>

          {step === 4 ? (
            <button
              onClick={handleComplete}
              disabled={isLoading}
              className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all"
              style={{ 
                background: 'linear-gradient(to right, #9333ea, #3b82f6)',
                color: 'white'
              }}
            >
              {isLoading ? (
                <>
                  <Loader2 size={18} className="animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  Get Started
                  <Sparkles size={18} />
                </>
              )}
            </button>
          ) : (
            <button
              onClick={goNext}
              disabled={!canProceed()}
              className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all disabled:opacity-50"
              style={{ 
                background: canProceed() ? 'linear-gradient(to right, #9333ea, #3b82f6)' : 'var(--bg-tertiary)',
                color: canProceed() ? 'white' : 'var(--text-muted)'
              }}
            >
              Continue
              <ChevronRight size={18} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
