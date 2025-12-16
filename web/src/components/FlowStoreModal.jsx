import { useState, useEffect } from 'react'
import { X, Search, Download, Check, AlertCircle, Loader2, Tag, Package, Plus, Trash2, RefreshCw, Store } from 'lucide-react'

// API functions for Flow Store
const fetchFlowStore = async () => {
  const res = await fetch('/api/flow-store')
  if (!res.ok) throw new Error('Failed to fetch flow store')
  return res.json()
}

const addTap = async (url, alias = '') => {
  const res = await fetch('/api/flow-store/taps', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, alias })
  })
  if (!res.ok) {
    const errorText = await res.text()
    throw new Error(errorText || 'Failed to add tap')
  }
  return res.json()
}

const removeTap = async (name) => {
  const res = await fetch(`/api/flow-store/taps/${encodeURIComponent(name)}`, {
    method: 'DELETE'
  })
  if (!res.ok) {
    const errorText = await res.text()
    throw new Error(errorText || 'Failed to remove tap')
  }
  return res.json()
}

const installFlow = async (tapName, flowName) => {
  const res = await fetch(`/api/flow-store/${encodeURIComponent(tapName)}/${encodeURIComponent(flowName)}/install`, {
    method: 'POST'
  })
  if (!res.ok) {
    const errorText = await res.text()
    throw new Error(errorText || 'Failed to install flow')
  }
  return res.json()
}

const uninstallFlow = async (tapName, flowName) => {
  const res = await fetch(`/api/flow-store/${encodeURIComponent(tapName)}/${encodeURIComponent(flowName)}`, {
    method: 'DELETE'
  })
  if (!res.ok) {
    const errorText = await res.text()
    throw new Error(errorText || 'Failed to uninstall flow')
  }
  return res.json()
}

const updateStore = async () => {
  const res = await fetch('/api/flow-store/update', { method: 'POST' })
  if (!res.ok) throw new Error('Failed to update store')
  return res.json()
}

