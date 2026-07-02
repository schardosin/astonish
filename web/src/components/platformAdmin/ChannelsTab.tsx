import { useState, useEffect, useCallback, type ChangeEvent } from 'react'
import { Trash2, Loader2, ToggleLeft, ToggleRight } from 'lucide-react'
import * as adminApi from '../../api/platformAdmin'
import type { ChannelFullInfo } from '../../api/platformAdmin'
import { InlineError, InlineSuccess, inputStyle } from './shared'

// ---------------------------------------------------------------------------
// Channels Tab
// ---------------------------------------------------------------------------

export default function ChannelsTab() {
  const [channels, setChannels] = useState<ChannelFullInfo[]>([])
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<string>('')
  const [success, setSuccess] = useState<string>('')
  const [expandedChannel, setExpandedChannel] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await adminApi.listChannels()
      setChannels(data || [])
    } catch (e) {
      setError((e as Error).message)
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
            onSaved={(msg: string) => { setSuccess(msg); setError(''); load() }}
            onError={(msg: string) => { setError(msg); setSuccess('') }}
            onDeleted={(msg: string) => { setSuccess(msg); setError(''); load() }}
          />
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Channel Card
// ---------------------------------------------------------------------------

interface ChannelCardProps {
  channel: ChannelFullInfo
  expanded: boolean
  onToggle: () => void
  onSaved: (msg: string) => void
  onError: (msg: string) => void
  onDeleted: (msg: string) => void
}

function ChannelCard({ channel, expanded, onToggle, onSaved, onError, onDeleted }: ChannelCardProps) {
  const [form, setForm] = useState<Record<string, any>>({})
  const [secrets, setSecrets] = useState<Record<string, string>>({})
  const [enabled, setEnabled] = useState<boolean>(channel.enabled)
  const [saving, setSaving] = useState<boolean>(false)
  const [testing, setTesting] = useState<boolean>(false)
  const [testResult, setTestResult] = useState<{ status: string; message: string; email?: string } | null>(null)

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
      onError((e as Error).message)
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
      onError((e as Error).message)
    }
  }

  const handleTestConnection = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const result = await adminApi.testEmailConnection()
      setTestResult(result)
    } catch (e) {
      setTestResult({ status: 'error', message: (e as Error).message })
    } finally {
      setTesting(false)
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
                    onChange={(e: ChangeEvent<HTMLInputElement>) => setSecrets(prev => ({ ...prev, [s.key]: e.target.value }))}
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
            <div className="flex items-center gap-2">
              {channel.type === 'email' && (
                <button onClick={handleTestConnection} disabled={testing} className="px-3 py-2 rounded-lg text-xs font-medium" style={{ color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}>
                  {testing ? <Loader2 size={12} className="animate-spin inline mr-1" /> : null}
                  {testing ? 'Testing...' : 'Test Connection'}
                </button>
              )}
              <button onClick={handleSave} disabled={saving} className="px-4 py-2 rounded-lg text-xs font-medium text-white" style={{ background: 'var(--accent)' }}>
                {saving ? <Loader2 size={12} className="animate-spin inline mr-1" /> : null}
                Save & Apply
              </button>
            </div>
          </div>
          {/* Test result */}
          {testResult && (
            <div className="text-xs px-2 py-1 rounded" style={{
              color: testResult.status === 'ok' ? '#22c55e' : '#ef4444',
              background: testResult.status === 'ok' ? '#22c55e15' : '#ef444415',
            }}>
              {testResult.message}{testResult.email ? ' (' + testResult.email + ')' : ''}
            </div>
          )}
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

interface EmailConfigFieldsProps {
  form: Record<string, any>
  setForm: (form: Record<string, any>) => void
}

function EmailConfigFields({ form, setForm }: EmailConfigFieldsProps) {
  const update = (key: string, value: string | number | boolean) => setForm({ ...form, [key]: value })
  const isMSGraph = (form.provider || 'imap') === 'msgraph'

  return (
    <div className="space-y-3">
      {/* Provider selection - prominent at the top */}
      <div>
        <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Provider</label>
        <select
          value={form.provider || 'imap'}
          onChange={(e: ChangeEvent<HTMLSelectElement>) => update('provider', e.target.value)}
          className="w-full px-3 py-2 rounded-lg text-xs outline-none"
          style={inputStyle}
        >
          <option value="imap">IMAP/SMTP</option>
          <option value="gmail">Gmail (IMAP/SMTP)</option>
          <option value="msgraph">Microsoft 365 (Graph API)</option>
        </select>
      </div>

      {/* IMAP/SMTP fields (hidden for msgraph) */}
      {!isMSGraph && (
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>IMAP Server</label>
            <input
              type="text"
              value={form.imap_server || ''}
              onChange={(e: ChangeEvent<HTMLInputElement>) => update('imap_server', e.target.value)}
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
              onChange={(e: ChangeEvent<HTMLInputElement>) => update('smtp_server', e.target.value)}
              placeholder="smtp.gmail.com:587"
              className="w-full px-3 py-2 rounded-lg text-xs outline-none"
              style={inputStyle}
            />
          </div>
        </div>
      )}

      {/* Email address + Username (username hidden for msgraph) */}
      <div className={isMSGraph ? '' : 'grid grid-cols-2 gap-3'}>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Email Address</label>
          <input
            type="text"
            value={form.address || ''}
            onChange={(e: ChangeEvent<HTMLInputElement>) => update('address', e.target.value)}
            placeholder="agent@example.com"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        {!isMSGraph && (
          <div>
            <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Username</label>
            <input
              type="text"
              value={form.username || ''}
              onChange={(e: ChangeEvent<HTMLInputElement>) => update('username', e.target.value)}
              placeholder="(defaults to address)"
              className="w-full px-3 py-2 rounded-lg text-xs outline-none"
              style={inputStyle}
            />
          </div>
        )}
      </div>

      {/* Microsoft Graph credential field */}
      {isMSGraph && (
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Credential Name</label>
          <input
            type="text"
            value={form.credential || ''}
            onChange={(e: ChangeEvent<HTMLInputElement>) => update('credential', e.target.value)}
            placeholder="email-microsoft"
            className="w-full px-3 py-2 rounded-lg text-xs outline-none font-mono"
            style={inputStyle}
          />
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Name of the credential (type: oauth_authorization_code) containing your
            Microsoft 365 refresh token, client ID, and client secret.
          </p>
        </div>
      )}

      {/* Behavior settings */}
      <div className="grid grid-cols-3 gap-3">
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Poll Interval (sec)</label>
          <input
            type="number"
            value={form.poll_interval || 30}
            onChange={(e: ChangeEvent<HTMLInputElement>) => update('poll_interval', parseInt(e.target.value) || 30)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Folder</label>
          <input
            type="text"
            value={form.folder || 'INBOX'}
            onChange={(e: ChangeEvent<HTMLInputElement>) => update('folder', e.target.value)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
        <div>
          <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Max Body Chars</label>
          <input
            type="number"
            value={form.max_body_chars || 50000}
            onChange={(e: ChangeEvent<HTMLInputElement>) => update('max_body_chars', parseInt(e.target.value) || 50000)}
            className="w-full px-3 py-2 rounded-lg text-xs outline-none"
            style={inputStyle}
          />
        </div>
      </div>
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
    </div>
  )
}

interface SlackConfigFieldsProps {
  form: Record<string, any>
  setForm: (form: Record<string, any>) => void
}

function SlackConfigFields({ form, setForm }: SlackConfigFieldsProps) {
  return (
    <div className="space-y-3">
      <div>
        <label className="block text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>Mode</label>
        <select
          value={form.mode || 'socket'}
          onChange={(e: ChangeEvent<HTMLSelectElement>) => setForm({ ...form, mode: e.target.value })}
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
