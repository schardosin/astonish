import { useState, useEffect, useRef } from 'react'
import { Sparkles, ChevronRight, ChevronLeft, Check, Loader2, Key, Zap, AlertCircle, Plus, Folder, Search, Globe, Monitor, Shield, ShieldAlert, ExternalLink } from 'lucide-react'
import { fetchStandardServers, installStandardServer } from '../api/agents'
import { fetchSandboxStatus, fetchOptionalTools, initSandbox } from '../api/sandbox'

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
  { id: 'openai_compat', name: 'OpenAI Compatible', description: 'Connect to any OpenAI-compatible API endpoint', icon: '🔄', fields: [{ key: 'api_key', label: 'API Key', placeholder: 'sk-...' }, { key: 'base_url', label: 'Base URL', placeholder: 'https://api.example.com/v1' }], defaultModel: 'gpt-4o' },
  { id: 'sap_ai_core', name: 'SAP AI Core', description: 'Enterprise AI from SAP Business AI', icon: '🏢', fields: [{ key: 'auth_url', label: 'Auth URL', placeholder: 'https://your-tenant.authentication.sap.hana.ondemand.com' }, { key: 'client_id', label: 'Client ID', placeholder: 'sb-xxx' }, { key: 'client_secret', label: 'Client Secret', placeholder: 'Your client secret' }, { key: 'base_url', label: 'Base URL', placeholder: 'https://api.ai.prod.region.aws.ml.hana.ondemand.com/v2' }, { key: 'resource_group', label: 'Resource Group', placeholder: 'default' }], defaultModel: 'gpt-4o' }
]

const BROWSER_ENGINES = [
  { id: 'default', name: 'Default Chromium', description: 'Auto-downloaded by Astonish. No setup needed.', recommended: true },
  { id: 'cloakbrowser', name: 'CloakBrowser', description: 'Anti-detect Chromium with C++ stealth patches. Install via CLI.' },
  { id: 'custom', name: 'Custom Chrome', description: 'Use your own Chrome/Chromium binary.' },
  { id: 'remote', name: 'Remote Browser', description: 'Connect to Chrome running on another machine via CDP.' },
]

const TOTAL_STEPS = 9

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