export default function FlowStoreModal({ isOpen, onClose, onInstall }) {
  const [taps, setTaps] = useState([])
  const [flows, setFlows] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [installing, setInstalling] = useState(null)
  const [installSuccess, setInstallSuccess] = useState(null)
  const [activeTab, setActiveTab] = useState('flows') // 'flows' or 'taps'
  const [newTapUrl, setNewTapUrl] = useState('')
  const [newTapAlias, setNewTapAlias] = useState('')
  const [addingTap, setAddingTap] = useState(false)

  useEffect(() => {
    if (isOpen) {
      loadStore()
    }
  }, [isOpen])

  const loadStore = async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchFlowStore()
      setTaps(data.taps || [])
      setFlows(data.flows || [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  const handleInstall = async (flow) => {
    setInstalling(flow.fullName)
    setError(null)
    try {
      await installFlow(flow.tapName, flow.name)
      setInstallSuccess(flow.fullName)
      if (onInstall) onInstall(flow)
      await loadStore()
      setTimeout(() => setInstallSuccess(null), 3000)
    } catch (err) {
      setError(err.message)
    } finally {
      setInstalling(null)
    }
  }

  const handleUninstall = async (flow) => {
    setInstalling(flow.fullName)
    setError(null)
    try {
      await uninstallFlow(flow.tapName, flow.name)
      await loadStore()
    } catch (err) {
      setError(err.message)
    } finally {
      setInstalling(null)
    }
  }

  const handleAddTap = async () => {
    if (!newTapUrl.trim()) return
    setAddingTap(true)
    setError(null)
    try {
      await addTap(newTapUrl.trim(), newTapAlias.trim())
      setNewTapUrl('')
      setNewTapAlias('')
      await loadStore()
    } catch (err) {
      setError(err.message)
    } finally {
      setAddingTap(false)
    }
  }

  const handleRemoveTap = async (name) => {
    setError(null)
    try {
      await removeTap(name)
      await loadStore()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleRefresh = async () => {
    setLoading(true)
    setError(null)
    try {
      await updateStore()
      await loadStore()
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  // Filter flows by search
  const filteredFlows = flows.filter(f => 
    f.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    f.description.toLowerCase().includes(searchQuery.toLowerCase()) ||
    (f.tags && f.tags.some(t => t.toLowerCase().includes(searchQuery.toLowerCase())))
  )

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
            <Store size={24} className="text-blue-400" />
            <h2 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>Flow Store</h2>
            <span className="text-sm px-2 py-0.5 rounded-full bg-blue-500/20 text-blue-400">
              {flows.length} flows
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleRefresh}
              disabled={loading}
              className="p-2 rounded-lg hover:bg-white/10 transition-colors"
              style={{ color: 'var(--text-secondary)' }}
              title="Refresh stores"
            >
              <RefreshCw size={18} className={loading ? 'animate-spin' : ''} />
            </button>
            <button
              onClick={onClose}
              className="p-2 rounded-lg hover:bg-white/10 transition-colors"
              style={{ color: 'var(--text-secondary)' }}
            >
              <X size={20} />
            </button>
          </div>
        </div>

        {/* Tabs */}
        <div className="flex border-b" style={{ borderColor: 'var(--border-color)' }}>
          <button
            onClick={() => setActiveTab('flows')}
            className={`px-4 py-2 font-medium transition-colors ${
              activeTab === 'flows' 
                ? 'text-blue-400 border-b-2 border-blue-400' 
                : 'text-gray-400 hover:text-gray-300'
            }`}
          >
            <Package size={16} className="inline mr-2" />
            Browse Flows
          </button>
          <button
            onClick={() => setActiveTab('taps')}
            className={`px-4 py-2 font-medium transition-colors ${
              activeTab === 'taps' 
                ? 'text-blue-400 border-b-2 border-blue-400' 
                : 'text-gray-400 hover:text-gray-300'
            }`}
          >
            <Store size={16} className="inline mr-2" />
            Manage Taps ({taps.length})
          </button>
        </div>

        {/* Error Banner */}
        {error && (
          <div className="px-4 py-2 bg-red-500/20 border-b border-red-500/50 flex items-center gap-2">
            <AlertCircle size={16} className="text-red-400" />
            <span className="text-red-300 text-sm">{error}</span>
            <button onClick={() => setError(null)} className="ml-auto text-red-400 hover:text-red-300">
              <X size={14} />
            </button>
          </div>
        )}

        {/* Content */}
        <div className="flex-1 overflow-hidden">
          {activeTab === 'flows' ? (
            <div className="h-full flex flex-col">
              {/* Search */}
              <div className="p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
                <div className="relative">
                  <Search size={18} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                  <input
                    type="text"
                    placeholder="Search flows..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="w-full pl-10 pr-4 py-2 rounded-lg border bg-transparent outline-none focus:border-blue-500 transition-colors"
                    style={{ borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                  />
                </div>
              </div>

              {/* Flow List */}
              <div className="flex-1 overflow-y-auto p-4">
                {loading ? (
                  <div className="flex items-center justify-center h-32">
                    <Loader2 size={24} className="animate-spin text-blue-400" />
                  </div>
                ) : filteredFlows.length === 0 ? (
                  <div className="text-center py-8 text-gray-400">
                    {searchQuery ? 'No flows match your search' : 'No flows available'}
                  </div>
                ) : (
                  <div className="grid gap-3">
                    {filteredFlows.map(flow => (
                      <div
                        key={flow.fullName}
                        className="p-4 rounded-lg border transition-colors hover:border-blue-500/50"
                        style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}
                      >
                        <div className="flex items-start justify-between gap-4">
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2">
                              <h3 className="font-medium truncate" style={{ color: 'var(--text-primary)' }}>
                                {flow.fullName}
                              </h3>
                              {flow.tapName === 'official' && (
                                <span className="text-xs px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-400">
                                  official
                                </span>
                              )}
                              {flow.installed && (
                                <span className="text-xs px-1.5 py-0.5 rounded bg-green-500/20 text-green-400">
                                  installed
                                </span>
                              )}
                            </div>
                            <p className="text-sm mt-1 line-clamp-2" style={{ color: 'var(--text-secondary)' }}>
                              {flow.description}
                            </p>
                            {flow.tags && flow.tags.length > 0 && (
                              <div className="flex flex-wrap gap-1 mt-2">
                                {flow.tags.map(tag => (
                                  <span
                                    key={tag}
                                    className="text-xs px-2 py-0.5 rounded-full bg-gray-500/20"
                                    style={{ color: 'var(--text-secondary)' }}
                                  >
                                    {tag}
                                  </span>
                                ))}
                              </div>
                            )}
                          </div>
                          <div className="flex items-center gap-2">
                            {flow.installed ? (
                              <button
                                onClick={() => handleUninstall(flow)}
                                disabled={installing === flow.fullName}
                                className="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors bg-red-500/20 text-red-400 hover:bg-red-500/30"
                              >
                                {installing === flow.fullName ? (
                                  <Loader2 size={14} className="animate-spin" />
                                ) : (
                                  <>
                                    <Trash2 size={14} className="inline mr-1" />
                                    Uninstall
                                  </>
                                )}
                              </button>
                            ) : (
                              <button
                                onClick={() => handleInstall(flow)}
                                disabled={installing === flow.fullName}
                                className="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors bg-blue-500 text-white hover:bg-blue-600"
                              >
                                {installing === flow.fullName ? (
                                  <Loader2 size={14} className="animate-spin" />
                                ) : installSuccess === flow.fullName ? (
                                  <>
                                    <Check size={14} className="inline mr-1" />
                                    Installed
                                  </>
                                ) : (
                                  <>
                                    <Download size={14} className="inline mr-1" />
                                    Install
                                  </>
                                )}
                              </button>
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ) : (
            /* Taps Management */
            <div className="h-full flex flex-col p-4">
              {/* Add Tap Form */}
              <div className="mb-4 p-4 rounded-lg border" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
                <h3 className="font-medium mb-3" style={{ color: 'var(--text-primary)' }}>Add New Tap</h3>
                <div className="flex gap-2">
                  <input
                    type="text"
                    placeholder="owner or owner/repo"
                    value={newTapUrl}
                    onChange={(e) => setNewTapUrl(e.target.value)}
                    className="flex-1 px-3 py-2 rounded-lg border bg-transparent outline-none focus:border-blue-500"
                    style={{ borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                  />
                  <input
                    type="text"
                    placeholder="alias (optional)"
                    value={newTapAlias}
                    onChange={(e) => setNewTapAlias(e.target.value)}
                    className="w-32 px-3 py-2 rounded-lg border bg-transparent outline-none focus:border-blue-500"
                    style={{ borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
                  />
                  <button
                    onClick={handleAddTap}
                    disabled={addingTap || !newTapUrl.trim()}
                    className="px-4 py-2 rounded-lg font-medium transition-colors bg-blue-500 text-white hover:bg-blue-600 disabled:opacity-50"
                  >
                    {addingTap ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
                  </button>
                </div>
                <p className="text-xs mt-2" style={{ color: 'var(--text-secondary)' }}>
                  Just enter "owner" to add owner/astonish-flows, or "owner/repo" for custom repos
                </p>
              </div>

              {/* Taps List */}
              <div className="flex-1 overflow-y-auto">
                <div className="grid gap-2">
                  {taps.map(tap => (
                    <div
                      key={tap.name}
                      className="p-3 rounded-lg border flex items-center justify-between"
                      style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}
                    >
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-medium" style={{ color: 'var(--text-primary)' }}>
                            {tap.name}
                          </span>
                          {tap.isOfficial && (
                            <span className="text-xs px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-400">
                              official
                            </span>
                          )}
                        </div>
                        <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                          {tap.url}
                        </span>
                      </div>
                      {!tap.isOfficial && (
                        <button
                          onClick={() => handleRemoveTap(tap.name)}
                          className="p-2 rounded-lg text-red-400 hover:bg-red-500/20 transition-colors"
                          title="Remove tap"
                        >
                          <Trash2 size={16} />
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
