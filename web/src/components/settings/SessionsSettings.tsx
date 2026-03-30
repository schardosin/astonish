import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

export default function SessionsSettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    storage: '',
    base_dir: '',
    compaction: {
      enabled: true,
      threshold: 0.8,
      preserve_recent: 4
    },
    cleanup: {
      max_age_days: 5
    }
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        storage: config.storage || '',
        base_dir: config.base_dir || '',
        compaction: {
          enabled: config.compaction?.enabled !== false,
          threshold: config.compaction?.threshold || 0.8,
          preserve_recent: config.compaction?.preserve_recent || 4
        },
        cleanup: {
          max_age_days: config.cleanup?.max_age_days ?? 5
        }
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('sessions', form)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const effectiveStorage = form.storage || 'file'

  return (
    <div className="max-w-xl space-y-6">
      {/* Storage Type */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Storage Type
        </label>
        <select
          value={form.storage}
          onChange={(e) => setForm({ ...form, storage: e.target.value })}
          className={inputClass}
          style={inputStyle}
        >
          <option value="">File (default)</option>
          <option value="file">File</option>
          <option value="memory">Memory</option>
        </select>
        <p className="text-xs mt-1" style={hintStyle}>
          Default: File. Sessions are persisted to disk at ~/.config/astonish/sessions/ and survive restarts. 
          &quot;Memory&quot; stores sessions in RAM only (lost on restart, <code>astonish sessions</code> CLI will not work).
        </p>
      </div>

      {/* Base Directory */}
      {effectiveStorage === 'file' && (
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Sessions Directory
          </label>
          <input
            type="text"
            value={form.base_dir}
            onChange={(e) => setForm({ ...form, base_dir: e.target.value })}
            placeholder="~/.config/astonish/sessions/ (default)"
            className={inputClass + ' font-mono'}
            style={inputStyle}
          />
          <p className="text-xs mt-1" style={hintStyle}>
            Directory where session files are stored. Default: ~/.config/astonish/sessions/
          </p>
        </div>
      )}

      {/* Compaction */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <div className="flex items-center justify-between mb-4">
          <div>
            <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
              Context Compaction
            </h4>
            <p className="text-xs mt-0.5" style={hintStyle}>
              Automatically summarize older messages when the context window fills up. Default: enabled.
            </p>
          </div>
          <button
            onClick={() => setForm({ ...form, compaction: { ...form.compaction, enabled: !form.compaction.enabled } })}
            className="relative w-11 h-6 rounded-full transition-colors"
            style={{
              background: form.compaction.enabled ? '#a855f7' : 'var(--bg-tertiary)',
              border: `1px solid ${form.compaction.enabled ? '#a855f7' : 'var(--border-color)'}`
            }}
          >
            <span
              className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
              style={{ transform: form.compaction.enabled ? 'translateX(20px)' : 'translateX(0)' }}
            />
          </button>
        </div>

        {form.compaction.enabled && (
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Threshold
              </label>
              <input
                type="number"
                value={form.compaction.threshold}
                onChange={(e) => setForm({ ...form, compaction: { ...form.compaction, threshold: parseFloat(e.target.value) || 0.8 } })}
                min="0.1"
                max="1.0"
                step="0.05"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                Fraction of context window that triggers compaction. Default: 0.8 (80% full).
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Preserve Recent
              </label>
              <input
                type="number"
                value={form.compaction.preserve_recent}
                onChange={(e) => setForm({ ...form, compaction: { ...form.compaction, preserve_recent: parseInt(e.target.value) || 4 } })}
                min="1"
                max="20"
                className={inputClass}
                style={inputStyle}
              />
              <p className="text-xs mt-1" style={hintStyle}>
                Recent messages to keep intact (not summarized). Default: 4.
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Session Cleanup */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <div className="mb-4">
          <h4 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Session Cleanup
          </h4>
          <p className="text-xs mt-0.5" style={hintStyle}>
            Automatically delete sessions that have been inactive for a period of time.
            Containers associated with expired sessions are also destroyed.
          </p>
        </div>
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Auto-delete after (days)
          </label>
          <input
            type="number"
            value={form.cleanup.max_age_days}
            onChange={(e) => setForm({ ...form, cleanup: { ...form.cleanup, max_age_days: parseInt(e.target.value) || 0 } })}
            min="0"
            max="365"
            className={inputClass}
            style={{ ...inputStyle, maxWidth: '120px' }}
          />
          <p className="text-xs mt-1" style={hintStyle}>
            Sessions with no activity for this many days are automatically deleted. Set to 0 to disable. Default: 5 days.
          </p>
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
