import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, saveButtonStyle } from './settingsApi'

export default function IdentitySettings({ config, onSaved }: { config: Record<string, any>; onSaved?: () => void }) {
  const [form, setForm] = useState({
    name: '',
    username: '',
    email: '',
    bio: '',
    website: '',
    locale: '',
    timezone: ''
  })
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (config) {
      setForm({
        name: config.name || '',
        username: config.username || '',
        email: config.email || '',
        bio: config.bio || '',
        website: config.website || '',
        locale: config.locale || '',
        timezone: config.timezone || ''
      })
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      await saveFullConfigSection('agent_identity', form)
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
      <p className="text-sm" style={hintStyle}>
        Configure the agent persona used for web portal registrations and profile information. 
        The agent uses these details to fill registration forms and maintain a consistent identity.
      </p>

      {/* Name & Username */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Display Name
          </label>
          <input
            type="text"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="Agent Name"
            className={inputClass}
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Username
          </label>
          <input
            type="text"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
            placeholder="agentuser"
            className={inputClass + ' font-mono'}
            style={inputStyle}
          />
          <p className="text-xs mt-1" style={hintStyle}>
            Base username for registrations.
          </p>
        </div>
      </div>

      {/* Email */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Email
        </label>
        <input
          type="email"
          value={form.email}
          onChange={(e) => setForm({ ...form, email: e.target.value })}
          placeholder="agent@example.com"
          className={inputClass}
          style={inputStyle}
        />
        <p className="text-xs mt-1" style={hintStyle}>
          Should match the email channel config if using email integration.
        </p>
      </div>

      {/* Bio */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Bio
        </label>
        <textarea
          value={form.bio}
          onChange={(e) => setForm({ ...form, bio: e.target.value })}
          placeholder="A short description for profile fields..."
          rows={3}
          className={inputClass}
          style={{ ...inputStyle, resize: 'vertical' }}
        />
      </div>

      {/* Website */}
      <div>
        <label className="block text-sm font-medium mb-2" style={labelStyle}>
          Website
        </label>
        <input
          type="url"
          value={form.website}
          onChange={(e) => setForm({ ...form, website: e.target.value })}
          placeholder="https://example.com"
          className={inputClass + ' font-mono'}
          style={inputStyle}
        />
      </div>

      {/* Locale & Timezone */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Locale
          </label>
          <select
            value={form.locale}
            onChange={(e) => setForm({ ...form, locale: e.target.value })}
            className={inputClass}
            style={inputStyle}
          >
            <option value="">Not set</option>
            <option value="en-US">English (US)</option>
            <option value="en-GB">English (UK)</option>
            <option value="es-ES">Spanish (Spain)</option>
            <option value="fr-FR">French (France)</option>
            <option value="de-DE">German (Germany)</option>
            <option value="it-IT">Italian (Italy)</option>
            <option value="pt-BR">Portuguese (Brazil)</option>
            <option value="ja-JP">Japanese</option>
            <option value="ko-KR">Korean</option>
            <option value="zh-CN">Chinese (Simplified)</option>
            <option value="zh-TW">Chinese (Traditional)</option>
            <option value="ru-RU">Russian</option>
            <option value="ar-SA">Arabic</option>
            <option value="hi-IN">Hindi</option>
            <option value="nl-NL">Dutch</option>
            <option value="sv-SE">Swedish</option>
            <option value="pl-PL">Polish</option>
            <option value="tr-TR">Turkish</option>
          </select>
        </div>
        <div>
          <label className="block text-sm font-medium mb-2" style={labelStyle}>
            Timezone
          </label>
          <input
            type="text"
            value={form.timezone}
            onChange={(e) => setForm({ ...form, timezone: e.target.value })}
            placeholder="America/New_York"
            className={inputClass + ' font-mono'}
            style={inputStyle}
          />
          <p className="text-xs mt-1" style={hintStyle}>
            IANA timezone identifier.
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
