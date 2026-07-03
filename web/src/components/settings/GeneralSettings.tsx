import { useState, useEffect, useCallback } from 'react'
import { Save, ToggleLeft, ToggleRight, Loader2, AlertTriangle } from 'lucide-react'
import type { SettingsData, WebCapableTools, StandardServer } from './settingsApi'
import * as adminApi from '../../api/platformAdmin'

interface GeneralSettingsProps {
  settings: SettingsData | null
  generalForm: {
    default_provider: string
    default_model: string
    web_search_tool: string
    web_extract_tool: string
    timezone: string
  }
  setGeneralForm: (form: GeneralSettingsProps['generalForm']) => void
  webCapableTools: WebCapableTools
  standardServers: StandardServer[]
  saving: boolean
  onSave: () => void
  onSectionChange?: (section: string) => void
  isPlatform?: boolean
}

export default function GeneralSettings({
  generalForm,
  setGeneralForm,
  webCapableTools,
  standardServers,
  saving,
  onSave,
  onSectionChange,
  isPlatform = false
}: GeneralSettingsProps) {
  return (
    <div className="max-w-xl space-y-6">
      {/* Platform Environment Section (superadmin only) */}
      {isPlatform && <EnvironmentSection />}

      {/* Web Tools Section */}
      <div>
        <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
          Web Tools
        </h4>
        
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
              Web Search Tool
            </label>
            <select
              value={generalForm.web_search_tool}
              onChange={(e) => setGeneralForm({ ...generalForm, web_search_tool: e.target.value })}
              className="w-full px-4 py-2.5 rounded-lg border text-sm"
              style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            >
              <option value="">None (disabled)</option>
              {webCapableTools.webSearch.map(t => (
                <option key={`${t.source}:${t.name}`} value={`${t.source}:${t.name}`}>
                  {t.source} ({t.name})
                </option>
              ))}
            </select>
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
              Used for internet search when finding MCP servers online
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
              Web Extract Tool
            </label>
            <select
              value={generalForm.web_extract_tool}
              onChange={(e) => setGeneralForm({ ...generalForm, web_extract_tool: e.target.value })}
              className="w-full px-4 py-2.5 rounded-lg border text-sm"
              style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            >
              <option value="">None (disabled)</option>
              {webCapableTools.webExtract.map(t => (
                <option key={`${t.source}:${t.name}`} value={`${t.source}:${t.name}`}>
                  {t.source} ({t.name})
                </option>
              ))}
            </select>
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
              Used to extract content from URLs when user provides a link
            </p>
          </div>

          {/* Quick setup hint if no web tools configured */}
          {!generalForm.web_search_tool && !generalForm.web_extract_tool && standardServers.some(s => !s.installed) && (
            <p className="text-xs p-2 rounded" style={{ 
              color: 'var(--text-muted)', 
              background: 'rgba(168, 85, 247, 0.1)',
              border: '1px solid rgba(168, 85, 247, 0.2)'
            }}>
              No web tools configured. Go to the <button 
                onClick={() => onSectionChange && onSectionChange('mcp')}
                className="underline font-medium"
                style={{ color: 'var(--accent)' }}
              >MCP Servers</button> section to quick-install a web search provider.
            </p>
          )}
        </div>
      </div>

      {/* Timezone */}
      <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-primary)' }}>
        <h3 className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>Timezone</h3>
        <div>
          <label className="block text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
            IANA Timezone
          </label>
          <input
            type="text"
            value={generalForm.timezone}
            onChange={(e) => setGeneralForm({ ...generalForm, timezone: e.target.value })}
            placeholder="e.g. America/Sao_Paulo (leave empty for system default)"
            className="w-full px-4 py-2.5 rounded-lg border text-sm"
            style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
          />
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Used for scheduling and time display. Must be a valid IANA timezone identifier.
          </p>
        </div>
      </div>

      <button
        onClick={onSave}
        disabled={saving}
        className="flex items-center gap-2 px-4 py-2 rounded-lg text-white font-medium transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
        style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
      >
        <Save size={16} />
        {saving ? 'Saving...' : 'Save Changes'}
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// EnvironmentSection — toggles the "Development Environment" platform flag.
// Uses the same /api/platform/admin/auth-settings endpoint as AuthTab.
// ---------------------------------------------------------------------------

function EnvironmentSection() {
  const [devEnvironment, setDevEnvironment] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await adminApi.getPlatformAuthSettings()
      setDevEnvironment(data.dev_environment)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  // Auto-dismiss messages
  useEffect(() => {
    if (success) { const t = setTimeout(() => setSuccess(''), 3000); return () => clearTimeout(t) }
  }, [success])
  useEffect(() => {
    if (error) { const t = setTimeout(() => setError(''), 5000); return () => clearTimeout(t) }
  }, [error])

  const handleToggle = async () => {
    setSaving(true)
    try {
      const newValue = !devEnvironment
      const updated = await adminApi.savePlatformAuthSettings({ dev_environment: newValue })
      setDevEnvironment(updated.dev_environment)
      setSuccess(`Development environment ${newValue ? 'enabled' : 'disabled'}`)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mb-2">
      <h4 className="text-sm font-medium mb-3" style={{ color: 'var(--text-primary)' }}>
        Environment
      </h4>

      {error && (
        <p className="text-xs p-2 rounded mb-3" style={{ color: '#dc2626', background: 'rgba(220, 38, 38, 0.1)', border: '1px solid rgba(220, 38, 38, 0.2)' }}>
          {error}
        </p>
      )}
      {success && (
        <p className="text-xs p-2 rounded mb-3" style={{ color: '#16a34a', background: 'rgba(22, 163, 74, 0.1)', border: '1px solid rgba(22, 163, 74, 0.2)' }}>
          {success}
        </p>
      )}

      {loading ? (
        <div className="flex items-center gap-2 py-4">
          <Loader2 size={16} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>Loading...</span>
        </div>
      ) : (
        <div
          className="flex items-center justify-between p-4 rounded-xl"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
        >
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg" style={{ background: devEnvironment ? 'rgba(245, 158, 11, 0.1)' : 'rgba(107, 114, 128, 0.1)' }}>
              <AlertTriangle size={16} style={{ color: devEnvironment ? '#f59e0b' : 'var(--text-muted)' }} />
            </div>
            <div>
              <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Development environment</div>
              <div className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                When enabled, all outbound emails include a warning banner indicating this is a development instance that may be unstable.
              </div>
            </div>
          </div>
          <button
            onClick={handleToggle}
            disabled={saving}
            className="p-1.5 rounded-lg transition-opacity hover:opacity-80 flex-shrink-0 ml-4"
            title={devEnvironment ? 'Disable dev environment banner' : 'Enable dev environment banner'}
            style={{ color: devEnvironment ? '#f59e0b' : 'var(--text-muted)' }}
          >
            {saving ? (
              <Loader2 size={20} className="animate-spin" />
            ) : devEnvironment ? (
              <ToggleRight size={24} />
            ) : (
              <ToggleLeft size={24} />
            )}
          </button>
        </div>
      )}
    </div>
  )
}
