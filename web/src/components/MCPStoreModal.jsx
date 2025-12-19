import { useState, useEffect } from 'react'
import { X, Search, Star, ExternalLink, Download, Check, AlertCircle, Loader2, Tag, Package } from 'lucide-react'

// API functions for MCP Store
const fetchMCPStore = async (query = '') => {
  const url = query ? `/api/mcp-store?q=${encodeURIComponent(query)}` : '/api/mcp-store'
  const res = await fetch(url)
  if (!res.ok) throw new Error('Failed to fetch MCP store')
  return res.json()
}

const installMCPServer = async (mcpId, env = {}) => {
  const encodedId = encodeURIComponent(mcpId).replace(/%2F/g, '/')
  const res = await fetch(`/api/mcp-store/${encodedId}/install`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ env })
  })
  if (!res.ok) {
    // Try to get error message from response body
    const errorText = await res.text()
    throw new Error(errorText || `Failed to install MCP server (${res.status})`)
  }
  return res.json()
}

export default function MCPStoreModal({ isOpen, onClose, onInstall }) {
  const [servers, setServers] = useState([])
  const [sources, setSources] = useState([]) // Available sources for dropdown
  const [selectedSource, setSelectedSource] = useState('all') // Current source filter
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchDebounce, setSearchDebounce] = useState(null)
  const [selectedServer, setSelectedServer] = useState(null)
  const [installing, setInstalling] = useState(null)
  const [installSuccess, setInstallSuccess] = useState(null)
  const [envOverrides, setEnvOverrides] = useState({})

  useEffect(() => {
    if (isOpen) {
      // Reset filters to defaults when modal opens
      setSearchQuery('')
      setSelectedSource('all')
      loadServers()
    }
  }, [isOpen])

  useEffect(() => {
    if (searchDebounce) {
      clearTimeout(searchDebounce)
    }
    const timeout = setTimeout(() => {
      loadServers(searchQuery, selectedSource)
    }, 300)
    setSearchDebounce(timeout)
    return () => clearTimeout(timeout)
  }, [searchQuery, selectedSource])

  const loadServers = async (query = '', source = 'all') => {
    setLoading(true)
    setError(null)
    try {
      // Build URL with optional params
      const params = new URLSearchParams()
      if (query) params.set('q', query)
      if (source && source !== 'all') params.set('source', source)
      const url = params.toString() ? `/api/mcp-store?${params}` : '/api/mcp-store'
      
      const res = await fetch(url)
      if (!res.ok) throw new Error('Failed to fetch MCP store')
      const data = await res.json()
      
      setServers(data.servers || [])
      setSources(data.sources || [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const handleInstall = async (server) => {
    setInstalling(server.mcpId)
    setInstallSuccess(null)
    try {
      const envToSend = { ...envOverrides }
      await installMCPServer(server.mcpId, envToSend)
      setInstallSuccess(server.mcpId)
      setEnvOverrides({})
      if (onInstall) onInstall(server)
      setTimeout(() => setInstallSuccess(null), 3000)
    } catch (err) {
      setError(err.message)
    } finally {
      setInstalling(null)
    }
  }

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.8)' }}>
      <div 
        className="w-full max-w-5xl h-[85vh] rounded-xl border overflow-hidden flex flex-col"
        style={{ background: 'var(--bg-primary)', borderColor: 'var(--border-color)' }}
      >
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
          <div className="flex items-center gap-3">
            <Package size={24} className="text-purple-400" />
            <h2 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>MCP Store</h2>
            <span className="text-sm px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-400">
              {servers.length} servers
            </span>
          </div>
          <button
            onClick={onClose}
            className="p-2 rounded-lg hover:bg-gray-600/30 transition-colors"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={20} />
          </button>
        </div>

        {/* Search Bar and Source Filter */}
        <div className="p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <div className="flex gap-3">
            <div className="relative flex-1">
              <Search size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search MCP servers..."
                className="w-full pl-10 pr-4 py-2.5 rounded-lg border text-sm"
                style={{ 
                  background: 'var(--bg-secondary)', 
                  borderColor: 'var(--border-color)', 
                  color: 'var(--text-primary)' 
                }}
              />
            </div>
            <select
              value={selectedSource}
              onChange={(e) => setSelectedSource(e.target.value)}
              className="px-3 py-2.5 rounded-lg border text-sm"
              style={{ 
                background: 'var(--bg-secondary)', 
                borderColor: 'var(--border-color)', 
                color: 'var(--text-primary)',
                minWidth: '140px'
              }}
            >
              <option value="all">All Sources</option>
              {sources.map(source => (
                <option key={source} value={source}>{source}</option>
              ))}
            </select>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4">
          {loading && (
            <div className="flex items-center justify-center h-full">
              <Loader2 size={32} className="animate-spin text-purple-400" />
            </div>
          )}

          {error && (
            <div className="flex items-center justify-center gap-2 text-red-400 p-4">
              <AlertCircle size={20} />
              <span>{error}</span>
            </div>
          )}

          {!loading && !error && servers.length === 0 && (
            <div className="flex flex-col items-center justify-center h-full" style={{ color: 'var(--text-muted)' }}>
              <Package size={48} className="opacity-30 mb-4" />
              <p>No MCP servers found</p>
              {searchQuery && <p className="text-sm mt-1">Try a different search term</p>}
            </div>
          )}

          {!loading && !error && servers.length > 0 && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {servers.map(server => (
                <div
                  key={server.mcpId}
                  className={`p-4 rounded-lg border cursor-pointer transition-all hover:border-purple-500/50 ${
                    selectedServer?.mcpId === server.mcpId ? 'border-purple-500 ring-1 ring-purple-500/30' : ''
                  }`}
                  style={{ 
                    background: 'var(--bg-secondary)', 
                    borderColor: selectedServer?.mcpId === server.mcpId ? undefined : 'var(--border-color)' 
                  }}
                  onClick={() => setSelectedServer(selectedServer?.mcpId === server.mcpId ? null : server)}
                >
                  <div className="flex items-start justify-between mb-2">
                    <div className="flex-1 min-w-0">
                      <h3 className="font-semibold truncate" style={{ color: 'var(--text-primary)' }}>
                        {server.name}
                      </h3>
                      <div className="flex items-center gap-2 text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
                        <span>by {server.author}</span>
                        {server.githubStars > 0 && (
                          <span className="flex items-center gap-1">
                            <Star size={12} className="text-yellow-400 fill-yellow-400" />
                            {server.githubStars.toLocaleString()}
                          </span>
                        )}
                      </div>
                    </div>
                    <span className="shrink-0 ml-2 text-xs font-mono px-2 py-0.5 rounded" 
                          style={{ background: 'var(--bg-primary)', color: 'var(--text-muted)' }}>
                      {server.config?.command}
                    </span>
                  </div>

                  <p className="text-sm line-clamp-2 mb-3" style={{ color: 'var(--text-secondary)' }}>
                    {server.description}
                  </p>

                  {/* Tags */}
                  {server.tags && server.tags.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-3">
                      {server.tags.slice(0, 4).map(tag => (
                        <span 
                          key={tag} 
                          className="px-2 py-0.5 text-xs rounded-full"
                          style={{ background: 'var(--bg-primary)', color: 'var(--text-muted)' }}
                        >
                          {tag}
                        </span>
                      ))}
                      {server.tags.length > 4 && (
                        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                          +{server.tags.length - 4}
                        </span>
                      )}
                    </div>
                  )}

                  {/* Expanded details */}
                  {selectedServer?.mcpId === server.mcpId && (
                    <div className="mt-4 pt-4 border-t" style={{ borderColor: 'var(--border-color)' }}>
                      {/* Config preview */}
                      {server.config && (
                        <div className="mb-4">
                          <h4 className="text-xs font-medium mb-2" style={{ color: 'var(--text-muted)' }}>
                            Configuration
                          </h4>
                          <pre 
                            className="p-3 rounded text-xs overflow-x-auto"
                            style={{ background: 'var(--bg-primary)', color: 'var(--text-secondary)' }}
                          >
                            {JSON.stringify(server.config, null, 2)}
                          </pre>
                        </div>
                      )}

                      {/* Environment variable inputs */}
                      {server.config?.env && Object.keys(server.config.env).length > 0 && (
                        <div className="mb-4">
                          <h4 className="text-xs font-medium mb-2" style={{ color: 'var(--text-muted)' }}>
                            Environment Variables
                          </h4>
                          <div className="space-y-2">
                            {Object.entries(server.config.env).map(([key, defaultValue]) => (
                              <div key={key}>
                                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>
                                  {key}
                                </label>
                                <input
                                  type="text"
                                  value={envOverrides[key] ?? defaultValue}
                                  onChange={(e) => setEnvOverrides({ ...envOverrides, [key]: e.target.value })}
                                  placeholder={defaultValue || 'Enter value...'}
                                  onClick={(e) => e.stopPropagation()}
                                  className="w-full px-3 py-1.5 rounded border text-xs font-mono"
                                  style={{ 
                                    background: 'var(--bg-primary)', 
                                    borderColor: 'var(--border-color)', 
                                    color: 'var(--text-primary)' 
                                  }}
                                />
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Actions */}
                      <div className="flex items-center gap-2">
                        <button
                          onClick={(e) => {
                            e.stopPropagation()
                            handleInstall(server)
                          }}
                          disabled={installing === server.mcpId}
                          className="flex items-center gap-2 px-4 py-2 rounded-lg bg-purple-600 hover:bg-purple-500 text-white text-sm font-medium transition-colors disabled:opacity-50"
                        >
                          {installing === server.mcpId ? (
                            <>
                              <Loader2 size={14} className="animate-spin" />
                              Installing...
                            </>
                          ) : installSuccess === server.mcpId ? (
                            <>
                              <Check size={14} />
                              Installed!
                            </>
                          ) : (
                            <>
                              <Download size={14} />
                              Install
                            </>
                          )}
                        </button>
                        
                        <a
                          href={server.githubUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="flex items-center gap-2 px-3 py-2 rounded-lg hover:bg-gray-600/20 text-sm transition-colors"
                          style={{ color: 'var(--text-secondary)' }}
                        >
                          <ExternalLink size={14} />
                          Docs
                        </a>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