const replaceAllProviders = async (providers) => {
  const res = await fetch('/api/settings/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ providers: { '__replace_all__': { '__array__': JSON.stringify(providers) } } })
  })
  if (!res.ok) throw new Error('Failed to replace providers')
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

  // Step 5: Web Search state
  const [standardServers, setStandardServers] = useState([])
  const [selectedWebServer, setSelectedWebServer] = useState(null)
  const [webApiKey, setWebApiKey] = useState('')
  const [webInstalled, setWebInstalled] = useState(false)
  const [webInstalledName, setWebInstalledName] = useState('')
  const [webSkipped, setWebSkipped] = useState(false)

  // Step 6: Browser Engine state
  const [browserEngine, setBrowserEngine] = useState('default')
  const [browserCustomPath, setBrowserCustomPath] = useState('')
  const [browserRemoteHost, setBrowserRemoteHost] = useState('')
  const [browserRemotePort, setBrowserRemotePort] = useState('9222')
  const [browserSaved, setBrowserSaved] = useState(false)

  // Step 7: Sandbox state
  const [sandboxStatus, setSandboxStatus] = useState(null)
  const [optionalTools, setOptionalTools] = useState([])
  const [selectedTools, setSelectedTools] = useState({})
  const [sandboxInitializing, setSandboxInitializing] = useState(false)
  const [sandboxProgress, setSandboxProgress] = useState([])
  const [sandboxDone, setSandboxDone] = useState(false)
  const [sandboxSkipped, setSandboxSkipped] = useState(false)
  const [sandboxSkipConfirm, setSandboxSkipConfirm] = useState(false)
  const sandboxAbortRef = useRef(null)
  const progressEndRef = useRef(null)

  useEffect(() => { loadSettings() }, [])

  // Auto-scroll sandbox progress log
  useEffect(() => {
    if (progressEndRef.current) {
      progressEndRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [sandboxProgress])

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

  const saveProviderInstance = async () => {
    const provider = getProviderInfo(selectedProvider)
    let configToSave = { ...credentials, type: selectedProvider }
    if (provider.id === 'sap_ai_core' && !configToSave.resource_group) configToSave.resource_group = 'default'

    const currentProviders = (settings?.providers || []).map(p => {
      const prov = { name: p.name, type: p.type }
      if (p.fields) {
        for (const [key, val] of Object.entries(p.fields)) {
          prov[key] = val
        }
      }
      return prov
    })

    const updatedProviders = currentProviders.filter(p => p.name !== '__new_provider__' && p.name !== instanceName)
    updatedProviders.push({ name: instanceName, ...configToSave })

    await replaceAllProviders(updatedProviders)
    setSelectedInstance(instanceName)
  }

  const handleSaveProvider = async () => {
    setIsLoading(true)
    setError(null)
    try {
      await saveProviderInstance()
      setStep(3)
    } catch (err) { setError(err.message || 'Failed to save provider') }
    finally { setIsLoading(false) }
  }

  // Step 5: Load standard servers on entering web search step
  const loadStandardServers = async () => {
    setIsLoading(true)
    try {
      const data = await fetchStandardServers()
      setStandardServers(data.servers || [])
      // Check if any is already installed
      const installed = (data.servers || []).find(s => s.installed)
      if (installed) {
        setWebInstalled(true)
        setWebInstalledName(installed.displayName)
      }
    } catch (err) {
      console.error('Failed to load standard servers:', err)
    } finally {
      setIsLoading(false)
    }
  }

  const handleInstallWebServer = async () => {
    if (!selectedWebServer) return
    setIsLoading(true)
    setError(null)
    try {
      const srv = standardServers.find(s => s.id === selectedWebServer)
      const envMap = {}
      if (srv.envVars?.length > 0 && webApiKey) {
        envMap[srv.envVars[0].name] = webApiKey
      }
      const result = await installStandardServer(selectedWebServer, envMap)
      setWebInstalled(true)
      setWebInstalledName(srv.displayName)
      // Auto-set as default web tools
      if (result.webSearchTool) {
        await fetch('/api/settings/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ general: { web_search_tool: result.webSearchTool, web_extract_tool: result.webExtractTool || '' } })
        })
      }
    } catch (err) {
      setError(err.message || 'Failed to install web search provider')
    } finally {
      setIsLoading(false)
    }
  }

  // Step 6: Save browser config
  const handleSaveBrowser = async () => {
    setIsLoading(true)
    setError(null)
    try {
      const browserConfig = {}
      if (browserEngine === 'default') {
        browserConfig.chrome_path = ''
        browserConfig.remote_cdp_url = ''
        browserConfig.fingerprint_seed = ''
        browserConfig.fingerprint_platform = ''
      } else if (browserEngine === 'custom') {
        browserConfig.chrome_path = browserCustomPath
        browserConfig.remote_cdp_url = ''
        browserConfig.fingerprint_seed = ''
        browserConfig.fingerprint_platform = ''
      } else if (browserEngine === 'remote') {
        const port = browserRemotePort || '9222'
        browserConfig.remote_cdp_url = `ws://${browserRemoteHost}:${port}`
        browserConfig.chrome_path = ''
        browserConfig.fingerprint_seed = ''
        browserConfig.fingerprint_platform = ''
      }
      // CloakBrowser is configured via CLI only — no changes needed

      if (browserEngine !== 'cloakbrowser') {
        const res = await fetch('/api/settings/full', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ browser: browserConfig })
        })
        if (!res.ok) throw new Error('Failed to save browser config')
      }
      setBrowserSaved(true)
    } catch (err) {
      setError(err.message || 'Failed to save browser settings')
    } finally {
      setIsLoading(false)
    }
  }

  // Step 7: Load sandbox status on entering sandbox step
  const loadSandboxStatus = async () => {
    setIsLoading(true)
    try {
      const status = await fetchSandboxStatus()
      setSandboxStatus(status)
      if (status.incusAvailable && !status.baseTemplateExists) {
        const toolsData = await fetchOptionalTools()
        setOptionalTools(toolsData.tools || [])
        // Pre-select recommended tools
        const defaults = {}
        for (const t of (toolsData.tools || [])) {
          if (t.recommended) defaults[t.id] = true
        }
        setSelectedTools(defaults)
      }
    } catch (err) {
      console.error('Failed to load sandbox status:', err)
    } finally {
      setIsLoading(false)
    }
  }

  const handleSandboxInit = () => {
    setSandboxInitializing(true)
    setSandboxProgress([])
    setSandboxDone(false)
    setError(null)

    const { abort } = initSandbox({
      installTools: selectedTools,
      onProgress: (msg) => {
        setSandboxProgress(prev => [...prev, msg])
      },
      onDone: () => {
        setSandboxDone(true)
        setSandboxInitializing(false)
      },
      onError: (msg) => {
        setError(msg)
        setSandboxInitializing(false)
      },
    })
    sandboxAbortRef.current = abort
  }

  const canProceed = () => {
    switch (step) {
      case 0: return true
      case 1: return selectedProvider !== null
      case 2: return Object.values(credentials).some(v => v)
      case 3: return instanceName.trim() !== ''
      case 4: return selectedModel !== ''
      case 5: return webInstalled || webSkipped
      case 6: return browserSaved || browserEngine === 'default'
      case 7: return sandboxDone || sandboxSkipped || sandboxStatus?.baseTemplateExists
      default: return true
    }
  }

  const goNext = async () => {
    if (!canProceed()) return
    if (step === 3) {
      try {
        await saveProviderInstance()
      } catch (err) {
        setError(err.message || 'Failed to save provider')
        return
      }
    }
    const nextStep = step + 1
    setStep(nextStep)
    setError(null)

    // Trigger data loads for upcoming steps
    if (nextStep === 5) loadStandardServers()
    if (nextStep === 7) loadSandboxStatus()
  }
  const goBack = () => { if (step > 0) { setStep(step - 1); setError(null) } }

  const StepIndicator = () => (
    <div className="flex items-center justify-center gap-2 mb-8">
      {Array.from({ length: TOTAL_STEPS }, (_, i) => (
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
            <p className="text-lg mb-6 max-w-md mx-auto" style={{ color: 'var(--text-muted)' }}>Build powerful AI agents visually. Let's get you set up in just a few steps.</p>
            <div className="flex flex-col gap-3 max-w-sm mx-auto text-left p-4 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">1</div><span style={{ color: 'var(--text-secondary)' }}>Connect an AI provider</span></div>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">2</div><span style={{ color: 'var(--text-secondary)' }}>Configure web search</span></div>
              <div className="flex items-center gap-3"><div className="w-6 h-6 rounded-full bg-purple-500/20 flex items-center justify-center text-purple-400 text-sm">3</div><span style={{ color: 'var(--text-secondary)' }}>Set up browser & sandbox</span></div>
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
      case 2: {
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
      }
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

      // Step 5: Web Search
      case 5:
        return (
          <div className="max-w-2xl mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Web Search Tools</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Enable web search so your AI can find information online.</p>

            {webInstalled ? (
              <div className="text-center">
                <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-green-500/10 mb-4"><Check size={32} className="text-green-400" /></div>
                <p className="text-lg font-medium mb-2" style={{ color: 'var(--text-primary)' }}>{webInstalledName} configured</p>
                <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Web search and content extraction are ready.</p>
              </div>
            ) : isLoading && standardServers.length === 0 ? (
              <div className="text-center py-8"><Loader2 size={24} className="animate-spin mx-auto mb-2 text-purple-400" /><p style={{ color: 'var(--text-muted)' }}>Loading providers...</p></div>
            ) : (
              <>
                <div className="grid grid-cols-3 gap-3 mb-6">
                  {standardServers.map(srv => (
                    <button
                      key={srv.id}
                      onClick={() => { setSelectedWebServer(srv.id); setWebApiKey(''); setError(null) }}
                      className={`p-4 rounded-xl border-2 text-left transition-all hover:scale-[1.02] ${selectedWebServer === srv.id ? 'border-purple-500 bg-purple-500/10' : srv.installed ? 'border-green-500/50' : 'border-transparent hover:border-gray-600'}`}
                      style={{ background: selectedWebServer === srv.id ? undefined : 'var(--bg-tertiary)' }}
                    >
                      <div className="flex items-center gap-2 mb-2">
                        <Search size={18} className="text-purple-400" />
                        <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>{srv.displayName}</span>
                        {srv.isDefault && !srv.installed && <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-purple-500/20 text-purple-400">recommended</span>}
                        {srv.installed && <Check size={14} className="text-green-400" />}
                      </div>
                      <p className="text-xs mb-1" style={{ color: 'var(--text-muted)' }}>{srv.description?.slice(0, 80)}</p>
                      <p className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                        {srv.capabilities?.webSearch && srv.capabilities?.webExtract ? 'Search + Extract' : 'Search only'}
                      </p>
                    </button>
                  ))}
                </div>

                {selectedWebServer && (() => {
                  const srv = standardServers.find(s => s.id === selectedWebServer)
                  if (!srv) return null
                  const needsKey = srv.envVars?.length > 0
                  return (
                    <div className="p-4 rounded-lg mb-4" style={{ background: 'var(--bg-tertiary)' }}>
                      {needsKey ? (
                        <>
                          <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>{srv.envVars[0].name}</label>
                          <div className="flex gap-3">
                            <input
                              type="password"
                              value={webApiKey}
                              onChange={e => setWebApiKey(e.target.value)}
                              placeholder={srv.envVars[0].description || 'Enter API key'}
                              className="flex-1 px-4 py-2.5 rounded-lg border text-sm focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                              style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                            />
                            <button
                              onClick={handleInstallWebServer}
                              disabled={isLoading || !webApiKey}
                              className="px-4 py-2 rounded-lg font-medium text-sm transition-all disabled:opacity-50"
                              style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}
                            >
                              {isLoading ? <Loader2 size={16} className="animate-spin" /> : 'Install'}
                            </button>
                          </div>
                        </>
                      ) : (
                        <div className="flex items-center justify-between">
                          <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>No API key required</span>
                          <button
                            onClick={handleInstallWebServer}
                            disabled={isLoading}
                            className="px-4 py-2 rounded-lg font-medium text-sm transition-all disabled:opacity-50"
                            style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}
                          >
                            {isLoading ? <Loader2 size={16} className="animate-spin" /> : 'Install'}
                          </button>
                        </div>
                      )}
                    </div>
                  )
                })()}

                {error && <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2 mb-4"><AlertCircle size={18} className="text-red-400" /><span className="text-sm text-red-400">{error}</span></div>}

                {!webSkipped && (
                  <button onClick={() => setWebSkipped(true)} className="w-full py-2 text-sm transition-all" style={{ color: 'var(--text-muted)' }}>
                    Skip for now
                  </button>
                )}
              </>
            )}
          </div>
        )

      // Step 6: Browser Engine
      case 6:
        return (
          <div className="max-w-2xl mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Browser Engine</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Choose which browser to use for web automation and content extraction.</p>

            <div className="grid grid-cols-2 gap-3 mb-6">
              {BROWSER_ENGINES.map(eng => (
                <button
                  key={eng.id}
                  onClick={() => { setBrowserEngine(eng.id); setBrowserSaved(false); setError(null) }}
                  className={`p-4 rounded-xl border-2 text-left transition-all hover:scale-[1.02] ${browserEngine === eng.id ? 'border-purple-500 bg-purple-500/10' : 'border-transparent hover:border-gray-600'}`}
                  style={{ background: browserEngine === eng.id ? undefined : 'var(--bg-tertiary)' }}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <Monitor size={18} className="text-purple-400" />
                    <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>{eng.name}</span>
                    {eng.recommended && <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-purple-500/20 text-purple-400">recommended</span>}
                  </div>
                  <p className="text-xs" style={{ color: 'var(--text-muted)' }}>{eng.description}</p>
                </button>
              ))}
            </div>

            {/* Engine-specific configuration */}
            {browserEngine === 'custom' && (
              <div className="p-4 rounded-lg mb-4" style={{ background: 'var(--bg-tertiary)' }}>
                <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>Chrome/Chromium binary path</label>
                <input
                  type="text"
                  value={browserCustomPath}
                  onChange={e => { setBrowserCustomPath(e.target.value); setBrowserSaved(false) }}
                  placeholder="/usr/bin/google-chrome"
                  className="w-full px-4 py-2.5 rounded-lg border text-sm font-mono focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                  style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                />
              </div>
            )}

            {browserEngine === 'remote' && (
              <div className="p-4 rounded-lg mb-4 space-y-3" style={{ background: 'var(--bg-tertiary)' }}>
                <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Launch Chrome with --remote-debugging-port=9222, then enter its address here.</p>
                <div className="grid grid-cols-3 gap-3">
                  <div className="col-span-2">
                    <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Host / IP</label>
                    <input
                      type="text"
                      value={browserRemoteHost}
                      onChange={e => { setBrowserRemoteHost(e.target.value); setBrowserSaved(false) }}
                      placeholder="192.168.1.100"
                      className="w-full px-4 py-2.5 rounded-lg border text-sm font-mono focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Port</label>
                    <input
                      type="text"
                      value={browserRemotePort}
                      onChange={e => { setBrowserRemotePort(e.target.value); setBrowserSaved(false) }}
                      placeholder="9222"
                      className="w-full px-4 py-2.5 rounded-lg border text-sm font-mono focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                    />
                  </div>
                </div>
              </div>
            )}

            {browserEngine === 'cloakbrowser' && (
              <div className="p-4 rounded-lg mb-4 bg-purple-500/5 border border-purple-500/20">
                <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                  CloakBrowser requires dependency installation via the CLI.
                  Run <code className="text-purple-400">astonish config browser</code> to set it up.
                </p>
              </div>
            )}

            {error && <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2 mb-4"><AlertCircle size={18} className="text-red-400" /><span className="text-sm text-red-400">{error}</span></div>}

            {browserEngine !== 'default' && browserEngine !== 'cloakbrowser' && (
              <button
                onClick={handleSaveBrowser}
                disabled={isLoading || (browserEngine === 'custom' && !browserCustomPath) || (browserEngine === 'remote' && !browserRemoteHost)}
                className="w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 transition-all disabled:opacity-50"
                style={{ background: browserSaved ? 'var(--bg-tertiary)' : 'linear-gradient(to right, #9333ea, #3b82f6)', color: browserSaved ? 'var(--text-secondary)' : 'white' }}
              >
                {isLoading ? <><Loader2 size={18} className="animate-spin" />Saving...</> : browserSaved ? <><Check size={18} />Saved</> : <><Check size={18} />Save Browser Config</>}
              </button>
            )}
          </div>
        )

      // Step 7: Sandbox
      case 7:
        return (
          <div className="max-w-2xl mx-auto">
            <h2 className="text-2xl font-bold mb-2 text-center" style={{ color: 'var(--text-primary)' }}>Sandbox</h2>
            <p className="text-center mb-6" style={{ color: 'var(--text-muted)' }}>Container isolation for AI tool execution. Prevents tools from accessing your host system directly.</p>

            {isLoading && !sandboxStatus ? (
              <div className="text-center py-8"><Loader2 size={24} className="animate-spin mx-auto mb-2 text-purple-400" /><p style={{ color: 'var(--text-muted)' }}>Detecting sandbox environment...</p></div>
            ) : sandboxStatus?.baseTemplateExists ? (
              <div className="text-center">
                <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-green-500/10 mb-4"><Shield size={32} className="text-green-400" /></div>
                <p className="text-lg font-medium mb-2" style={{ color: 'var(--text-primary)' }}>Sandbox already configured</p>
                <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Base template exists. AI tools will run inside isolated containers.</p>
              </div>
            ) : sandboxStatus?.incusAvailable ? (
              <>
                {/* Tool selection */}
                {!sandboxInitializing && !sandboxDone && (
                  <>
                    <p className="text-sm font-medium mb-3" style={{ color: 'var(--text-secondary)' }}>Optional tools to install in the sandbox:</p>
                    <div className="space-y-3 mb-6">
                      {optionalTools.map(tool => (
                        <button
                          key={tool.id}
                          onClick={() => setSelectedTools(prev => ({ ...prev, [tool.id]: !prev[tool.id] }))}
                          className={`w-full p-4 rounded-xl border-2 text-left transition-all ${selectedTools[tool.id] ? 'border-purple-500 bg-purple-500/10' : 'border-transparent hover:border-gray-600'}`}
                          style={{ background: selectedTools[tool.id] ? undefined : 'var(--bg-tertiary)' }}
                        >
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-3">
                              <div className={`w-5 h-5 rounded border-2 flex items-center justify-center transition-all ${selectedTools[tool.id] ? 'bg-purple-500 border-purple-500' : 'border-gray-500'}`}>
                                {selectedTools[tool.id] && <Check size={12} className="text-white" />}
                              </div>
                              <div>
                                <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>{tool.name}</span>
                                {tool.recommended && <span className="ml-2 text-[10px] px-1.5 py-0.5 rounded-full bg-purple-500/20 text-purple-400">recommended</span>}
                                {tool.requiresNesting && <span className="ml-2 text-[10px] px-1.5 py-0.5 rounded-full bg-yellow-500/20 text-yellow-400">needs nesting</span>}
                              </div>
                            </div>
                            {tool.url && (
                              <a href={tool.url} target="_blank" rel="noopener noreferrer" onClick={e => e.stopPropagation()} className="text-purple-400 hover:text-purple-300">
                                <ExternalLink size={14} />
                              </a>
                            )}
                          </div>
                          <p className="text-xs mt-1 ml-8" style={{ color: 'var(--text-muted)' }}>{tool.description}</p>
                        </button>
                      ))}
                    </div>

                    <button
                      onClick={handleSandboxInit}
                      className="w-full py-3 rounded-lg font-medium flex items-center justify-center gap-2 transition-all"
                      style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}
                    >
                      <Shield size={18} />Initialize Sandbox
                    </button>
                  </>
                )}

                {/* Progress display */}
                {(sandboxInitializing || sandboxDone) && (
                  <div>
                    {sandboxDone ? (
                      <div className="text-center mb-4">
                        <div className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-green-500/10 mb-3"><Check size={24} className="text-green-400" /></div>
                        <p className="font-medium" style={{ color: 'var(--text-primary)' }}>Sandbox initialized</p>
                      </div>
                    ) : (
                      <div className="flex items-center gap-2 mb-4">
                        <Loader2 size={18} className="animate-spin text-purple-400" />
                        <span className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>Initializing sandbox...</span>
                      </div>
                    )}
                    <div className="p-3 rounded-lg max-h-48 overflow-y-auto font-mono text-xs" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
                      {sandboxProgress.map((msg, i) => (
                        <div key={i} className="py-0.5">{msg}</div>
                      ))}
                      <div ref={progressEndRef} />
                    </div>
                  </div>
                )}

                {error && !sandboxInitializing && (
                  <div className="mt-4">
                    <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/30 flex items-center gap-2 mb-3"><AlertCircle size={18} className="text-red-400" /><span className="text-sm text-red-400">{error}</span></div>
                    <button
                      onClick={handleSandboxInit}
                      className="w-full py-2 rounded-lg font-medium text-sm transition-all"
                      style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}
                    >
                      Retry
                    </button>
                  </div>
                )}
              </>
            ) : (
              // Incus not available
              <div>
                <div className="p-4 rounded-lg mb-4 bg-yellow-500/5 border border-yellow-500/20">
                  <div className="flex items-start gap-3">
                    <ShieldAlert size={20} className="text-yellow-400 mt-0.5 shrink-0" />
                    <div>
                      <p className="font-medium text-sm mb-2" style={{ color: 'var(--text-primary)' }}>Incus not available</p>
                      {sandboxStatus?.reason && <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>{sandboxStatus.reason}</p>}
                      <div className="text-xs space-y-1" style={{ color: 'var(--text-secondary)' }}>
                        <p>To install Incus (Ubuntu/Debian):</p>
                        <code className="block p-2 rounded text-xs font-mono" style={{ background: 'var(--bg-secondary)' }}>sudo apt install incus && sudo incus admin init</code>
                      </div>
                    </div>
                  </div>
                </div>

                <div className="flex gap-3">
                  <button
                    onClick={loadSandboxStatus}
                    className="flex-1 py-2.5 rounded-lg font-medium text-sm flex items-center justify-center gap-2 transition-all"
                    style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}
                  >
                    {isLoading ? <Loader2 size={16} className="animate-spin" /> : null}
                    I've installed Incus — Retry
                  </button>
                  <button
                    onClick={() => setSandboxSkipConfirm(true)}
                    className="px-4 py-2.5 rounded-lg text-sm transition-all"
                    style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}
                  >
                    Skip
                  </button>
                </div>

                {sandboxSkipConfirm && !sandboxSkipped && (
                  <div className="mt-4 p-4 rounded-lg bg-red-500/5 border border-red-500/20">
                    <p className="text-sm font-medium mb-2" style={{ color: 'var(--text-primary)' }}>Continue without sandbox?</p>
                    <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>Without sandbox, AI tools will execute directly on your host system with full access to your files, network, and system resources.</p>
                    <div className="flex gap-3">
                      <button
                        onClick={() => { setSandboxSkipped(true); setSandboxSkipConfirm(false) }}
                        className="px-4 py-2 rounded-lg text-sm font-medium bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-all"
                      >
                        Yes, I accept the risk
                      </button>
                      <button
                        onClick={() => setSandboxSkipConfirm(false)}
                        className="px-4 py-2 rounded-lg text-sm transition-all"
                        style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
                      >
                        Go back
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        )

      // Step 8: All Set
      case 8: {
        const finalProvider = getProviderInfo(selectedProvider)
        return (
          <div className="text-center">
            <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-gradient-to-br from-green-500 to-emerald-400 mb-6"><Check size={40} className="text-white" /></div>
            <h2 className="text-2xl font-bold mb-4" style={{ color: 'var(--text-primary)' }}>You're All Set!</h2>
            <p className="text-lg mb-6" style={{ color: 'var(--text-muted)' }}>Astonish Studio is ready to use.</p>
            <div className="inline-block text-left p-4 rounded-lg space-y-3 min-w-[280px]" style={{ background: 'var(--bg-tertiary)' }}>
              <div className="flex items-center gap-3">
                <span className="text-2xl">{finalProvider.icon}</span>
                <div><div className="font-medium text-sm" style={{ color: 'var(--text-primary)' }}>{instanceName}</div><div className="text-xs" style={{ color: 'var(--text-muted)' }}>{selectedModel}</div></div>
              </div>
              <div className="border-t pt-3 space-y-2" style={{ borderColor: 'var(--border-color)' }}>
                <div className="flex items-center gap-2 text-xs">
                  <Search size={14} className={webInstalled ? 'text-green-400' : 'text-gray-500'} />
                  <span style={{ color: 'var(--text-secondary)' }}>Web Search:</span>
                  <span style={{ color: 'var(--text-muted)' }}>{webInstalled ? webInstalledName : 'Not configured'}</span>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <Monitor size={14} className="text-purple-400" />
                  <span style={{ color: 'var(--text-secondary)' }}>Browser:</span>
                  <span style={{ color: 'var(--text-muted)' }}>{BROWSER_ENGINES.find(e => e.id === browserEngine)?.name || 'Default'}</span>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <Shield size={14} className={sandboxDone || sandboxStatus?.baseTemplateExists ? 'text-green-400' : sandboxSkipped ? 'text-yellow-400' : 'text-gray-500'} />
                  <span style={{ color: 'var(--text-secondary)' }}>Sandbox:</span>
                  <span style={{ color: 'var(--text-muted)' }}>
                    {sandboxDone ? `Enabled${Object.entries(selectedTools).filter(([,v]) => v).length > 0 ? ` (${Object.entries(selectedTools).filter(([,v]) => v).map(([k]) => optionalTools.find(t => t.id === k)?.name || k).join(', ')})` : ''}` :
                     sandboxStatus?.baseTemplateExists ? 'Already configured' :
                     sandboxSkipped ? 'Skipped' : 'Not configured'}
                  </span>
                </div>
              </div>
            </div>
            <p className="text-sm mt-6" style={{ color: 'var(--text-muted)' }}>You can change all settings anytime in the Settings panel.</p>
          </div>
        )
      }
      default: return null
    }
  }

  const isLastStep = step === TOTAL_STEPS - 1

  return (
    <div className="fixed inset-0 flex items-center justify-center p-8 z-50" style={{ background: 'var(--bg-primary)' }}>
      <div className="w-full max-w-3xl p-8 rounded-2xl shadow-2xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
        <StepIndicator />
        <div className="min-h-[400px] flex flex-col justify-center">{renderStep()}</div>
        <div className="flex justify-between mt-8 pt-6" style={{ borderTop: '1px solid var(--border-color)' }}>
          <button onClick={goBack} disabled={step === 0 || sandboxInitializing} className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all disabled:opacity-30" style={{ color: 'var(--text-secondary)' }}><ChevronLeft size={18} />Back</button>
          {isLastStep ? (
            <button onClick={handleComplete} disabled={isLoading} className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all" style={{ background: 'linear-gradient(to right, #9333ea, #3b82f6)', color: 'white' }}>
              {isLoading ? <><Loader2 size={18} className="animate-spin" />Saving...</> : <><Sparkles size={18} />Get Started</>}
            </button>
          ) : (
            <button onClick={goNext} disabled={!canProceed() || sandboxInitializing} className="flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-all disabled:opacity-50" style={{ background: canProceed() && !sandboxInitializing ? 'linear-gradient(to right, #9333ea, #3b82f6)' : 'var(--bg-tertiary)', color: canProceed() && !sandboxInitializing ? 'white' : 'var(--text-muted)' }}>
              Continue<ChevronRight size={18} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
