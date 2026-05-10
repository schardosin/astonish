import { useState, useEffect, useCallback, lazy, Suspense, type FormEvent } from 'react'
import { ChevronRight, Download, Plus, Trash2, UserPlus, AlertCircle, Loader2, Key } from 'lucide-react'
import SettingsContent from './settings/SettingsContent'
import { PREFERENCE_ITEMS, RESOURCE_ITEMS, TEAM_ITEMS, ORG_ITEMS, PLATFORM_ITEMS, SYSTEM_ITEMS } from './settings/settingsMenuItems'
import type { SettingsMenuItem } from './settings/settingsMenuItems'
import { useSettingsData } from '../hooks/useSettingsData'
import type { UpdateInfo, MCPServerConfig, SettingsData, ProviderInfo } from './settings/settingsApi'
import { fetchMCPConfig, fetchPlatformProviders, fetchOrgProviders, fetchTeamProviders, fetchSettings, savePlatformProviders, saveOrgProviders, saveSettings as saveTeamSettings } from './settings/settingsApi'
import {
  fetchTeams, createTeam, deleteTeam,
  fetchTeamMembers, addTeamMember, removeTeamMember, setTeamMemberRole,
  type Team, type TeamMember,
} from '../api/platform'

declare const __UI_VERSION__: string

// Lazy-loaded sub-views
const UserManagement = lazy(() => import('./UserManagement'))
const AuditViewer = lazy(() => import('./AuditViewer'))
const SkillsSettings = lazy(() => import('./settings/SkillsSettings'))
const MCPServersSettings = lazy(() => import('./settings/MCPServersSettings'))
const ProvidersSettings = lazy(() => import('./settings/ProvidersSettings'))
const SchedulerSettings = lazy(() => import('./settings/SchedulerSettings'))
const TapsSettings = lazy(() => import('./settings/TapsSettings'))
const FlowStorePanel = lazy(() => import('./FlowStorePanel'))
const TeamContainerTab = lazy(() => import('./TeamContainerTab'))
const PlatformAdminPanel = lazy(() => import('./PlatformAdminPanel'))
const KnowledgeBrowser = lazy(() => import('./KnowledgeBrowser'))

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SettingsPageProps {
  activeSection?: string
  onSectionChange?: (section: string) => void
  onToolsRefresh?: () => void
  onSettingsSaved?: () => void
  updateAvailable?: UpdateInfo | null
  onUpdateClick?: (() => void) | null
  appVersion?: string
  theme?: string
  isPlatformMode?: boolean
  userRole?: string
  /** User's platform-level role (e.g. 'superadmin') */
  platformRole?: string
  /** Current user info (for team management) */
  user?: { id: string; email: string; display_name: string; role: string; platform_role?: string } | null
  /** Current org info */
  org?: { id: string; name: string; slug: string } | null
  /** Active team from TopBar context */
  activeTeam?: string | null
  /** All teams available to the user */
  teams?: { slug: string; name: string }[] | null
  /** URL params for team section */
  teamSlug?: string
  teamTab?: string
  /** URL params for org/platform sub-sections */
  subsection?: string
}

interface MenuCategory {
  label?: string
  items: SettingsMenuItem[]
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const slugify = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
const errMsg = (err: unknown, fallback: string) => err instanceof Error ? err.message : fallback
const gradientPurple = { background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }
const inputStyle = { background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }
const errorBg = { background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }

function InlineError({ msg }: { msg: string }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={errorBg}>
      <AlertCircle size={14} /><span>{msg}</span>
    </div>
  )
}

// ---------------------------------------------------------------------------
// MembersPanel — team member management
// ---------------------------------------------------------------------------

interface MembersPanelProps {
  user: { id: string; email: string; display_name: string; role: string }
  team: Team
  canManageTeam: boolean
}

