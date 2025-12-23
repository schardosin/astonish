import { useState, useEffect, useMemo } from 'react'
import { Search, X, Loader2, DollarSign, Database, Zap, Check } from 'lucide-react'

/**
 * Enhanced model selector with search for various providers
 */
export default function ProviderModelSelector({ isOpen, onClose, onSelect, currentModel, provider = 'openrouter' }) {
  const [models, setModels] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [searchQuery, setSearchQuery] = useState('')

  useEffect(() => {
    if (isOpen) {
      fetchModels()
      
      const handleKeyDown = (e) => {
        if (e.key === 'Escape') {
          onClose()
        }
      }
      window.addEventListener('keydown', handleKeyDown)
      return () => window.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, provider, onClose])

  const fetchModels = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/providers/${provider}/models-metadata`)
      if (!res.ok) throw new Error('Failed to fetch models')
      const data = await res.json()
      setModels(data.models || [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const filteredModels = useMemo(() => {
    if (!searchQuery.trim()) return models
    const query = searchQuery.toLowerCase()
    return models.filter(m => 
      m.name?.toLowerCase().includes(query) || 
      m.id?.toLowerCase().includes(query)
    )
  }, [models, searchQuery])

  const formatPrice = (price) => {
    if (!price || price === '0') return 'Free'
    const num = parseFloat(price)
    if (num === 0) return 'Free'
    // Convert to per 1M tokens for display
    const perMillion = num * 1000000
    if (perMillion < 0.01) return `$${perMillion.toFixed(4)}/M`
    if (perMillion < 1) return `$${perMillion.toFixed(3)}/M`
    return `$${perMillion.toFixed(2)}/M`
  }

  const formatContextLength = (length) => {
    if (!length) return '-'
    if (length >= 1000000) return `${(length / 1000000).toFixed(1)}M`
    if (length >= 1000) return `${(length / 1000).toFixed(0)}K`
    return length.toString()
  }

  const handleSelect = (model) => {
    onSelect(model.id)
    onClose()
  }

  if (!isOpen) return null

  return (
    <div 
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.8)' }}
      onClick={onClose}
    >
      <div 
        className="w-full max-w-3xl max-h-[80vh] rounded-xl overflow-hidden flex flex-col"
        style={{ background: 'var(--bg-primary)', border: '1px solid var(--border-color)' }}
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div 
          className="p-4 border-b flex items-center justify-between"
          style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}
        >
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            {provider === 'openrouter' ? 'Select OpenRouter Model' : 
             provider === 'anthropic' ? 'Select Anthropic Model' : 
             provider === 'gemini' ? 'Select Google AI Model' :
             provider === 'groq' ? 'Select Groq Model' :
             provider === 'openai' ? 'Select OpenAI Model' :
             provider === 'poe' ? 'Select Poe Model' :
             provider === 'sap_ai_core' ? 'Select SAP AI Core Model' :
             provider === 'xai' ? 'Select xAI Grok Model' :
             provider === 'lm_studio' ? 'Select LM Studio Model' :
             provider === 'ollama' ? 'Select Ollama (Local) Model' : 'Select Model'}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg transition-colors hover:bg-gray-600/20"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={20} />
          </button>
        </div>

        {/* Search */}
        <div className="p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <div className="relative">
            <Search 
              size={18} 
              className="absolute left-3 top-1/2 -translate-y-1/2" 
              style={{ color: 'var(--text-muted)' }} 
            />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search models..."
              className="w-full pl-10 pr-4 py-2.5 rounded-lg border text-sm"
              style={{ 
                background: 'var(--bg-secondary)', 
                borderColor: 'var(--border-color)', 
                color: 'var(--text-primary)' 
              }}
              autoFocus
            />
          </div>
          <div className="mt-2 text-xs" style={{ color: 'var(--text-muted)' }}>
            {filteredModels.length} models available
          </div>
        </div>

        {/* Models List */}
        <div className="flex-1 overflow-y-auto p-4">
          {loading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
              <span className="ml-2" style={{ color: 'var(--text-muted)' }}>Loading models...</span>
            </div>
          )}

          {error && (
            <div className="text-center py-8" style={{ color: 'var(--danger)' }}>
              {error}
            </div>
          )}

          {!loading && !error && (
            <div className="space-y-2">
              {filteredModels.map(model => {
                const isSelected = model.id === currentModel
                return (
                  <button
                    key={model.id}
                    onClick={() => handleSelect(model)}
                    className={`w-full p-3 rounded-lg border text-left transition-all hover:border-purple-500/50 ${
                      isSelected ? 'border-purple-500 ring-1 ring-purple-500/30' : ''
                    }`}
                    style={{ 
                      background: isSelected ? 'rgba(168, 85, 247, 0.1)' : 'var(--bg-secondary)',
                      borderColor: isSelected ? undefined : 'var(--border-color)'
                    }}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-medium truncate" style={{ color: 'var(--text-primary)' }}>
                            {model.name}
                          </span>
                          {isSelected && <Check size={16} style={{ color: '#a855f7' }} />}
                        </div>
                        <code 
                          className="text-xs mt-1 block truncate"
                          style={{ color: 'var(--text-muted)' }}
                        >
                          {model.id}
                        </code>
                      </div>

                      {/* Pricing & Stats */}
                      <div className="shrink-0 flex flex-col items-end gap-1 text-xs">
                        {model.pricing && (
                          <div className="flex items-center gap-3">
                            <span 
                              className="flex items-center gap-1 px-2 py-1 rounded"
                              style={{ 
                                background: 'rgba(34, 197, 94, 0.1)',
                                color: '#22c55e'
                              }}
                            >
                              <DollarSign size={12} />
                              In: {formatPrice(model.pricing.prompt)}
                            </span>
                            <span 
                              className="flex items-center gap-1 px-2 py-1 rounded"
                              style={{ 
                                background: 'rgba(59, 130, 246, 0.1)',
                                color: '#3b82f6'
                              }}
                            >
                              <DollarSign size={12} />
                              Out: {formatPrice(model.pricing.completion)}
                            </span>
                          </div>
                        )}
                        <div className="flex items-center gap-3 mt-1">
                          {model.context_length > 0 && (
                            <span 
                              className="flex items-center gap-1 px-2 py-1 rounded"
                              style={{ 
                                background: 'rgba(168, 85, 247, 0.1)',
                                color: '#a855f7'
                              }}
                            >
                              <Database size={12} />
                              {formatContextLength(model.context_length)} ctx
                            </span>
                          )}
                          {model.max_completion_tokens > 0 && (
                            <span 
                              className="flex items-center gap-1 px-2 py-1 rounded"
                              style={{ 
                                background: 'rgba(251, 191, 36, 0.1)',
                                color: '#fbbf24'
                              }}
                            >
                              <Zap size={12} />
                              {formatContextLength(model.max_completion_tokens)} out
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                  </button>
                )
              })}

              {filteredModels.length === 0 && !loading && (
                <div className="text-center py-8" style={{ color: 'var(--text-muted)' }}>
                  No models match your search
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
