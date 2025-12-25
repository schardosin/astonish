import React, { useState, useEffect } from 'react'
import { X, Lock, Save, Loader2, AlertCircle } from 'lucide-react'

export default function InstallMcpModal({ isOpen, onClose, onInstall, server }) {
  const [envVars, setEnvVars] = useState({})
  const [isInstalling, setIsInstalling] = useState(false)
  const [error, setError] = useState(null)

  // Initialize form with required env vars
  useEffect(() => {
    if (server?.config?.env) {
      const initial = {}
      // The store provides env as a map of key -> description/value
      // We want to capture values for keys, but skip defaults for sensitive fields
      Object.keys(server.config.env).forEach(key => {
        const isSensitive = /TOKEN|KEY|SECRET|PASSWORD|PASSWD|PWD|AUTH/i.test(key)
        initial[key] = isSensitive ? '' : (server.config.env[key] || '')
      })
      setEnvVars(initial)
    } else {
      setEnvVars({})
    }
    setError(null)
  }, [server])

  if (!isOpen || !server) return null

  const handleSubmit = async (e) => {
    e.preventDefault()
    setIsInstalling(true)
    setError(null)

    try {
      await onInstall(envVars)
      onClose()
    } catch (err) {
      setError(err.message || 'Failed to install server')
    } finally {
      setIsInstalling(false)
    }
  }

  const handleEnvChange = (key, value) => {
    setEnvVars(prev => ({
      ...prev,
      [key]: value
    }))
  }

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in duration-200">
      <div 
        className="w-full max-w-lg rounded-xl border shadow-2xl animate-in zoom-in-95 duration-200"
        style={{ 
          background: 'var(--bg-secondary)',
          borderColor: 'var(--border-color)',
          boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.3), 0 8px 10px -6px rgba(0, 0, 0, 0.3)'
        }}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            Install {server.name}
          </h2>
          <button
            onClick={onClose}
            className="p-1 rounded-lg hover:bg-gray-500/10 transition-colors"
            style={{ color: 'var(--text-muted)' }}
          >
            <X size={20} />
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="p-6 space-y-6">
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              {server.description}
            </p>

            {server.config?.env && Object.keys(server.config.env).length > 0 ? (
              <div className="space-y-4">
                <div className="flex items-center gap-2 text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                  <Lock size={16} className="text-blue-500" />
                  Configuration Required
                </div>
                
                <div className="space-y-4">
                  {Object.keys(server.config.env).map(key => {
                    const isSensitive = /TOKEN|KEY|SECRET|PASSWORD|PASSWD|PWD|AUTH/i.test(key)
                    return (
                      <div key={key} className="space-y-1.5">
                        <label 
                          htmlFor={`env-${key}`}
                          className="block text-xs font-medium uppercase tracking-wider"
                          style={{ color: 'var(--text-muted)' }}
                        >
                          {key}
                        </label>
                        <input
                          id={`env-${key}`}
                          type={isSensitive ? "password" : "text"}
                          value={envVars[key] || ''}
                          onChange={(e) => handleEnvChange(key, e.target.value)}
                          placeholder={`Enter ${key}`}
                          className="w-full px-3 py-2 rounded-lg border bg-black/20 focus:outline-none focus:ring-2 focus:ring-blue-500/50 transition-all font-mono text-sm"
                          style={{ 
                            borderColor: 'var(--border-color)',
                            color: 'var(--text-primary)'
                          }}
                        />
                      </div>
                    )
                  })}
                </div>
              </div>
            ) : (
              <div className="flex items-start gap-3 p-3 rounded-lg bg-blue-500/10 border border-blue-500/20">
                <AlertCircle size={18} className="text-blue-400 mt-0.5" />
                <div className="text-sm text-blue-300">
                  This server does not require any additional configuration. Click Install to proceed.
                </div>
              </div>
            )}

            {error && (
              <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm flex items-center gap-2">
                <AlertCircle size={16} />
                {error}
              </div>
            )}
          </div>

          <div className="flex items-center justify-end gap-3 p-4 border-t bg-black/20" style={{ borderColor: 'var(--border-color)' }}>
            <button
              type="button"
              onClick={onClose}
              disabled={isInstalling}
              className="px-4 py-2 rounded-lg text-sm font-medium hover:bg-gray-500/10 transition-colors"
              style={{ color: 'var(--text-primary)' }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isInstalling}
              className="px-4 py-2 rounded-lg text-sm font-medium flex items-center gap-2 shadow-lg transition-all hover:scale-[1.02] active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed"
              style={{ 
                background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)',
                color: 'white',
                boxShadow: '0 4px 12px rgba(124, 58, 237, 0.25)'
              }}
            >
              {isInstalling ? (
                <>
                  <Loader2 size={16} className="animate-spin" />
                  Installing...
                </>
              ) : (
                <>
                  <Save size={16} />
                  Install Server
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
