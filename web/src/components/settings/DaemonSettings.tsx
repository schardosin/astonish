import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, AlertTriangle } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

export default function DaemonSettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    port: 9393,
    log_dir: '',
    auth: {
      disabled: false,
      session_ttl_days: 90
    }
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [restartRequired, setRestartRequired] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        port: config.port || 9393,
        log_dir: config.log_dir || '',
        auth: {
          disabled: config.auth?.disabled || false,
          session_ttl_days: config.auth?.session_ttl_days || 90
        }
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setRestartRequired(false)
    setError(null)
    try {
      const result = await saveFullConfigSection('daemon', form)
      setSaveSuccess(true)
      if (result.restart_required) {
        setRestartRequired(true)
      }
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="max-w-xl space-y-6">
      {/* Restart Warning */}
      {restartRequired && (
        <div className="flex items-start gap-2 p-3 rounded-lg text-sm"
          style={{ background: 'rgba(234, 179, 8, 0.1)', border: '1px solid rgba(234, 179, 8, 0.3)' }}>
          <AlertTriangle size={16} className="mt-0.5 flex-shrink-0" style={{ color: '#eab308' }} />
          <span style={{ color: '#eab308' }}>
            Settings saved. Restart the daemon for port or authentication changes to take effect.
          </span>
        </div>
      )}

      {/* Port */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          HTTP Port
        </label>
        <input
          type="number"
          value={form.port}
          onChange={(e) => setForm({ ...form, port: parseInt(e.target.value) || 9393 })}
          min="1024"
          max="65535"
          className={inputClass}
          style={inputStyle}
        />
        <p className="text-xs mt-1" style={hintStyle}>
          Port for the Astonish daemon HTTP server. Requires restart to take effect.
        </p>
      </div>

      {/* Log Directory */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Log Directory
        </label>
        <input
          type="text"
          value={form.log_dir}
          onChange={(e) => setForm({ ...form, log_dir: e.target.value })}
          placeholder="~/.config/astonish/logs/ (default)"
          className={inputClass + ' font-mono'}
          style={inputStyle}
        />
      </div>

      {/* Authentication */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <h4 className="text-sm font-medium mb-4" style={{ color: 'var(--text-primary)' }}>
          Studio Authentication
        </h4>
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                Disable Authentication
              </label>
              <p className="text-xs mt-0.5" style={hintStyle}>
                Turn off device-based authentication for the Studio web UI. Not recommended for remote access.
              </p>
            </div>
            <button
              onClick={() => setForm({ ...form, auth: { ...form.auth, disabled: !form.auth.disabled } })}
              className="relative w-11 h-6 rounded-full transition-colors"
              style={{
                background: form.auth.disabled ? '#ef4444' : 'var(--bg-tertiary)',
                border: `1px solid ${form.auth.disabled ? '#ef4444' : 'var(--border-color)'}`
              }}
            >
              <span
                className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
                style={{ transform: form.auth.disabled ? 'translateX(20px)' : 'translateX(0)' }}
              />
            </button>
          </div>
          {!form.auth.disabled && (
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Session TTL (days)
              </label>
              <input
                type="number"
                value={form.auth.session_ttl_days}
                onChange={(e) => setForm({ ...form, auth: { ...form.auth, session_ttl_days: parseInt(e.target.value) || 90 } })}
                min="1"
                max="365"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                How many days an authorized session remains valid before re-authorization.
              </p>
            </div>
          )}
        </div>
      </div>

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
