import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, Shield, ShieldOff, Loader2 } from 'lucide-react'
import { saveFullConfigSection, hintStyle, saveButtonStyle } from './settingsApi'
import { fetchSandboxStatus } from '../../api/sandbox'

export default function SandboxSettings({ config, onSaved }) {
  const [form, setForm] = useState({ enabled: true })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)
  const [status, setStatus] = useState(null)
  const [statusLoading, setStatusLoading] = useState(true)

  useEffect(() => {
    if (config) {
      setForm({ enabled: config.enabled ?? true })
    }
  }, [config])

  useEffect(() => {
    setStatusLoading(true)
    fetchSandboxStatus()
      .then(setStatus)
      .catch(() => setStatus(null))
      .finally(() => setStatusLoading(false))
  }, [])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('sandbox', form)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const renderStatus = () => {
    if (statusLoading) {
      return (
        <div className="flex items-center gap-2 text-xs" style={{ color: 'var(--text-muted)' }}>
          <Loader2 size={14} className="animate-spin" /> Checking runtime...
        </div>
      )
    }
    if (!status) return null

    if (status.platform === 'unsupported') {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
          <ShieldOff size={14} style={{ color: '#ef4444' }} />
          <span style={{ color: '#ef4444' }}>
            Platform not supported{status.reason ? ` — ${status.reason}` : ''}
          </span>
        </div>
      )
    }

    if (!status.incusAvailable) {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(234, 179, 8, 0.1)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
          <Shield size={14} style={{ color: '#eab308' }} />
          <span style={{ color: '#eab308' }}>
            Incus not available — install with <code className="font-mono">sudo apt install incus</code>
          </span>
        </div>
      )
    }

    if (!status.baseTemplateExists) {
      return (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
          style={{ background: 'rgba(234, 179, 8, 0.1)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
          <Shield size={14} style={{ color: '#eab308' }} />
          <span style={{ color: '#eab308' }}>
            Base template not initialized — run the Setup Wizard to create it
          </span>
        </div>
      )
    }

    return (
      <div className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
        style={{ background: 'rgba(34, 197, 94, 0.1)', border: '1px solid rgba(34, 197, 94, 0.3)' }}>
        <Shield size={14} style={{ color: '#22c55e' }} />
        <span style={{ color: '#22c55e' }}>
          Runtime ready — Incus available, base template configured
        </span>
      </div>
    )
  }

  return (
    <div className="max-w-xl space-y-6">
      {/* Runtime Status */}
      {renderStatus()}

      {/* Enabled Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Sandbox Isolation
          </label>
          <p className="text-xs mt-0.5" style={hintStyle}>
            When enabled, all tool execution runs inside isolated containers. When disabled, tools execute directly on the host.
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

      {!form.enabled && (
        <div className="flex items-start gap-2 p-3 rounded-lg text-xs"
          style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
          <AlertCircle size={14} className="mt-0.5 flex-shrink-0" style={{ color: '#ef4444' }} />
          <span style={{ color: '#ef4444' }}>
            Sandbox is disabled. AI tools will execute directly on your host system with full access to files, network, and system resources.
          </span>
        </div>
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
