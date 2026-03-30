import { useState, useEffect } from 'react'
import { Save, AlertCircle, Check, ChevronDown, ChevronRight } from 'lucide-react'
import { saveFullConfigSection, inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

function ToggleSwitch({ value, onChange }) {
  return (
    <button
      onClick={() => onChange(!value)}
      className="relative w-11 h-6 rounded-full transition-colors"
      style={{
        background: value ? '#a855f7' : 'var(--bg-tertiary)',
        border: `1px solid ${value ? '#a855f7' : 'var(--border-color)'}`
      }}
    >
      <span
        className="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform bg-white"
        style={{ transform: value ? 'translateX(20px)' : 'translateX(0)' }}
      />
    </button>
  )
}

function CollapsibleSection({ title, description, enabled, onToggle, expanded, onExpand, children }) {
  return (
    <div className="rounded-lg border" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
      <div
        className="p-4 cursor-pointer flex items-center justify-between"
        onClick={onExpand}
      >
        <div className="flex items-center gap-3">
          {expanded ? <ChevronDown size={16} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={16} style={{ color: 'var(--text-muted)' }} />}
          <div>
            <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{title}</div>
            <div className="text-xs" style={hintStyle}>{description}</div>
          </div>
        </div>
        <div onClick={(e) => e.stopPropagation()}>
          <ToggleSwitch value={enabled} onChange={onToggle} />
        </div>
      </div>
      {expanded && (
        <div className="px-4 pb-4 space-y-4 border-t" style={sectionBorderStyle}>
          <div className="pt-4">
            {children}
          </div>
        </div>
      )}
    </div>
  )
}

export default function ChannelsSettings({ config, onSaved }) {
  const [form, setForm] = useState({
    enabled: false,
    telegram: {
      enabled: false,
      bot_token: '',
      allow_from: []
    },
    email: {
      enabled: false,
      provider: 'imap',
      imap_server: '',
      smtp_server: '',
      address: '',
      username: '',
      password: '',
      poll_interval: 30,
      allow_from: [],
      folder: 'INBOX',
      mark_read: true,
      max_body_chars: 50000
    }
  })
  const [tgExpanded, setTgExpanded] = useState(false)
  const [emailExpanded, setEmailExpanded] = useState(false)
  const [tgAllowFromText, setTgAllowFromText] = useState('')
  const [emailAllowFromText, setEmailAllowFromText] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (config) {
      const tg = config.telegram || {}
      const em = config.email || {}
      setForm({
        enabled: config.enabled || false,
        telegram: {
          enabled: tg.enabled || false,
          bot_token: tg.bot_token || '',
          allow_from: tg.allow_from || []
        },
        email: {
          enabled: em.enabled || false,
          provider: em.provider || 'imap',
          imap_server: em.imap_server || '',
          smtp_server: em.smtp_server || '',
          address: em.address || '',
          username: em.username || '',
          password: em.password || '',
          poll_interval: em.poll_interval || 30,
          allow_from: em.allow_from || [],
          folder: em.folder || 'INBOX',
          mark_read: em.mark_read !== false,
          max_body_chars: em.max_body_chars || 50000
        }
      })
      setTgAllowFromText((tg.allow_from || []).join(', '))
      setEmailAllowFromText((em.allow_from || []).join(', '))
      // Auto-expand configured sections
      if (tg.enabled) setTgExpanded(true)
      if (em.enabled) setEmailExpanded(true)
    }
  }, [config])

  const handleSave = async () => {
    setSaving(true)
    setSaveSuccess(false)
    setError(null)
    try {
      // Parse allow_from text fields into arrays
      const saveData = {
        ...form,
        telegram: {
          ...form.telegram,
          allow_from: tgAllowFromText.split(',').map(s => s.trim()).filter(Boolean)
        },
        email: {
          ...form.email,
          allow_from: emailAllowFromText.split(',').map(s => s.trim()).filter(Boolean)
        }
      }
      await saveFullConfigSection('channels', saveData)
      setSaveSuccess(true)
      if (onSaved) onSaved()
      setTimeout(() => setSaveSuccess(false), 2000)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const updateTelegram = (updates) => {
    setForm(f => ({ ...f, telegram: { ...f.telegram, ...updates } }))
  }

  const updateEmail = (updates) => {
    setForm(f => ({ ...f, email: { ...f.email, ...updates } }))
  }

  return (
    <div className="max-w-xl space-y-6">
      {/* Master Toggle */}
      <div className="flex items-center justify-between">
        <div>
          <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Enable Channels
          </label>
          <p className="text-xs mt-0.5" style={hintStyle}>
            Master switch for all communication channels (Telegram, Email).
          </p>
        </div>
        <ToggleSwitch value={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
      </div>

      {/* Telegram */}
      <CollapsibleSection
        title="Telegram"
        description="Receive and respond to messages via Telegram bot"
        enabled={form.telegram.enabled}
        onToggle={(v) => updateTelegram({ enabled: v })}
        expanded={tgExpanded}
        onExpand={() => setTgExpanded(!tgExpanded)}
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Bot Token
            </label>
            <input
              type="password"
              value={form.telegram.bot_token}
              onChange={(e) => updateTelegram({ bot_token: e.target.value })}
              placeholder="Paste your BotFather token"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Get a token from <a href="https://t.me/BotFather" target="_blank" rel="noreferrer" className="underline" style={{ color: 'var(--accent)' }}>@BotFather</a> on Telegram.
            </p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Allowed User IDs
            </label>
            <input
              type="text"
              value={tgAllowFromText}
              onChange={(e) => setTgAllowFromText(e.target.value)}
              placeholder="123456789, 987654321"
              className={inputClass + ' font-mono'}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Comma-separated Telegram user IDs allowed to interact with the bot. Required for security.
            </p>
          </div>
        </div>
      </CollapsibleSection>

      {/* Email */}
      <CollapsibleSection
        title="Email"
        description="Monitor an inbox and respond to emails"
        enabled={form.email.enabled}
        onToggle={(v) => updateEmail({ enabled: v })}
        expanded={emailExpanded}
        onExpand={() => setEmailExpanded(!emailExpanded)}
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                IMAP Server
              </label>
              <input
                type="text"
                value={form.email.imap_server}
                onChange={(e) => updateEmail({ imap_server: e.target.value })}
                placeholder="imap.gmail.com:993"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                SMTP Server
              </label>
              <input
                type="text"
                value={form.email.smtp_server}
                onChange={(e) => updateEmail({ smtp_server: e.target.value })}
                placeholder="smtp.gmail.com:587"
                className={inputClass + ' font-mono'}
                style={inputStyle}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Email Address
            </label>
            <input
              type="email"
              value={form.email.address}
              onChange={(e) => updateEmail({ address: e.target.value })}
              placeholder="agent@example.com"
              className={inputClass}
              style={inputStyle}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Username
              </label>
              <input
                type="text"
                value={form.email.username}
                onChange={(e) => updateEmail({ username: e.target.value })}
                placeholder="Same as email (default)"
                className={inputClass}
                style={inputStyle}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>
                Password
              </label>
              <input
                type="password"
                value={form.email.password}
                onChange={(e) => updateEmail({ password: e.target.value })}
                placeholder="App password"
                className={inputClass}
                style={inputStyle}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2" style={labelStyle}>
              Allowed Senders
            </label>
            <input
              type="text"
              value={emailAllowFromText}
              onChange={(e) => setEmailAllowFromText(e.target.value)}
              placeholder="user@example.com, * for anyone"
              className={inputClass}
              style={inputStyle}
            />
            <p className="text-xs mt-1" style={hintStyle}>
              Comma-separated email addresses. Use * to allow anyone.
            </p>
          </div>

          {/* Advanced email options */}
          <div className="pt-3 border-t space-y-4" style={sectionBorderStyle}>
            <div className="grid grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Poll Interval (sec)
                </label>
                <input
                  type="number"
                  value={form.email.poll_interval}
                  onChange={(e) => updateEmail({ poll_interval: parseInt(e.target.value) || 30 })}
                  min="5"
                  className={inputClass}
                  style={inputStyle}
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Folder
                </label>
                <input
                  type="text"
                  value={form.email.folder}
                  onChange={(e) => updateEmail({ folder: e.target.value })}
                  placeholder="INBOX"
                  className={inputClass}
                  style={inputStyle}
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>
                  Max Body (chars)
                </label>
                <input
                  type="number"
                  value={form.email.max_body_chars}
                  onChange={(e) => updateEmail({ max_body_chars: parseInt(e.target.value) || 50000 })}
                  min="1000"
                  className={inputClass}
                  style={inputStyle}
                />
              </div>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <label className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Mark as Read</label>
                <p className="text-xs" style={hintStyle}>Mark processed emails as read.</p>
              </div>
              <ToggleSwitch value={form.email.mark_read} onChange={(v) => updateEmail({ mark_read: v })} />
            </div>
          </div>
        </div>
      </CollapsibleSection>

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
