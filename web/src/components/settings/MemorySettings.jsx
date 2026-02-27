import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

// Cloud providers that need model/base_url/api_key fields
const CLOUD_PROVIDERS = ['openai', 'ollama', 'openai-compat']

// Default models per provider
const DEFAULT_MODELS = {
  '': 'sentence-transformers/all-MiniLM-L6-v2 (384-dim, ~23 MB)',
  'local': 'sentence-transformers/all-MiniLM-L6-v2 (384-dim, ~23 MB)',
  'openai': 'text-embedding-3-small',
  'ollama': 'nomic-embed-text',
  'openai-compat': 'text-embedding-3-small',
}

export default function MemorySettings({ config, onSaved }) {
  const [form, setForm] = useState({
    enabled: true,
    memory_dir: '',
    vector_dir: '',
    embedding: { provider: '', model: '', base_url: '', api_key: '' },
    chunking: { max_chars: 1600, overlap: 320 },
    search: { max_results: 6, min_score: 0.35 },
    sync: { watch: true, debounce_ms: 1500 }
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (config) {
      setForm({
        enabled: config.enabled !== false,
        memory_dir: config.memory_dir || '',
        vector_dir: config.vector_dir || '',
        embedding: {
          provider: config.embedding?.provider || '',
          model: config.embedding?.model || '',
          base_url: config.embedding?.base_url || '',
          api_key: config.embedding?.api_key || ''
        },
        chunking: {
          max_chars: config.chunking?.max_chars || 1600,
          overlap: config.chunking?.overlap || 320
        },
        search: {
          max_results: config.search?.max_results || 6,
          min_score: config.search?.min_score || 0.35
        },
        sync: {
          watch: config.sync?.watch !== false,
          debounce_ms: config.sync?.debounce_ms || 1500
        }
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('memory', form)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const isCloudProvider = CLOUD_PROVIDERS.includes(form.embedding.provider)
  const effectiveProvider = form.embedding.provider || ''
  const defaultModel = DEFAULT_MODELS[effectiveProvider] || ''

  return (
    <div className="max-w-xl space-y-6">
      {/* Master Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Enable Memory
          </label>
          <p className="text-xs mt-0.5" style={hintStyle}>
            Semantic memory and RAG system. Indexes memory files for context-aware retrieval. Default: enabled.
          </p>
        </div>
        <button
          onClick={() => setForm({ ...form, enabled: !form.enabled })}
          className="relative w-11 h-6 rounded-full transition-colors"
          style={{
            background: form.enabled ? '#a855f7' : 'var(--bg-tertiary)',
            border: `1px solid ${form.enabled ? '#a855f7' : 'var(--border-color)'}`
          }}
        >
          <span
            className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
            style={{ transform: form.enabled ? 'translateX(20px)' : 'translateX(0)' }}
          />
        </button>
      </div>

      {form.enabled && (
        <>
          {/* Directories */}
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Memory Directory
              </label>
              <input
                type="text"
                value={form.memory_dir}
                onChange={(e) => setForm({ ...form, memory_dir: e.target.value })}
                placeholder="~/.config/astonish/memory/ (default)"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                Directory containing memory markdown files. Default: ~/.config/astonish/memory/
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Vector Directory
              </label>
              <input
                type="text"
                value={form.vector_dir}
                onChange={(e) => setForm({ ...form, vector_dir: e.target.value })}
                placeholder="~/.config/astonish/memory/vectors/ (default)"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                Directory for vector index storage. Default: ~/.config/astonish/memory/vectors/
              </p>
            </div>
          </div>

          {/* Embedding */}
          <div className="pt-4 border-t" style={sectionBorderStyle}>
            <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
              Embedding Provider
            </h4>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Provider
                </label>
                <select
                  value={form.embedding.provider}
                  onChange={(e) => setForm({ ...form, embedding: { ...form.embedding, provider: e.target.value } })}
                  className={inputClass}
                  style={inputStyle}
                >
                  <option value="">Local built-in (default)</option>
                  <option value="openai">OpenAI</option>
                  <option value="ollama">Ollama (local server)</option>
                  <option value="openai-compat">OpenAI Compatible API</option>
                </select>
                <p className="text-xs mt-1" style={hintStyle}>
                  {!isCloudProvider
                    ? 'Default: Local all-MiniLM-L6-v2 model running in pure Go. Zero cost, no API calls. Downloaded automatically on first use (~23 MB).'
                    : form.embedding.provider === 'ollama'
                      ? 'Uses a locally running Ollama server for embeddings.'
                      : form.embedding.provider === 'openai'
                        ? 'Uses OpenAI API for embeddings (requires API key, incurs cost per request).'
                        : 'Uses any OpenAI-compatible embedding API (requires base URL and API key).'
                  }
                </p>
              </div>

              {/* Default model info for local provider */}
              {!isCloudProvider && (
                <div className="p-3 rounded-lg" style={{ background: 'var(--bg-primary)' }}>
                  <div className="text-xs font-medium mb-1" style={hintStyle}>Active Model</div>
                  <div className="text-sm font-mono" style={{ color: 'var(--text-primary)' }}>
                    sentence-transformers/all-MiniLM-L6-v2
                  </div>
                  <div className="text-xs mt-1" style={hintStyle}>
                    384-dimensional vectors, ~23 MB. Same model used by ChromaDB. Stored at ~/.config/astonish/models/
                  </div>
                </div>
              )}

              {/* Cloud provider fields */}
              {isCloudProvider && (
                <>
                  <div>
                    <label className="block text-sm font-medium mb-2" style={labelStyle}>
                      Model
                    </label>
                    <input
                      type="text"
                      value={form.embedding.model}
                      onChange={(e) => setForm({ ...form, embedding: { ...form.embedding, model: e.target.value } })}
                      placeholder={defaultModel ? `${defaultModel} (default)` : 'Model name'}
                      className={inputClass + ' font-mono'}
                      style={inputStyle}
                    />
                    <p className="text-xs mt-1" style={hintStyle}>
                      Leave empty to use the default model for this provider.
                    </p>
                  </div>
                  {(form.embedding.provider === 'openai-compat' || form.embedding.provider === 'ollama') && (
                    <div>
                      <label className="block text-sm font-medium mb-2" style={labelStyle}>
                        Base URL
                      </label>
                      <input
                        type="text"
                        value={form.embedding.base_url}
                        onChange={(e) => setForm({ ...form, embedding: { ...form.embedding, base_url: e.target.value } })}
                        placeholder={form.embedding.provider === 'ollama' ? 'http://localhost:11434/api (default)' : 'https://your-api-server.com/v1'}
                        className={inputClass + ' font-mono'}
                        style={inputStyle}
                      />
                      <p className="text-xs mt-1" style={hintStyle}>
                        {form.embedding.provider === 'ollama'
                          ? 'Ollama API endpoint. Default: http://localhost:11434/api'
                          : 'Base URL of the OpenAI-compatible API endpoint. Required.'
                        }
                      </p>
                    </div>
                  )}
                  {form.embedding.provider !== 'ollama' && (
                    <div>
                      <label className="block text-sm font-medium mb-2" style={labelStyle}>
                        API Key
                      </label>
                      <input
                        type="password"
                        value={form.embedding.api_key}
                        onChange={(e) => setForm({ ...form, embedding: { ...form.embedding, api_key: e.target.value } })}
                        placeholder={form.embedding.provider === 'openai' ? 'Uses main OpenAI provider key if empty' : 'API key for the embedding endpoint'}
                        className={inputClass + ' font-mono'}
                        style={inputStyle}
                      />
                      <p className="text-xs mt-1" style={hintStyle}>
                        {form.embedding.provider === 'openai'
                          ? 'Leave empty to reuse the API key from your OpenAI provider configuration.'
                          : 'API key for authenticating with the embedding endpoint.'
                        }
                      </p>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>

          {/* Chunking */}
          <div className="pt-4 border-t" style={sectionBorderStyle}>
            <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
              Chunking
            </h4>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Max Characters
                </label>
                <input
                  type="number"
                  value={form.chunking.max_chars}
                  onChange={(e) => setForm({ ...form, chunking: { ...form.chunking, max_chars: parseInt(e.target.value) || 1600 } })}
                  min="100"
                  className={inputClass}
                  style={inputStyle}
                />
                <p className="text-xs mt-1" style={hintStyle}>Characters per chunk. Default: 1600.</p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Overlap
                </label>
                <input
                  type="number"
                  value={form.chunking.overlap}
                  onChange={(e) => setForm({ ...form, chunking: { ...form.chunking, overlap: parseInt(e.target.value) || 320 } })}
                  min="0"
                  className={inputClass}
                  style={inputStyle}
                />
                <p className="text-xs mt-1" style={hintStyle}>Character overlap between chunks. Default: 320.</p>
              </div>
            </div>
          </div>

          {/* Search */}
          <div className="pt-4 border-t" style={sectionBorderStyle}>
            <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
              Search Defaults
            </h4>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Max Results
                </label>
                <input
                  type="number"
                  value={form.search.max_results}
                  onChange={(e) => setForm({ ...form, search: { ...form.search, max_results: parseInt(e.target.value) || 6 } })}
                  min="1"
                  max="50"
                  className={inputClass}
                  style={inputStyle}
                />
                <p className="text-xs mt-1" style={hintStyle}>Maximum chunks returned per search. Default: 6.</p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Min Similarity Score
                </label>
                <input
                  type="number"
                  value={form.search.min_score}
                  onChange={(e) => setForm({ ...form, search: { ...form.search, min_score: parseFloat(e.target.value) || 0.35 } })}
                  min="0"
                  max="1"
                  step="0.05"
                  className={inputClass}
                  style={inputStyle}
                />
                <p className="text-xs mt-1" style={hintStyle}>0.0 to 1.0. Higher = stricter matching. Default: 0.35.</p>
              </div>
            </div>
          </div>

          {/* Sync */}
          <div className="pt-4 border-t" style={sectionBorderStyle}>
            <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
              File Watcher
            </h4>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Watch for Changes</label>
                  <p className="text-xs mt-0.5" style={hintStyle}>Auto-reindex when memory files change. Default: enabled.</p>
                </div>
                <button
                  onClick={() => setForm({ ...form, sync: { ...form.sync, watch: !form.sync.watch } })}
                  className="relative w-11 h-6 rounded-full transition-colors"
                  style={{
                    background: form.sync.watch ? '#a855f7' : 'var(--bg-tertiary)',
                    border: `1px solid ${form.sync.watch ? '#a855f7' : 'var(--border-color)'}`
                  }}
                >
                  <span
                    className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
                    style={{ transform: form.sync.watch ? 'translateX(20px)' : 'translateX(0)' }}
                  />
                </button>
              </div>
              {form.sync.watch && (
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>
                    Debounce (ms)
                  </label>
                  <input
                    type="number"
                    value={form.sync.debounce_ms}
                    onChange={(e) => setForm({ ...form, sync: { ...form.sync, debounce_ms: parseInt(e.target.value) || 1500 } })}
                    min="100"
                    className={inputClass}
                    style={inputStyle}
                  />
                  <p className="text-xs mt-1" style={hintStyle}>Milliseconds to wait after file changes before reindexing. Default: 1500.</p>
                </div>
              )}
            </div>
          </div>
        </>
      )}

      {/* Save */}
      <div className="flex items-center gap-3">
        <button
          onClick={handleSave}
          disabled={saving}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
          style={saveButtonStyle}
        >
          <Save size={16} />
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
        {saveSuccess && (
          <span className="flex items-center gap-1 text-green-400 text-sm">
            <Check size={16} /> Saved
          </span>
        )}
        {error && (
          <span className="flex items-center gap-1 text-sm" style={{ color: 'var(--danger)' }}>
            <AlertCircle size={16} /> {error}
          </span>
        )}
      </div>
    </div>
  )
}
