import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, Info } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

export default function OpenCodeSettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    model: ''
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        model: config.model || ''
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('open_code', form)
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
      {/* Info Banner */}
      <div
        className="flex items-start gap-3 p-4 rounded-lg border"
        style={{
          background: 'var(--bg-secondary)',
          borderColor: 'var(--border-color)'
        }}
      >
        <Info size={18} className="mt-0.5 flex-shrink-0" style={{ color: 'var(--accent)' }} />
        <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          <p>
            OpenCode is automatically configured to use the same provider and credentials as Astonish.
            No separate OpenCode setup is needed.
          </p>
          <p className="mt-2">
            By default, OpenCode uses the same model configured in General settings.
            You can override the model below if you want OpenCode to use a different one.
          </p>
        </div>
      </div>

      {/* Model Override */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Model Override
        </label>
        <input
          type="text"
          value={form.model}
          onChange={(e) => setForm({ ...form, model: e.target.value })}
          placeholder="Same as Astonish (default)"
          className={inputClass + ' font-mono'}
          style={inputStyle}
        />
        <p className="text-xs mt-1" style={hintStyle}>
          Override the model used by OpenCode. Leave empty to use the same model as Astonish.
          Enter the model name as configured in your provider (e.g., &quot;claude-4.5-sonnet&quot;, &quot;gpt-5&quot;).
        </p>
      </div>

      {/* Save */}
      <div className="pt-4 border-t flex items-center gap-3" style={sectionBorderStyle}>
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
