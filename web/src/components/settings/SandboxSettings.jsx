import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, ChevronDown, ChevronRight, Shield, ShieldOff, Loader2 } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'
import { fetchSandboxStatus } from '../../api/sandbox'

export default function SandboxSettings({ config, onSaved }) {
  const [form, setForm] = useState({
    enabled: true,
    memory: '2GB',
    cpu: 2,
    processes: 500,
    network: 'bridged',
    warm_pool: 2,
    orphan_check_hours: 6
  })
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)
  const [status, setStatus] = useState(null)
  const [statusLoading, setStatusLoading] = useState(true)

  useEffect(() => {
    if (config) {
      setForm({
        enabled: config.enabled ?? true,
        memory: config.memory || '2GB',
        cpu: config.cpu || 2,
        processes: config.processes || 500,
        network: config.network || 'bridged',
        warm_pool: config.warm_pool || 2,
        orphan_check_hours: config.orphan_check_hours || 6
      })
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

  // Status badge
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

      {/* Advanced Settings Toggle */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <button
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="flex items-center gap-2 text-sm font-medium transition-colors"
          style={{ color: 'var(--text-secondary)' }}
        >
          {showAdvanced ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
          Advanced Settings
        </button>
      </div>

      {showAdvanced && (
        <div className="space-y-5">
          {/* Resource Limits */}
          <div>
            <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
              Resource Limits
            </h4>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Memory
                </label>
                <input
                  type="text"
                  value={form.memory}
                  onChange={(e) => setForm({ ...form, memory: e.target.value })}
                  placeholder="2GB"
                  className={inputClass}
                  style={inputStyle}
                />
                <p className="text-xs mt-1" style={hintStyle}>
                  Memory limit per container (e.g. &quot;2GB&quot;, &quot;4GB&quot;)
                </p>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>
                    CPU Cores
                  </label>
                  <input
                    type="number"
                    value={form.cpu}
                    onChange={(e) => setForm({ ...form, cpu: parseInt(e.target.value) || 2 })}
                    min="1"
                    max="32"
                    className={inputClass}
                    style={inputStyle}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>
                    Max Processes
                  </label>
                  <input
                    type="number"
                    value={form.processes}
                    onChange={(e) => setForm({ ...form, processes: parseInt(e.target.value) || 500 })}
                    min="50"
                    max="10000"
                    className={inputClass}
                    style={inputStyle}
                  />
                </div>
              </div>
            </div>
          </div>

          {/* Network */}
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Network Mode
            </label>
            <select
              value={form.network}
              onChange={(e) => setForm({ ...form, network: e.target.value })}
              className={inputClass}
              style={inputStyle}
            >
              <option value="bridged">Bridged (containers have network access)</option>
              <option value="none">None (no network access)</option>
            </select>
          </div>

          {/* Warm Pool */}
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Warm Pool Size
            </label>
            <input
              type="number"
              value={form.warm_pool}
              onChange={(e) => setForm({ ...form, warm_pool: parseInt(e.target.value) || 0 })}
              min="0"
              max="10"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Number of pre-warmed containers kept ready for instant session creation
            </p>
          </div>

          {/* Prune */}
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Orphan Check Interval (hours)
            </label>
            <input
              type="number"
              value={form.orphan_check_hours}
              onChange={(e) => setForm({ ...form, orphan_check_hours: parseInt(e.target.value) || 6 })}
              min="1"
              max="168"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              How often to check for and clean up orphaned containers
            </p>
          </div>
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
