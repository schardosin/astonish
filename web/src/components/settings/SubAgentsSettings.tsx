import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

export default function SubAgentsSettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    enabled: true,
    max_depth: 2,
    max_concurrent: 5,
    task_timeout_sec: 300
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        enabled: config.enabled !== false,
        max_depth: config.max_depth || 2,
        max_concurrent: config.max_concurrent || 5,
        task_timeout_sec: config.task_timeout_sec || 300
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('sub_agents', form)
      setSaveSuccess(true)
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
      {/* Master Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Enable Sub-Agents
          </label>
          <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
            Allow the AI to delegate subtasks to specialized sub-agents for parallel execution.
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
        <div className="pt-4 border-t space-y-4" style={sectionBorderStyle}>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Max Delegation Depth
            </label>
            <input
              type="number"
              value={form.max_depth}
              onChange={(e) => setForm({ ...form, max_depth: parseInt(e.target.value) || 2 })}
              min="1"
              max="10"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Maximum nesting depth for sub-agent delegation chains.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Max Concurrent
            </label>
            <input
              type="number"
              value={form.max_concurrent}
              onChange={(e) => setForm({ ...form, max_concurrent: parseInt(e.target.value) || 5 })}
              min="1"
              max="20"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Maximum number of sub-agents running in parallel.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Task Timeout (seconds)
            </label>
            <input
              type="number"
              value={form.task_timeout_sec}
              onChange={(e) => setForm({ ...form, task_timeout_sec: parseInt(e.target.value) || 300 })}
              min="30"
              max="3600"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Maximum time per sub-agent task before it is cancelled.
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