function MembersPanel({ user, team, canManageTeam }: MembersPanelProps) {
  const [members, setMembers] = useState<TeamMember[]>([])
  const [membersLoading, setMembersLoading] = useState(true)
  const [membersError, setMembersError] = useState('')
  const [showAddMember, setShowAddMember] = useState(false)
  const [addEmail, setAddEmail] = useState('')
  const [addRole, setAddRole] = useState('member')
  const [addNotify, setAddNotify] = useState(true)
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState('')

  const loadMembers = useCallback(async () => {
    setMembersLoading(true); setMembersError('')
    try {
      const resp = await fetchTeamMembers(team.slug)
      setMembers(resp.members)
    }
    catch (err) { setMembersError(errMsg(err, 'Failed to load members')) }
    finally { setMembersLoading(false) }
  }, [team.slug])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setMembersLoading(true); setMembersError('')
      try {
        const resp = await fetchTeamMembers(team.slug)
        if (cancelled) return
        setMembers(resp.members)
      }
      catch (err) { if (!cancelled) setMembersError(errMsg(err, 'Failed to load members')) }
      finally { if (!cancelled) setMembersLoading(false) }
    }
    load()
    return () => { cancelled = true }
  }, [team.slug])

  const handleAddMember = async (e: FormEvent) => {
    e.preventDefault()
    setAdding(true); setAddError('')
    try {
      await addTeamMember(team.slug, addEmail, addRole, addNotify)
      setShowAddMember(false); setAddEmail(''); setAddRole('member'); setAddNotify(true)
      await loadMembers()
    } catch (err) { setAddError(errMsg(err, 'Failed to add member')) }
    finally { setAdding(false) }
  }

  const handleRemoveMember = async (userId: string) => {
    try { await removeTeamMember(team.slug, userId); await loadMembers() }
    catch (err) { setMembersError(errMsg(err, 'Failed to remove member')) }
  }

  const handleRoleChange = async (userId: string, role: string) => {
    try { await setTeamMemberRole(team.slug, userId, role); await loadMembers() }
    catch (err) { setMembersError(errMsg(err, 'Failed to update role')) }
  }

  return (
    <div className="h-full flex flex-col overflow-hidden">
      {canManageTeam && (
        <div className="flex items-center justify-end px-6 py-3 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <button onClick={() => setShowAddMember(true)} className="flex items-center gap-2 px-4 py-2 rounded-xl text-white text-sm font-medium hover:opacity-90" style={gradientPurple}>
            <UserPlus size={16} />Add Member
          </button>
        </div>
      )}

      {showAddMember && (
        <form onSubmit={handleAddMember} className="flex items-center gap-3 px-6 py-3 border-b flex-wrap" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <input type="email" value={addEmail} onChange={e => setAddEmail(e.target.value)} placeholder="Email address" required className="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle} autoFocus />
          <select value={addRole} onChange={e => setAddRole(e.target.value)} className="px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
          <label className="flex items-center gap-1.5 text-xs cursor-pointer" style={{ color: 'var(--text-secondary)' }}>
            <input type="checkbox" checked={addNotify} onChange={e => setAddNotify(e.target.checked)} className="rounded" />
            Notify
          </label>
          <button type="submit" disabled={adding} className="px-4 py-2 rounded-lg text-white text-sm font-medium hover:opacity-90 disabled:opacity-50" style={gradientPurple}>
            {adding ? <Loader2 size={16} className="animate-spin" /> : 'Add'}
          </button>
          <button type="button" onClick={() => { setShowAddMember(false); setAddError('') }} className="px-3 py-2 rounded-lg text-sm" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
          {addError && <span className="text-xs flex items-center gap-1" style={{ color: 'var(--danger)' }}><AlertCircle size={12} />{addError}</span>}
        </form>
      )}

      {membersError && <div className="mx-6 mt-3"><InlineError msg={membersError} /></div>}

      <div className="flex-1 overflow-y-auto px-6 py-4">
        {membersLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : members.length === 0 ? (
          <p className="text-center py-12 text-sm" style={{ color: 'var(--text-muted)' }}>No members in this team.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <th className="text-left py-2 px-3 font-medium">Email</th>
                <th className="text-left py-2 px-3 font-medium">Name</th>
                <th className="text-left py-2 px-3 font-medium">Role</th>
                <th className="text-left py-2 px-3 font-medium">Joined</th>
                {canManageTeam && <th className="w-10" />}
              </tr>
            </thead>
            <tbody>
              {members.map(m => (
                <tr key={m.user_id} className="border-t" style={{ borderColor: 'var(--border-color)' }}>
                  <td className="py-2.5 px-3" style={{ color: 'var(--text-primary)' }}>{m.email}</td>
                  <td className="py-2.5 px-3" style={{ color: 'var(--text-secondary)' }}>{m.display_name}</td>
                  <td className="py-2.5 px-3">
                    {canManageTeam ? (
                      <select value={m.role} onChange={e => handleRoleChange(m.user_id, e.target.value)} className="px-2 py-1 rounded text-xs outline-none" style={inputStyle}>
                        <option value="viewer">viewer</option>
                        <option value="member">member</option>
                        <option value="admin">admin</option>
                      </select>
                    ) : (
                      <span className="px-2 py-0.5 rounded text-xs" style={{ background: 'var(--accent-soft)', color: 'var(--accent)' }}>{m.role}</span>
                    )}
                  </td>
                  <td className="py-2.5 px-3" style={{ color: 'var(--text-muted)' }}>{new Date(m.joined_at).toLocaleDateString()}</td>
                  {canManageTeam && (
                    <td className="py-2.5 px-3">
                      {m.user_id !== user.id && (
                        <button onClick={() => handleRemoveMember(m.user_id)} className="p-1 rounded hover:opacity-80 transition-opacity" style={{ color: 'var(--danger)' }} title="Remove member">
                          <Trash2 size={14} />
                        </button>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// TeamMCPServersTab — wraps MCPServersSettings with team-scoped data
// ---------------------------------------------------------------------------

function TeamMCPServersTab({ teamSlug, theme }: { teamSlug: string; theme: string }) {
  const [mcpServers, setMcpServers] = useState<Record<string, MCPServerConfig>>({})
  const [mcpServerNames, setMcpServerNames] = useState<Record<string, string>>({})
  const [mcpServerArgs, setMcpServerArgs] = useState<Record<string, string>>({})
  const [, setMcpHasChanges] = useState(false)
  const [saving, setSaving] = useState(false)
  const [, setSaveSuccess] = useState(false)
  const [, setError] = useState<string | null>(null)

  // Org-level MCP servers (inherited, read-only)
  const [orgServers, setOrgServers] = useState<Record<string, MCPServerConfig>>({})

  const loadData = useCallback(async () => {
    try {
      const data = await fetchMCPConfig(teamSlug)
      const servers = data.mcpServers || {}
      setMcpServers(servers)
      const names: Record<string, string> = {}
      const args: Record<string, string> = {}
      Object.entries(servers).forEach(([name, server]) => {
        names[name] = name
        args[name] = (server.args || []).join(', ')
      })
      setMcpServerNames(names)
      setMcpServerArgs(args)
    } catch (err) {
      console.error('Failed to load team MCP config:', err)
    }
  }, [teamSlug])

  const loadOrgServers = useCallback(async () => {
    try {
      // Fetch org-level MCP config (no scope=team)
      const data = await fetchMCPConfig()
      setOrgServers(data.mcpServers || {})
    } catch {
      // Org servers are optional/inherited — ignore errors
    }
  }, [])

  useEffect(() => { loadData(); loadOrgServers() }, [loadData, loadOrgServers])

  const orgServerEntries = Object.entries(orgServers)

  return (
    <div className="space-y-6">
      {/* Org-level inherited MCP servers (read-only) */}
      {orgServerEntries.length > 0 && (
        <div>
          <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
            Organization (inherited)
          </div>
          <div className="space-y-2">
            {orgServerEntries.map(([name, server]) => (
              <div
                key={name}
                className="flex items-center justify-between p-3 rounded-lg"
                style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', opacity: 0.85 }}
              >
                <div className="flex items-center gap-3 min-w-0">
                  <div
                    className="w-2 h-2 rounded-full shrink-0"
                    style={{ background: server.enabled !== false ? '#22c55e' : '#6b7280' }}
                  />
                  <div className="min-w-0">
                    <div className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>{name}</div>
                    <div className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>
                      {server.transport === 'sse' || server.url ? server.url || 'SSE' : `${server.command || ''} ${(server.args || []).join(' ')}`.trim() || 'No command'}
                    </div>
                  </div>
                </div>
                <span className="text-[10px] px-2 py-0.5 rounded-full shrink-0" style={{ background: 'rgba(59, 130, 246, 0.15)', color: '#3b82f6' }}>
                  org
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Team-level MCP servers (editable) */}
      {orgServerEntries.length > 0 && (
        <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
          Team
        </div>
      )}
      <MCPServersSettings
        mcpServers={mcpServers}
        setMcpServers={setMcpServers}
        mcpServerNames={mcpServerNames}
        setMcpServerNames={setMcpServerNames}
        mcpServerArgs={mcpServerArgs}
        setMcpServerArgs={setMcpServerArgs}
        setMcpHasChanges={setMcpHasChanges}
        standardServers={[]}
        saving={saving}
        setSaving={setSaving}
        setSaveSuccess={setSaveSuccess}
        setError={setError}
        onToolsRefresh={loadData}
        loadData={loadData}
        setGeneralForm={() => {}}
        theme={theme}
        teamSlug={teamSlug}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// OrgMCPServersTab — wraps MCPServersSettings with org-scoped data
// ---------------------------------------------------------------------------

function OrgMCPServersTab({ theme }: { theme: string }) {
  const [mcpServers, setMcpServers] = useState<Record<string, MCPServerConfig>>({})
  const [mcpServerNames, setMcpServerNames] = useState<Record<string, string>>({})
  const [mcpServerArgs, setMcpServerArgs] = useState<Record<string, string>>({})
  const [, setMcpHasChanges] = useState(false)
  const [saving, setSaving] = useState(false)
  const [, setSaveSuccess] = useState(false)
  const [, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      // No teamSlug → fetches org-level MCP config
      const data = await fetchMCPConfig()
      const servers = data.mcpServers || {}
      setMcpServers(servers)
      const names: Record<string, string> = {}
      const args: Record<string, string> = {}
      Object.entries(servers).forEach(([name, server]) => {
        names[name] = name
        args[name] = (server.args || []).join(', ')
      })
      setMcpServerNames(names)
      setMcpServerArgs(args)
    } catch (err) {
      console.error('Failed to load org MCP config:', err)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  return (
    <MCPServersSettings
      mcpServers={mcpServers}
      setMcpServers={setMcpServers}
      mcpServerNames={mcpServerNames}
      setMcpServerNames={setMcpServerNames}
      mcpServerArgs={mcpServerArgs}
      setMcpServerArgs={setMcpServerArgs}
      setMcpHasChanges={setMcpHasChanges}
      standardServers={[]}
      saving={saving}
      setSaving={setSaving}
      setSaveSuccess={setSaveSuccess}
      setError={setError}
      onToolsRefresh={loadData}
      loadData={loadData}
      setGeneralForm={() => {}}
      theme={theme}
    />
  )
}

// ---------------------------------------------------------------------------
// PlatformProvidersTab — wraps ProvidersSettings for platform-level provider management (superadmin)
// ---------------------------------------------------------------------------

function PlatformProvidersTab() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [providerForms, setProviderForms] = useState<Record<string, Record<string, string>>>({})
  const [generalForm, setGeneralForm] = useState({ default_provider: '', default_model: '' })
  const [saving, setSaving] = useState(false)
  const [, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      const data = await fetchPlatformProviders()
      const providers: ProviderInfo[] = []
      if (data.providers) {
        for (const [name, fields] of Object.entries(data.providers)) {
          const type = fields?.type || name
          const fieldsCopy = { ...fields }
          delete fieldsCopy.type
          providers.push({
            name,
            type,
            display_name: type,
            configured: Object.keys(fieldsCopy).length > 0,
            fields: fieldsCopy
          })
        }
      }
      const settingsData: SettingsData = {
        general: {
          default_provider: data.default_provider || '',
          default_model: data.default_model || '',
          web_search_tool: '',
          web_extract_tool: '',
          timezone: ''
        },
        providers
      }
      setSettings(settingsData)
      setGeneralForm({ default_provider: data.default_provider || '', default_model: data.default_model || '' })
      const pForms: Record<string, Record<string, string>> = {}
      providers.forEach(p => { pForms[p.name] = { ...p.fields } })
      setProviderForms(pForms)
    } catch (err) {
      console.error('Failed to load platform providers:', err)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  return (
    <ProvidersSettings
      settings={settings}
      providerForms={providerForms}
      setProviderForms={setProviderForms}
      generalForm={generalForm}
      setGeneralForm={setGeneralForm}
      saving={saving}
      setSaving={setSaving}
      setSaveSuccess={setSaveSuccess}
      error={error}
      setError={setError}
      loadData={loadData}
      level="platform"
      inheritedProviders={[]}
      onSaveDefault={async (provider, model) => {
        await savePlatformProviders({ default_provider: provider, default_model: model })
        loadData()
      }}
    />
  )
}

// ---------------------------------------------------------------------------
// OrgProvidersTab — wraps ProvidersSettings with org-scoped data + inherited platform providers
// ---------------------------------------------------------------------------

function OrgProvidersTab() {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [providerForms, setProviderForms] = useState<Record<string, Record<string, string>>>({})
  const [generalForm, setGeneralForm] = useState({ default_provider: '', default_model: '' })
  const [saving, setSaving] = useState(false)
  const [, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Inherited providers (read-only)
  const [platformProviders, setPlatformProviders] = useState<{ name: string; type: string; configured: boolean }[]>([])
  // Platform-level defaults for inheritance display
  const [platformDefaults, setPlatformDefaults] = useState<{ provider: string; model: string }>({ provider: '', model: '' })

  const loadData = useCallback(async () => {
    // Load org-level settings (raw, no cascade)
    try {
      const data = await fetchOrgProviders()
      const providers: ProviderInfo[] = []
      if (data.providers) {
        for (const [name, fields] of Object.entries(data.providers)) {
          const type = fields?.type || name
          const fieldsCopy = { ...fields }
          delete fieldsCopy.type
          providers.push({
            name,
            type,
            display_name: type,
            configured: Object.keys(fieldsCopy).length > 0,
            fields: fieldsCopy
          })
        }
      }
      const settingsData: SettingsData = {
        general: {
          default_provider: data.default_provider || '',
          default_model: data.default_model || '',
          web_search_tool: '',
          web_extract_tool: '',
          timezone: ''
        },
        providers
      }
      setSettings(settingsData)
      // Show only the org's explicit values (empty = "Not Set")
      setGeneralForm({
        default_provider: data.default_provider || '',
        default_model: data.default_model || ''
      })
      const pForms: Record<string, Record<string, string>> = {}
      providers.forEach(p => { pForms[p.name] = { ...p.fields } })
      setProviderForms(pForms)
    } catch (err) {
      console.error('Failed to load org providers:', err)
    }

    // Load platform providers (for inherited list + defaults display)
    try {
      const platformData = await fetchPlatformProviders()
      const items: { name: string; type: string; configured: boolean }[] = []
      if (platformData.providers) {
        for (const [name, fields] of Object.entries(platformData.providers)) {
          const type = fields?.type || name
          const fieldsCopy = { ...fields }
          delete fieldsCopy.type
          items.push({ name, type, configured: Object.keys(fieldsCopy).length > 0 })
        }
      }
      setPlatformProviders(items)
      setPlatformDefaults({
        provider: platformData.default_provider || '',
        model: platformData.default_model || ''
      })
    } catch {
      // Platform providers are optional
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  return (
    <div className="space-y-6">
      {/* Inherited platform providers (read-only) */}
      {platformProviders.length > 0 && (
        <div>
          <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
            Platform (inherited)
          </div>
          <div className="space-y-2">
            {platformProviders.map(p => (
              <div
                key={p.name}
                className="flex items-center justify-between p-3 rounded-lg"
                style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', opacity: 0.85 }}
              >
                <div className="flex items-center gap-3 min-w-0">
                  <div className="w-8 h-8 rounded-lg flex items-center justify-center shrink-0"
                    style={{ background: 'rgba(168, 85, 247, 0.15)', border: '1px solid rgba(168, 85, 247, 0.2)' }}>
                    <Key size={14} style={{ color: '#a855f7' }} />
                  </div>
                  <div className="min-w-0">
                    <div className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>{p.name}</div>
                    <div className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>{p.type}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {generalForm.default_provider === p.name && (
                    <span className="text-[10px] px-2 py-0.5 rounded-full"
                      style={{ background: 'rgba(168, 85, 247, 0.2)', color: '#a855f7' }}>
                      default
                    </span>
                  )}
                  <span className="text-[10px] px-2 py-0.5 rounded-full"
                    style={{ background: 'rgba(59, 130, 246, 0.15)', color: '#3b82f6' }}>
                    platform
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Org-level providers header (shown when there are inherited items) */}
      {platformProviders.length > 0 && (
        <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
          Organization
        </div>
      )}

      {/* Org-level providers (editable) */}
      <ProvidersSettings
        settings={settings}
        providerForms={providerForms}
        setProviderForms={setProviderForms}
        generalForm={generalForm}
        setGeneralForm={setGeneralForm}
        saving={saving}
        setSaving={setSaving}
        setSaveSuccess={setSaveSuccess}
        error={error}
        setError={setError}
        loadData={loadData}
        level="org"
        inheritedProviders={platformProviders.map(p => ({ name: p.name, type: p.type, level: 'platform' }))}
        inheritedDefaults={platformDefaults.provider || platformDefaults.model ? { provider: platformDefaults.provider, model: platformDefaults.model, source: 'Platform' } : undefined}
        onSaveDefault={async (provider, model) => {
          await saveOrgProviders({ default_provider: provider, default_model: model })
          loadData()
        }}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// TeamProvidersTab — wraps ProvidersSettings with team-scoped data + inherited platform/org providers
// ---------------------------------------------------------------------------

function TeamProvidersTab({ teamSlug }: { teamSlug: string }) {
  const [settings, setSettings] = useState<SettingsData | null>(null)
  const [providerForms, setProviderForms] = useState<Record<string, Record<string, string>>>({})
  const [generalForm, setGeneralForm] = useState({ default_provider: '', default_model: '' })
  const [saving, setSaving] = useState(false)
  const [, setSaveSuccess] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Inherited providers (read-only)
  const [inheritedProviders, setInheritedProviders] = useState<{ name: string; type: string; configured: boolean; level: string }[]>([])
  // Inherited defaults for display
  const [inheritedDefaults, setInheritedDefaults] = useState<{ provider: string; model: string; source: string }>({ provider: '', model: '', source: '' })

  const loadData = useCallback(async () => {
    // Load effective settings for the full provider list
    try {
      const data = await fetchSettings()
      setSettings(data)
      const pForms: Record<string, Record<string, string>> = {}
      data.providers.forEach(p => { pForms[p.name] = { ...p.fields } })
      setProviderForms(pForms)
    } catch (err) {
      console.error('Failed to load team settings:', err)
    }

    // Load raw team-level defaults (no cascade) for generalForm
    try {
      const teamData = await fetchTeamProviders()
      setGeneralForm({
        default_provider: teamData.default_provider || '',
        default_model: teamData.default_model || ''
      })
    } catch {
      // If team providers endpoint fails, defaults stay empty (= "Not Set")
      setGeneralForm({ default_provider: '', default_model: '' })
    }
  }, [teamSlug])

  const loadInheritedProviders = useCallback(async () => {
    const inherited: { name: string; type: string; configured: boolean; level: string }[] = []
    let platformDefaultProvider = ''
    let platformDefaultModel = ''
    let orgDefaultProvider = ''
    let orgDefaultModel = ''

    try {
      const platformData = await fetchPlatformProviders()
      platformDefaultProvider = platformData.default_provider || ''
      platformDefaultModel = platformData.default_model || ''
      if (platformData.providers) {
        for (const [name, fields] of Object.entries(platformData.providers)) {
          const type = fields?.type || name
          const fieldsCopy = { ...fields }
          delete fieldsCopy.type
          inherited.push({ name, type, configured: Object.keys(fieldsCopy).length > 0, level: 'platform' })
        }
      }
    } catch { /* ignore */ }
    try {
      const orgData = await fetchOrgProviders()
      orgDefaultProvider = orgData.default_provider || ''
      orgDefaultModel = orgData.default_model || ''
      if (orgData.providers) {
        for (const [name, fields] of Object.entries(orgData.providers)) {
          const type = fields?.type || name
          const fieldsCopy = { ...fields }
          delete fieldsCopy.type
          // Don't duplicate if already inherited from platform
          if (!inherited.find(i => i.name === name)) {
            inherited.push({ name, type, configured: Object.keys(fieldsCopy).length > 0, level: 'org' })
          }
        }
      }
    } catch { /* ignore */ }
    setInheritedProviders(inherited)

    // Compute effective inherited defaults (org wins over platform)
    const effectiveProvider = orgDefaultProvider || platformDefaultProvider
    const effectiveModel = orgDefaultModel || platformDefaultModel
    const source = orgDefaultProvider || orgDefaultModel ? 'Org' : 'Platform'
    setInheritedDefaults({ provider: effectiveProvider, model: effectiveModel, source })
  }, [])

  useEffect(() => { loadData(); loadInheritedProviders() }, [loadData, loadInheritedProviders])

  // Team-only providers (exclude inherited ones from the editable list)
  const inheritedNames = new Set(inheritedProviders.map(p => p.name))
  const teamOnlySettings: SettingsData | null = settings ? {
    ...settings,
    providers: settings.providers.filter(p => !inheritedNames.has(p.name))
  } : null

  return (
    <div className="space-y-6">
      {/* Inherited providers (read-only) */}
      {inheritedProviders.length > 0 && (
        <div>
          <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
            Inherited
          </div>
          <div className="space-y-2">
            {inheritedProviders.map(p => (
              <div
                key={p.name}
                className="flex items-center justify-between p-3 rounded-lg"
                style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)', opacity: 0.85 }}
              >
                <div className="flex items-center gap-3 min-w-0">
                  <div className="w-8 h-8 rounded-lg flex items-center justify-center shrink-0"
                    style={{ background: 'rgba(168, 85, 247, 0.15)', border: '1px solid rgba(168, 85, 247, 0.2)' }}>
                    <Key size={14} style={{ color: '#a855f7' }} />
                  </div>
                  <div className="min-w-0">
                    <div className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>{p.name}</div>
                    <div className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>{p.type}</div>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  {generalForm.default_provider === p.name && (
                    <span className="text-[10px] px-2 py-0.5 rounded-full"
                      style={{ background: 'rgba(168, 85, 247, 0.2)', color: '#a855f7' }}>
                      default
                    </span>
                  )}
                  <span className="text-[10px] px-2 py-0.5 rounded-full"
                    style={{ background: p.level === 'platform' ? 'rgba(59, 130, 246, 0.15)' : 'rgba(34, 197, 94, 0.15)', color: p.level === 'platform' ? '#3b82f6' : '#22c55e' }}>
                    {p.level}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Team-level providers header (shown when there are inherited items) */}
      {inheritedProviders.length > 0 && (
        <div className="text-xs font-medium mb-2 uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
          Team
        </div>
      )}

      {/* Team-level providers (editable) */}
      <ProvidersSettings
        settings={teamOnlySettings}
        providerForms={providerForms}
        setProviderForms={setProviderForms}
        generalForm={generalForm}
        setGeneralForm={setGeneralForm}
        saving={saving}
        setSaving={setSaving}
        setSaveSuccess={setSaveSuccess}
        error={error}
        setError={setError}
        loadData={loadData}
        level="team"
        inheritedProviders={inheritedProviders.map(p => ({ name: p.name, type: p.type, level: p.level }))}
        inheritedDefaults={inheritedDefaults.provider || inheritedDefaults.model ? inheritedDefaults : undefined}
        onSaveDefault={async (provider, model) => {
          // Load current team settings to preserve other general fields (web tools, etc.)
          try {
            const current = await fetchSettings()
            await saveTeamSettings({
              general: {
                ...current.general,
                default_provider: provider,
                default_model: model
              }
            })
          } catch {
            // Fallback: just save defaults (may clear other fields if settings can't be loaded)
            await saveTeamSettings({ general: { default_provider: provider, default_model: model } })
          }
          loadData()
        }}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// TeamContent — renders the right sub-component for each team tab
// ---------------------------------------------------------------------------

interface TeamContentProps {
  tabId: string
  teamSlug: string
  theme: string
  user: { id: string; email: string; display_name: string; role: string }
  canManageTeam: boolean
  team: Team | null
}

function TeamContent({ tabId, teamSlug, theme, user, canManageTeam, team }: TeamContentProps) {
  const [fullConfig, setFullConfig] = useState<any>(null)
  const [fullConfigLoading, setFullConfigLoading] = useState(false)

  const needsFullConfig = ['skills', 'scheduler'].includes(tabId)

  useEffect(() => {
    if (!needsFullConfig || fullConfig) return
    let cancelled = false
    const load = async () => {
      setFullConfigLoading(true)
      try {
        const { fetchFullConfig } = await import('./settings/settingsApi')
        const data = await fetchFullConfig()
        if (!cancelled) setFullConfig(data)
      } catch (err) {
        console.error('Failed to load config:', err)
      } finally {
        if (!cancelled) setFullConfigLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [needsFullConfig, fullConfig])

  const handleSaved = () => setFullConfig(null)

  if (tabId === 'members' && team) {
    return <MembersPanel user={user} team={team} canManageTeam={canManageTeam} />
  }

  if (tabId === 'knowledge') {
    return (
      <div className="flex-1 overflow-hidden p-6 flex flex-col">
        <Suspense fallback={<div className="flex items-center justify-center py-12"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
          <KnowledgeBrowser theme={theme as 'dark' | 'light'} user={user} activeTeam={teamSlug} />
        </Suspense>
      </div>
    )
  }

  if (tabId === 'container') {
    return (
      <div className="flex-1 overflow-hidden p-6 flex flex-col">
        <Suspense fallback={<div className="flex items-center justify-center py-12"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
          <TeamContainerTab teamSlug={teamSlug} theme={theme as 'dark' | 'light'} canManage={canManageTeam} />
        </Suspense>
      </div>
    )
  }

  if (fullConfigLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
        <span className="ml-2 text-sm" style={{ color: 'var(--text-muted)' }}>Loading...</span>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <Suspense fallback={<div className="flex items-center justify-center py-12"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
        {tabId === 'providers' && <TeamProvidersTab teamSlug={teamSlug} />}
        {tabId === 'skills' && <SkillsSettings config={fullConfig?.skills || null} onSaved={handleSaved} theme={theme} isPlatform canManage={canManageTeam} teamSlug={teamSlug} />}
        {tabId === 'mcp' && <TeamMCPServersTab teamSlug={teamSlug} theme={theme} />}
        {tabId === 'scheduler' && fullConfig && <SchedulerSettings config={fullConfig.scheduler} onSaved={handleSaved} teamSlug={teamSlug} />}
        {tabId === 'taps' && <TapsSettings teamSlug={teamSlug} />}
        {tabId === 'flows' && <FlowStorePanel teamSlug={teamSlug} canManage={canManageTeam} />}
      </Suspense>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main SettingsPage
// ---------------------------------------------------------------------------

export default function SettingsPage({
  activeSection: externalActiveSection,
  onSectionChange,
  onToolsRefresh,
  onSettingsSaved,
  updateAvailable = null,
  onUpdateClick = null,
  appVersion = 'dev',
  theme = 'dark',
  isPlatformMode = false,
  userRole = '',
  platformRole = '',
  user = null,
  org = null,
  activeTeam = null,
  teams = null,
  teamSlug: urlTeamSlug = '',
  teamTab: urlTeamTab = 'members',
  subsection = '',
}: SettingsPageProps) {
  const isAdmin = userRole === 'admin' || userRole === 'owner'
  const isSuperadmin = platformRole === 'superadmin'

  // --- Team state ---
  const [allTeams, setAllTeams] = useState<Team[]>([])
  const [teamsLoading, setTeamsLoading] = useState(false)
  const [teamsError, setTeamsError] = useState('')
  const [callerRoles, setCallerRoles] = useState<Record<string, string>>({})
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newSlug, setNewSlug] = useState('')
  const [newDesc, setNewDesc] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')

  // Resolve which team is selected — use activeTeam from TopBar context (no separate picker)
  const resolvedTeamSlug = urlTeamSlug || activeTeam || (allTeams.length === 1 ? allTeams[0].slug : '') || ''
  const selectedTeam = allTeams.find(t => t.slug === resolvedTeamSlug) || null
  const activeTeamName = teams?.find(t => t.slug === resolvedTeamSlug)?.name || selectedTeam?.name || resolvedTeamSlug

  // Load teams in platform mode
  const loadTeams = useCallback(async () => {
    if (!isPlatformMode) return
    setTeamsLoading(true); setTeamsError('')
    try {
      const data = await fetchTeams()
      setAllTeams(data)
    } catch (err) { setTeamsError(errMsg(err, 'Failed to load teams')) }
    finally { setTeamsLoading(false) }
  }, [isPlatformMode])

  useEffect(() => {
    if (isPlatformMode) loadTeams()
  }, [isPlatformMode, loadTeams])

  // Load caller role for selected team
  useEffect(() => {
    if (!resolvedTeamSlug || callerRoles[resolvedTeamSlug]) return
    let cancelled = false
    const load = async () => {
      try {
        const resp = await fetchTeamMembers(resolvedTeamSlug)
        if (!cancelled) setCallerRoles(prev => ({ ...prev, [resolvedTeamSlug]: resp.callerRole }))
      } catch { /* ignore */ }
    }
    load()
    return () => { cancelled = true }
  }, [resolvedTeamSlug, callerRoles])

  const canManageTeam = isAdmin || callerRoles[resolvedTeamSlug] === 'admin' || callerRoles[resolvedTeamSlug] === 'org_admin'

  // --- Build sidebar categories ---
  // Admin-only team items (hidden from regular members)
  const adminOnlyTeamItems = new Set(['team-providers', 'team-mcp', 'team-scheduler', 'team-taps', 'team-container'])
  const memberTeamItems = canManageTeam ? TEAM_ITEMS : TEAM_ITEMS.filter(item => !adminOnlyTeamItems.has(item.id))

  // In platform mode, skills, MCP, and providers are managed at org/team level, not system
  const platformSystemItems = SYSTEM_ITEMS.filter(item => 
    item.id !== 'skills' && item.id !== 'mcp' && item.id !== 'providers'
  )

  const categories: MenuCategory[] = isPlatformMode
    ? isAdmin
      ? [
          { label: 'Preferences', items: PREFERENCE_ITEMS },
          { label: activeTeamName ? `Team — ${activeTeamName}` : 'Team', items: TEAM_ITEMS },
          { label: 'Organization', items: ORG_ITEMS },
          ...(isSuperadmin ? [{ label: 'Platform', items: PLATFORM_ITEMS }] : []),
          { label: 'System', items: platformSystemItems },
        ]
      : [
          { label: 'Preferences', items: PREFERENCE_ITEMS },
          { label: activeTeamName ? `Team — ${activeTeamName}` : 'Team', items: memberTeamItems },
        ]
    : [
        { label: 'Preferences', items: PREFERENCE_ITEMS },
        { label: 'Resources', items: RESOURCE_ITEMS },
        { label: 'System', items: SYSTEM_ITEMS },
      ]

  // --- Resolve active section ---
  const allItems = categories.flatMap(c => c.items)
  // Map URL section params to internal section IDs
  let resolvedSection = externalActiveSection || 'chat'
  if (resolvedSection === 'team') {
    resolvedSection = `team-${urlTeamTab || 'members'}`
  } else if (resolvedSection === 'org') {
    resolvedSection = `org-${subsection || 'users'}`
  } else if (resolvedSection === 'platform') {
    resolvedSection = `platform-${subsection || 'orgs'}`
  }

  const activeSection = allItems.some(i => i.id === resolvedSection) ? resolvedSection : 'chat'
  const activeLabel = allItems.find(item => item.id === activeSection)?.label || ''

  // Determine which data to load (for system-level settings sections)
  const isSystemSection = !activeSection.startsWith('team-') && !activeSection.startsWith('org-') && !activeSection.startsWith('platform-')
  const data = useSettingsData(isSystemSection ? activeSection : '')

  // --- Team CRUD handlers ---
  const handleCreateTeam = async (e: FormEvent) => {
    e.preventDefault(); setCreating(true); setCreateError('')
    try {
      await createTeam(newName, newSlug, newDesc)
      setShowCreateModal(false); setNewName(''); setNewSlug(''); setNewDesc('')
      await loadTeams()
    } catch (err) { setCreateError(errMsg(err, 'Failed to create team')) }
    finally { setCreating(false) }
  }

  const handleDeleteTeam = async () => {
    if (!resolvedTeamSlug || resolvedTeamSlug === 'general') return
    try {
      await deleteTeam(resolvedTeamSlug)
      await loadTeams()
      // Navigate to first available team
      const newSlug = allTeams.find(t => t.slug !== resolvedTeamSlug)?.slug || 'general'
      if (onSectionChange) onSectionChange(`team-members`)
    } catch (err) { setTeamsError(errMsg(err, 'Failed to delete team')) }
  }

  // Navigate to a team tab with slug
  const navigateTeamTab = (tab: string, slug?: string) => {
    if (onSectionChange) {
      // We encode team navigation as a special format the parent can parse
      // The parent (App.tsx) will build the correct URL
      onSectionChange(`team/${slug || resolvedTeamSlug}/${tab}`)
    }
  }

  // --- Loading state ---
  if (isSystemSection && data.loading) {
    return (
      <div className="flex h-full items-center justify-center" style={{ background: 'var(--bg-primary)' }}>
        <div style={{ color: 'var(--text-muted)' }}>Loading settings...</div>
      </div>
    )
  }

  return (
    <div className="flex h-full" style={{ background: 'var(--bg-primary)' }}>
      {/* Left Sidebar */}
      <div className="w-64 border-r flex flex-col shrink-0" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)' }}>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Settings</h2>
        </div>

        {/* Menu items with category headers */}
        <nav className="flex-1 p-2 space-y-0.5 overflow-y-auto">
          {categories.map((category, catIdx) => (
            <div key={catIdx}>
              {category.label && (
                <div className="px-3 pt-4 pb-1.5 text-[11px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
                  {category.label}
                </div>
              )}

              {category.items.map(item => {
                const Icon = item.icon
                const isActive = activeSection === item.id
                return (
                  <button
                    key={item.id}
                    onClick={() => {
                      if (item.id.startsWith('team-')) {
                        const tab = item.id.replace('team-', '')
                        navigateTeamTab(tab, resolvedTeamSlug)
                      } else if (item.id.startsWith('org-')) {
                        if (onSectionChange) onSectionChange(`org/${item.id.replace('org-', '')}`)
                      } else if (item.id.startsWith('platform-')) {
                        if (onSectionChange) onSectionChange(`platform/${item.id.replace('platform-', '')}`)
                      } else {
                        if (onSectionChange) onSectionChange(item.id)
                      }
                    }}
                    className="w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-all"
                    style={{
                      background: isActive ? 'var(--accent-soft)' : 'transparent',
                      color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                      border: `1px solid ${isActive ? 'rgba(95, 79, 178, 0.25)' : 'transparent'}`
                    }}
                  >
                    <Icon size={18} />
                    <span className="font-medium text-sm">{item.label}</span>
                    {isActive && <ChevronRight size={16} className="ml-auto" />}
                  </button>
                )
              })}
            </div>
          ))}
        </nav>

        {/* Update Available Indicator */}
        {updateAvailable && onUpdateClick && (
          <div className="p-3 border-t" style={{ borderColor: 'var(--border-color)' }}>
            <button
              onClick={onUpdateClick}
              className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all hover:scale-[1.02]"
              style={{
                background: 'linear-gradient(135deg, rgba(168, 85, 247, 0.2) 0%, rgba(124, 58, 237, 0.2) 100%)',
                border: '1px solid rgba(168, 85, 247, 0.3)'
              }}
            >
              <Download size={18} style={{ color: '#a855f7' }} />
              <div className="flex-1 text-left">
                <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Update Available</div>
                <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{updateAvailable.version}</div>
              </div>
            </button>
          </div>
        )}

        {/* Version Info */}
        <div className="p-3 border-t text-xs space-y-1" style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
          <div className="opacity-60">App Version: {appVersion}</div>
          <div className="opacity-60">UI Version: {__UI_VERSION__}</div>
        </div>
      </div>

      {/* Right Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="p-4 border-b flex items-center justify-between" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <h3 className="text-xl font-semibold" style={{ color: 'var(--text-primary)' }}>
            {activeLabel}
          </h3>
          {/* Delete team button (when in team section, admin, not 'general' team) */}
          {activeSection.startsWith('team-') && isAdmin && selectedTeam && selectedTeam.slug !== 'general' && (
            <button onClick={handleDeleteTeam} className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium hover:opacity-90" style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
              <Trash2 size={12} />Delete Team
            </button>
          )}
        </div>

        {/* Content */}
        <div className={activeSection === 'mcp' || activeSection === 'team-mcp' ? 'flex-1 overflow-hidden' : 'flex-1 overflow-y-auto'}>
          {/* Team sections */}
          {activeSection.startsWith('team-') && resolvedTeamSlug && selectedTeam && user && (
            <TeamContent
              tabId={activeSection.replace('team-', '')}
              teamSlug={resolvedTeamSlug}
              theme={theme}
              user={user}
              canManageTeam={canManageTeam}
              team={selectedTeam}
            />
          )}

          {activeSection.startsWith('team-') && (!resolvedTeamSlug || !selectedTeam) && !teamsLoading && (
            <div className="flex items-center justify-center h-full">
              <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                {teamsError || 'No team selected'}
              </p>
            </div>
          )}

          {activeSection.startsWith('team-') && teamsLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
            </div>
          )}

          {/* Organization sections */}
          {activeSection === 'org-users' && user && org && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <UserManagement theme={theme as 'dark' | 'light'} user={user} org={org} />
            </Suspense>
          )}

          {activeSection === 'org-audit' && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <AuditViewer theme={theme as 'dark' | 'light'} />
            </Suspense>
          )}

          {activeSection === 'org-skills' && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <SkillsSettings config={null} onSaved={() => {}} theme={theme} scope="org" isPlatform canManage={isAdmin} teamSlug={resolvedTeamSlug} />
            </Suspense>
          )}

          {activeSection === 'org-mcp' && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <div className="p-6 h-full">
                <OrgMCPServersTab theme={theme} />
              </div>
            </Suspense>
          )}

          {activeSection === 'org-providers' && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <div className="p-6">
                <OrgProvidersTab />
              </div>
            </Suspense>
          )}

          {/* Platform sections */}
          {activeSection === 'platform-providers' && isSuperadmin && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <div className="p-6">
                <PlatformProvidersTab />
              </div>
            </Suspense>
          )}
          {activeSection.startsWith('platform-') && activeSection !== 'platform-providers' && isSuperadmin && (
            <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
              <PlatformAdminPanel theme={theme as 'dark' | 'light'} activeTab={activeSection.replace('platform-', '')} />
            </Suspense>
          )}

          {/* System / preferences sections (delegated to SettingsContent) */}
          {isSystemSection && (
            <div className={activeSection === 'mcp' ? 'h-full' : 'p-6'}>
              <SettingsContent
                activeSection={activeSection}
                settings={data.settings}
                mcpConfig={data.mcpConfig}
                webCapableTools={data.webCapableTools}
                standardServers={data.standardServers}
                loadData={data.loadData}
                fullConfig={data.fullConfig}
                fullConfigLoading={data.fullConfigLoading}
                invalidateFullConfig={data.invalidateFullConfig}
                onToolsRefresh={onToolsRefresh}
                onSettingsSaved={onSettingsSaved}
                onSectionChange={onSectionChange}
                theme={theme}
                isPlatformMode={isPlatformMode}
                isOrgAdmin={isAdmin}
              />
            </div>
          )}
        </div>
      </div>

      {/* Create team modal */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setShowCreateModal(false)} />
          <div className="relative w-full max-w-md mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
            <div className="px-6 py-5" style={gradientPurple}>
              <h2 className="text-lg font-semibold text-white">Create Team</h2>
              <p className="text-sm text-white/70 mt-0.5">Add a new team to your organization</p>
            </div>
            <form onSubmit={handleCreateTeam} className="p-6 space-y-4">
              <InlineError msg={createError} />
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Name</label>
                <input type="text" value={newName} onChange={e => { setNewName(e.target.value); setNewSlug(slugify(e.target.value)) }} placeholder="Engineering" required className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Slug</label>
                <input type="text" value={newSlug} onChange={e => setNewSlug(e.target.value)} placeholder="engineering" required className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Description</label>
                <input type="text" value={newDesc} onChange={e => setNewDesc(e.target.value)} placeholder="Optional description" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
              </div>
              <div className="flex gap-3 pt-2">
                <button type="button" onClick={() => { setShowCreateModal(false); setCreateError('') }} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
                <button type="submit" disabled={creating} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={gradientPurple}>
                  {creating ? <Loader2 size={16} className="animate-spin" /> : 'Create Team'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
