import { useState, useEffect, useCallback } from 'react'
import { Trash2, Loader2, ToggleLeft, ToggleRight } from 'lucide-react'
import * as adminApi from '../../api/platformAdmin'
import { InlineError, InlineSuccess, inputStyle } from './shared'

// ---------------------------------------------------------------------------
// Channels Tab
// ---------------------------------------------------------------------------

export default function ChannelsTab() {
  const [channels, setChannels] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [expandedChannel, setExpandedChannel] = useState(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await adminApi.listChannels()
      setChannels(data || [])
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  if (loading) {
    return <div className="flex items-center justify-center py-12"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>
  }

  return (
    <div className="p-6 space-y-4">
      <div className="mb-2">
        <h3 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>Channel Adapters</h3>
        <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
          Configure messaging channels. Changes are applied immediately after save.
        </p>
      </div>

      <InlineError msg={error} />
      <InlineSuccess msg={success} />

      <div className="space-y-3">
        {channels.map(ch => (
          <ChannelCard
            key={ch.type}
            channel={ch}
            expanded={expandedChannel === ch.type}
            onToggle={() => setExpandedChannel(expandedChannel === ch.type ? null : ch.type)}
            onSaved={(msg) => { setSuccess(msg); setError(''); load() }}
            onError={(msg) => { setError(msg); setSuccess('') }}
            onDeleted={(msg) => { setSuccess(msg); setError(''); load() }}
          />
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Channel Card
// ---------------------------------------------------------------------------

function ChannelCard({ channel, expanded, onToggle, onSaved, onError, onDeleted }) {
  const [form, setForm] = useState({})
  const [secrets, setSecrets] = useState({})
  const [enabled, setEnabled] = useState(channel.enabled)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setForm({ ...channel.config })
    setEnabled(channel.enabled)
    setSecrets({})
  }, [channel])

  const handleSave = async () => {
    setSaving(true)
    try {
      const result = await adminApi.saveChannel(channel.type, {
        enabled,
        config: form,
        secrets,
      })
      onSaved(result.message)
    } catch (e) {
      onError(e.message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!confirm(`Remove all configuration and secrets for ${channel.type}? The channel will stop working.`)) return
    try {
      const result = await adminApi.deleteChannel(channel.type)
      onDeleted(result.message)
    } catch (e) {
      onError(e.message)
    }
  }

  const statusColor = channel.enabled && channel.secrets_configured ? '#22c55e' : channel.enabled ? '#f59e0b' : 'var(--text-muted)'
  const statusText = channel.enabled && channel.secrets_configured ? 'Active' : channel.enabled ? 'Missing secrets' : 'Disabled'

  return (
    <div className="rounded-xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
      {/* Header */}
      <div className="flex items-center justify-between p-4 cursor-pointer" onClick={onToggle}>
        <div className="flex items-center gap-3">
          <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            {channel.type.charAt(0).toUpperCase() + channel.type.slice(1)}
          </span>
          <span className="text-xs px-2 py-0.5 rounded-full" style={{
            background: `${statusColor}15`,
            color: statusColor,
          }}>
            {statusText}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{channel.description}</span>
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{expanded ? '\u25B2' : '\u25BC'}</span>
        </div>
      </div>

      {/* Expanded form */}
      {expanded && (
        <div className="px-4 pb-4 space-y-4" style={{ borderTop: '1px solid var(--border-color)' }}>
          {/* Enable toggle */}
          <div className="flex items-center justify-between pt-3">
            <label className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Enabled</label>
            <button
              onClick={() => setEnabled(!enabled)}
              className="flex items-center gap-1.5 text-xs font-medium"
              style={{ color: enabled ? '#22c55e' : 'var(--text-muted)' }}
            >
              {enabled ? <ToggleRight size={18} /> : <ToggleLeft size={18} />}
              {enabled ? 'On' : 'Off'}
            </button>
          </div>

          {/* Channel-specific config fields */}
          {channel.type === 'telegram' && (
            <TelegramConfigFields />
          )}
          {channel.type === 'email' && (
            <EmailConfigFields form={form} setForm={setForm} />
          )}
          {channel.type === 'slack' && (
            <SlackConfigFields form={form} setForm={setForm} />
          )}

          {/* Secrets */}
          <div className="pt-2">
            <label className="block text-xs font-semibold mb-2" style={{ color: 'var(--text-secondary)' }}>Credentials</label>
            <div className="space-y-2">
              {channel.secrets.map(s => (
                <div key={s.key}>
                  <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>
                    {s.label}
                    {s.configured && <span className="ml-1.5 text-xs" style={{ color: '#22c55e' }}>&#9679;</span>}
                  </label>
                  <input
                    type="password"
                    value={secrets[s.key] || ''}
                    onChange={e => setSecrets(prev => ({ ...prev, [s.key]: e.target.value }))}
                    placeholder={s.configured ? '(set -- leave blank to keep)' : 'Enter value...'}
                    className="w-full px-3 py-2 rounded-lg text-xs outline-none font-mono"
                    style={inputStyle}
                  />
                </div>
              ))}
            </div>
          </div>

          {/* Actions */}
          <div className="flex items-center justify-between pt-3" style={{ borderTop: '1px solid var(--border-color)' }}>
            <button onClick={handleDelete} className="px-3 py-2 rounded-lg text-xs font-medium" style={{ color: '#ef4444' }}>
              <Trash2 size={12} className="inline mr-1" />Remove Channel
            </button>
            <button onClick={handleSave} disabled={saving} className="px-4 py-2 rounded-lg text-xs font-medium text-white" style={{ background: 'var(--accent)' }}>
              {saving ? <Loader2 size={12} className="animate-spin inline mr-1" /> : null}
              Save & Apply
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Channel-specific config fields
// ---------------------------------------------------------------------------

function TelegramConfigFields() {
  return (
    <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
      Telegram only requires a bot token. Create one via <span className="font-mono">@BotFather</span> on Telegram.
    </p>
  )
}

function EmailConfigFields({ form, setForm }) {
  const update = (key, value) => setForm({ ...form, [key]: value })

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>IMAP Server</label>
          <input
            type="text"
            value={form.imap_server || ''}
            onChange={e => update('imap_server', e.target.value)}
            placeholder="imap.gmail.com:993"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>SMTP Server</label>
          <input
            type="text"
            value={form.smtp_server || ''}
            onChange={e => update('smtp_server', e.target.value)}
            placeholder="smtp.gmail.com:587"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Email Address</label>
          <input
            type="text"
            value={form.address || ''}
            onChange={e => update('address', e.target.value)}
            placeholder="agent@example.com"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Username</label>
          <input
            type="text"
            value={form.username || ''}
            onChange={e => update('username', e.target.value)}
            placeholder="(defaults to address)"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Provider</label>
          <select
            value={form.provider || 'imap'}
            onChange={e => update('provider', e.target.value)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          >
            <option value="imap">IMAP</option>
            <option value="gmail">Gmail</option>
          </select>
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Poll Interval (sec)</label>
          <input
            type="number"
            value={form.poll_interval || 30}
            onChange={e => update('poll_interval', parseInt(e.target.value) || 30)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Folder</label>
          <input
            type="text"
            value={form.folder || 'INBOX'}
            onChange={e => update('folder', e.target.value)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="flex items-center gap-2">
          <label className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>Mark Read</label>
          <button
            onClick={() => update('mark_read', !(form.mark_read ?? true))}
            className="text-xs"
            style={{ color: (form.mark_read ?? true) ? '#22c55e' : 'var(--text-muted)' }}
          >
            {(form.mark_read ?? true) ? <ToggleRight size={16} /> : <ToggleLeft size={16} />}
          </button>
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Max Body Chars</label>
          <input
            type="number"
            value={form.max_body_chars || 50000}
            onChange={e => update('max_body_chars', parseInt(e.target.value) || 50000)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
      </div>
    </div>
  )
}

function SlackConfigFields({ form, setForm }) {
  return (
    <div className="space-y-3">
      <div>
        <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Mode</label>
        <select
          value={form.mode || 'socket'}
          onChange={e => setForm({ ...form, mode: e.target.value })}
          className="w-full px-3 py-2 rounded-lg text-xs outline-none"
          style={inputStyle}
        >
          <option value="socket">Socket Mode (WebSocket, no public URL)</option>
          <option value="events">Events API (HTTP webhooks, requires public URL)</option>
        </select>
      </div>
      <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
        Socket mode requires Bot Token + App-Level Token. Events mode requires Bot Token + Signing Secret.
        OAuth fields (Client ID, Client Secret) are only needed for multi-workspace installs.
      </p>
    </div>
  )
}
