import { Save } from 'lucide-react'
import type { SettingsData, WebCapableTools, StandardServer } from './settingsApi'

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
}

export default function GeneralSettings({
  generalForm,
  setGeneralForm,
  webCapableTools,
  standardServers,
  saving,
  onSave,
  onSectionChange
}: GeneralSettingsProps) {
  return (
    <div className="max-w-xl space-y-6">
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
