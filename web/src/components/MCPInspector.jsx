import React, { useState, useEffect, useMemo } from 'react'
import { X, Play, Search, ChevronRight, Loader2, AlertCircle, CheckCircle, Clock } from 'lucide-react'

// Fetch tools for a specific MCP server
const fetchServerTools = async (serverName) => {
  const res = await fetch(`/api/mcp/${encodeURIComponent(serverName)}/tools`)
  if (!res.ok) throw new Error('Failed to fetch tools')
  return res.json()
}

// Run a tool on a specific MCP server
const runServerTool = async (serverName, toolName, params) => {
  const res = await fetch(`/api/mcp/${encodeURIComponent(serverName)}/tools/${encodeURIComponent(toolName)}/run`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ params })
  })
  if (!res.ok) throw new Error('Failed to run tool')
  return res.json()
}

export default function MCPInspector({ serverName, onClose }) {
  const [tools, setTools] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedTool, setSelectedTool] = useState(null)
  const [params, setParams] = useState({})
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState(null)
  const [resultError, setResultError] = useState(null)

  // Load tools when component mounts
  useEffect(() => {
    setLoading(true)
    setError(null)
    fetchServerTools(serverName)
      .then(data => {
        if (data.error) {
          setError(data.error)
        } else {
          setTools(data.tools || [])
          if (data.tools?.length > 0) {
            setSelectedTool(data.tools[0])
          }
        }
      })
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [serverName])

  // Filter tools based on search
  const filteredTools = useMemo(() => {
    if (!searchQuery) return tools
    const query = searchQuery.toLowerCase()
    return tools.filter(t => 
      t.name.toLowerCase().includes(query) || 
      (t.description && t.description.toLowerCase().includes(query))
    )
  }, [tools, searchQuery])

  // Reset params when tool changes
  useEffect(() => {
    setParams({})
    setResult(null)
    setResultError(null)
  }, [selectedTool])

  // Handle running the tool
  const handleRun = async () => {
    if (!selectedTool) return
    setRunning(true)
    setResult(null)
    setResultError(null)
    try {
      const res = await runServerTool(serverName, selectedTool.name, params)
      if (res.success) {
        setResult(res)
      } else {
        setResultError(res.error || 'Unknown error')
        setResult(res)
      }
    } catch (err) {
      setResultError(err.message)
    } finally {
      setRunning(false)
    }
  }

  // Render a parameter field based on its type
  const renderParamField = (name, schema = {}) => {
    const type = schema.type || 'string'
    const value = params[name] ?? ''

    return (
      <div key={name} className="space-y-1">
        <label className="block text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
          {name}
          {schema.description && (
            <span className="font-normal ml-1" style={{ color: 'var(--text-muted)' }}>
              - {schema.description}
            </span>
          )}
        </label>
        {type === 'boolean' ? (
          <select
            value={value === true ? 'true' : value === false ? 'false' : ''}
            onChange={(e) => setParams({ ...params, [name]: e.target.value === 'true' })}
            className="w-full px-3 py-2 rounded-lg border text-sm"
            style={{ 
              background: 'var(--bg-primary)', 
              borderColor: 'var(--border-color)', 
              color: 'var(--text-primary)' 
            }}
          >
            <option value="">Select...</option>
            <option value="true">true</option>
            <option value="false">false</option>
          </select>
        ) : type === 'array' || type === 'object' ? (
          <textarea
            value={typeof value === 'object' ? JSON.stringify(value, null, 2) : value}
            onChange={(e) => {
              try {
                setParams({ ...params, [name]: JSON.parse(e.target.value) })
              } catch {
                setParams({ ...params, [name]: e.target.value })
              }
            }}
            placeholder={`Enter ${type === 'array' ? 'array' : 'object'} as JSON...`}
            rows={3}
            className="w-full px-3 py-2 rounded-lg border text-sm font-mono resize-y"
            style={{ 
              background: 'var(--bg-primary)', 
              borderColor: 'var(--border-color)', 
              color: 'var(--text-primary)' 
            }}
          />
        ) : type === 'number' || type === 'integer' ? (
          <input
            type="number"
            value={value}
            onChange={(e) => setParams({ ...params, [name]: e.target.valueAsNumber || '' })}
            placeholder={`Enter ${name}...`}
            className="w-full px-3 py-2 rounded-lg border text-sm"
            style={{ 
              background: 'var(--bg-primary)', 
              borderColor: 'var(--border-color)', 
              color: 'var(--text-primary)' 
            }}
          />
        ) : (
          <input
            type="text"
            value={value}
            onChange={(e) => setParams({ ...params, [name]: e.target.value })}
            placeholder={`Enter ${name}...`}
            className="w-full px-3 py-2 rounded-lg border text-sm"
            style={{ 
              background: 'var(--bg-primary)', 
              borderColor: 'var(--border-color)', 
              color: 'var(--text-primary)' 
            }}
          />
        )}
      </div>
    )
  }

  // Extract parameters from tool schema
  const getToolParams = (tool) => {
    if (!tool?.parameters) return {}
    const params = tool.parameters
    if (params.properties) return params.properties
    if (typeof params === 'object') return params
    return {}
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-8" style={{ background: 'rgba(0,0,0,0.8)' }}>
      <div 
        className="w-full max-w-6xl h-[80vh] rounded-xl flex flex-col overflow-hidden shadow-2xl"
        style={{ background: 'var(--bg-primary)', border: '1px solid var(--border-color)' }}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <div>
            <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
              Tool Inspector
            </h2>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              {serverName}
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-2 rounded-lg transition-colors hover:bg-gray-600/20"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={20} />
          </button>
        </div>

        {loading ? (
          <div className="flex-1 flex items-center justify-center p-8">
            <Loader2 className="animate-spin" size={32} style={{ color: 'var(--accent)' }} />
          </div>
        ) : error ? (
          <div className="flex-1 flex items-center justify-center p-8">
            <div className="text-center">
              <AlertCircle size={48} className="mx-auto mb-4" style={{ color: 'var(--danger)' }} />
              <p className="text-lg font-medium" style={{ color: 'var(--text-primary)' }}>Failed to load tools</p>
              <p className="text-sm mt-2" style={{ color: 'var(--text-muted)' }}>{error}</p>
            </div>
          </div>
        ) : (
          <div className="flex-1 flex overflow-hidden">
            {/* Tool list */}
            <div className="w-72 shrink-0 border-r flex flex-col" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
              {/* Search */}
              <div className="p-3 border-b" style={{ borderColor: 'var(--border-color)' }}>
                <div className="relative">
                  <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
                  <input
                    type="text"
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    placeholder="Search tools..."
                    className="w-full pl-9 pr-3 py-2 rounded-lg border text-sm"
                    style={{ 
                      background: 'var(--bg-primary)', 
                      borderColor: 'var(--border-color)', 
                      color: 'var(--text-primary)' 
                    }}
                  />
                </div>
              </div>
              
              {/* Tool list */}
              <div className="flex-1 overflow-y-auto p-2 space-y-1">
                {filteredTools.length === 0 ? (
                  <p className="text-sm text-center p-4" style={{ color: 'var(--text-muted)' }}>
                    {searchQuery ? 'No tools match your search' : 'No tools available'}
                  </p>
                ) : (
                  filteredTools.map(tool => (
                    <button
                      key={tool.name}
                      onClick={() => setSelectedTool(tool)}
                      className={`w-full text-left px-3 py-2 rounded-lg transition-all ${
                        selectedTool?.name === tool.name ? 'ring-1 ring-purple-500/30' : ''
                      }`}
                      style={{ 
                        background: selectedTool?.name === tool.name ? 'var(--accent-soft)' : 'transparent',
                        color: selectedTool?.name === tool.name ? 'var(--accent)' : 'var(--text-secondary)'
                      }}
                    >
                      <div className="flex items-center gap-2">
                        <ChevronRight size={14} className={selectedTool?.name === tool.name ? 'opacity-100' : 'opacity-0'} />
                        <span className="text-sm font-medium truncate">{tool.name}</span>
                      </div>
                    </button>
                  ))
                )}
              </div>
              
              <div className="p-2 border-t text-xs text-center" style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
                {tools.length} tools available
              </div>
            </div>

            {/* Tool details and execution */}
            <div className="flex-1 flex flex-col overflow-hidden">
              {selectedTool ? (
                <>
                  {/* Tool info */}
                  <div className="p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
                    <h3 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
                      {selectedTool.name}
                    </h3>
                    {selectedTool.description && (
                      <p className="text-sm mt-1" style={{ color: 'var(--text-muted)' }}>
                        {selectedTool.description}
                      </p>
                    )}
                  </div>

                  {/* Parameters form */}
                  <div className="flex-1 overflow-y-auto p-4 space-y-4">
                    <h4 className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                      Parameters
                    </h4>
                    {Object.keys(getToolParams(selectedTool)).length === 0 ? (
                      <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                        This tool has no parameters
                      </p>
                    ) : (
                      Object.entries(getToolParams(selectedTool)).map(([name, schema]) => 
                        renderParamField(name, schema)
                      )
                    )}

                    {/* Run button */}
                    <button
                      onClick={handleRun}
                      disabled={running}
                      className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
                      style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                    >
                      {running ? (
                        <>
                          <Loader2 size={16} className="animate-spin" />
                          Running...
                        </>
                      ) : (
                        <>
                          <Play size={16} />
                          Run Tool
                        </>
                      )}
                    </button>

                    {/* Result display */}
                    {result && (
                      <div className="mt-4 space-y-2">
                        <div className="flex items-center gap-2">
                          {result.success ? (
                            <CheckCircle size={16} style={{ color: '#22c55e' }} />
                          ) : (
                            <AlertCircle size={16} style={{ color: '#ef4444' }} />
                          )}
                          <span className="text-sm font-medium" style={{ color: result.success ? '#22c55e' : '#ef4444' }}>
                            {result.success ? 'Success' : 'Error'}
                          </span>
                          {result.time_taken && (
                            <span className="flex items-center gap-1 text-xs" style={{ color: 'var(--text-muted)' }}>
                              <Clock size={12} />
                              {result.time_taken}
                            </span>
                          )}
                        </div>
                        
                        {resultError && (
                          <div 
                            className="p-3 rounded-lg text-sm"
                            style={{ 
                              background: 'rgba(239, 68, 68, 0.1)', 
                              border: '1px solid rgba(239, 68, 68, 0.2)',
                              color: '#f87171'
                            }}
                          >
                            {resultError}
                          </div>
                        )}

                        {result.result && (
                          <pre 
                            className="p-3 rounded-lg text-sm font-mono overflow-x-auto"
                            style={{ 
                              background: 'var(--bg-secondary)', 
                              border: '1px solid var(--border-color)',
                              color: 'var(--text-primary)'
                            }}
                          >
                            {JSON.stringify(result.result, null, 2)}
                          </pre>
                        )}
                      </div>
                    )}
                  </div>
                </>
              ) : (
                <div className="flex-1 flex items-center justify-center">
                  <p style={{ color: 'var(--text-muted)' }}>Select a tool to inspect</p>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
