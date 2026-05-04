import React, { useState, useEffect, useCallback, lazy, Suspense } from 'react'
import {
  Users, Plus, Trash2, Shield, UserPlus, ChevronRight, AlertCircle,
  Loader2, Wand2, Brain, Clock, GitBranch, Store, UserCog, FileText
} from 'lucide-react'
import {
  fetchTeams, createTeam, deleteTeam,
  fetchTeamMembers, addTeamMember, removeTeamMember, setTeamMemberRole,
  type Team, type TeamMember,
} from '../api/platform'

// Lazy-loaded sub-views and settings components
const UserManagement = lazy(() => import('./UserManagement'))
const AuditViewer = lazy(() => import('./AuditViewer'))
const SkillsSettings = lazy(() => import('./settings/SkillsSettings'))
const MemorySettings = lazy(() => import('./settings/MemorySettings'))
const SchedulerSettings = lazy(() => import('./settings/SchedulerSettings'))
const TapsSettings = lazy(() => import('./settings/TapsSettings'))
const FlowStorePanel = lazy(() => import('./FlowStorePanel'))

interface TeamManagementProps {
  theme: 'dark' | 'light'
  user: { id: string; email: string; display_name: string; role: string }
  org: { id: string; name: string; slug: string }
  activeTeam: string | null
  /** Sidebar section: 'teams' | 'users' | 'audit' */
  activeSection?: string
  /** Selected team slug (when section=teams) */
  activeTeamSlug?: string
  /** Active tab within team detail */
  activeTeamTab?: string
  /** Callback for navigation changes */
  onNavigate?: (section: string, teamSlug?: string, tab?: string) => void
}

// ---------------------------------------------------------------------------
// Constants
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
// Team detail tab definitions
// ---------------------------------------------------------------------------

interface TeamTab {
  id: string
  label: string
  icon: any
}

const TEAM_TABS: TeamTab[] = [
  { id: 'members', label: 'Members', icon: Users },
  { id: 'skills', label: 'Skills', icon: Wand2 },
  { id: 'memory', label: 'Memory', icon: Brain },
  { id: 'scheduler', label: 'Scheduler', icon: Clock },
  { id: 'taps', label: 'Repositories', icon: GitBranch },
  { id: 'flows', label: 'Flow Store', icon: Store },
]

// ---------------------------------------------------------------------------
// Sidebar section definitions
// ---------------------------------------------------------------------------

interface SidebarSection {
  id: string
  label: string
  icon: any
  adminOnly?: boolean
}

const SIDEBAR_SECTIONS: SidebarSection[] = [
  { id: 'users', label: 'Users', icon: UserCog, adminOnly: true },
  { id: 'audit', label: 'Audit', icon: FileText, adminOnly: true },
]

// ---------------------------------------------------------------------------
// Members panel (team member management)
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

  const handleAddMember = async (e: React.FormEvent) => {
    e.preventDefault()
    setAdding(true); setAddError('')
    try {
      await addTeamMember(team.slug, addEmail, addRole)
      setShowAddMember(false); setAddEmail(''); setAddRole('member')
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
        <form onSubmit={handleAddMember} className="flex items-center gap-3 px-6 py-3 border-b" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <input type="email" value={addEmail} onChange={e => setAddEmail(e.target.value)} placeholder="Email address" required className="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle} autoFocus />
          <select value={addRole} onChange={e => setAddRole(e.target.value)} className="px-3 py-2 rounded-lg text-sm outline-none" style={inputStyle}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
          </select>
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
// Team resource tab content — renders settings component based on tab
// ---------------------------------------------------------------------------

interface TeamResourceTabProps {
  tabId: string
  theme: string
  fullConfig: any
  fullConfigLoading: boolean
  onSaved: () => void
}

function TeamResourceTab({ tabId, theme, fullConfig, fullConfigLoading, onSaved }: TeamResourceTabProps) {
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
        {tabId === 'skills' && fullConfig && <SkillsSettings config={fullConfig.skills} onSaved={onSaved} theme={theme} />}
        {tabId === 'memory' && fullConfig && <MemorySettings config={fullConfig.memory} onSaved={onSaved} />}
        {tabId === 'scheduler' && fullConfig && <SchedulerSettings config={fullConfig.scheduler} onSaved={onSaved} />}
        {tabId === 'taps' && <TapsSettings />}
        {tabId === 'flows' && <FlowStorePanel />}
      </Suspense>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Team detail view — shows team header + tabs (Members, Skills, etc.)
// ---------------------------------------------------------------------------

interface TeamDetailProps {
  user: { id: string; email: string; display_name: string; role: string }
  team: Team
  callerRole: string
  activeTab: string
  onTabChange: (tab: string) => void
  onDeleteTeam: () => void
  theme: string
}

function TeamDetail({ user, team, callerRole, activeTab, onTabChange, onDeleteTeam, theme }: TeamDetailProps) {
  const isOrgAdmin = user.role === 'admin' || user.role === 'owner'
  const canManageTeam = isOrgAdmin || callerRole === 'admin' || callerRole === 'org_admin'

  // Full config loading for resource tabs
  const [fullConfig, setFullConfig] = useState<any>(null)
  const [fullConfigLoading, setFullConfigLoading] = useState(false)

  const needsFullConfig = ['skills', 'memory', 'scheduler'].includes(activeTab)

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

  const handleSaved = () => setFullConfig(null) // invalidate on save

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Team header */}
      <div className="flex items-center justify-between px-6 py-3 border-b" style={{ borderColor: 'var(--border-color)' }}>
        <div>
          <h2 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>{team.name}</h2>
          {team.description && <p className="text-sm mt-0.5" style={{ color: 'var(--text-muted)' }}>{team.description}</p>}
        </div>
        {isOrgAdmin && team.slug !== 'general' && (
          <button onClick={onDeleteTeam} className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium hover:opacity-90" style={{ background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }}>
            <Trash2 size={12} />Delete
          </button>
        )}
      </div>

      {/* Tab bar */}
      <div className="flex items-center gap-1 px-4 pt-2 pb-0 border-b" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
        {TEAM_TABS.map(tab => {
          const Icon = tab.icon
          const isActive = activeTab === tab.id
          return (
            <button
              key={tab.id}
              onClick={() => onTabChange(tab.id)}
              className="flex items-center gap-1.5 px-3 py-2 text-xs font-medium rounded-t-lg transition-colors"
              style={{
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                background: isActive ? 'var(--bg-primary)' : 'transparent',
                borderBottom: isActive ? '2px solid var(--accent)' : '2px solid transparent',
                marginBottom: '-1px',
              }}
            >
              <Icon size={14} />
              {tab.label}
            </button>
          )
        })}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-hidden">
        {activeTab === 'members' && (
          <MembersPanel user={user} team={team} canManageTeam={canManageTeam} />
        )}

        {activeTab !== 'members' && (
          <TeamResourceTab
            tabId={activeTab}
            theme={theme}
            fullConfig={fullConfig}
            fullConfigLoading={fullConfigLoading}
            onSaved={handleSaved}
          />
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main Management View
// ---------------------------------------------------------------------------

export default function TeamManagement({
  user,
  org,
  activeTeam,
  activeSection: externalSection,
  activeTeamSlug: externalTeamSlug,
  activeTeamTab: externalTeamTab,
  onNavigate,
  theme = 'dark',
}: TeamManagementProps) {
  const isOrgAdmin = user.role === 'admin' || user.role === 'owner'

  // State
  const [teams, setTeams] = useState<Team[]>([])
  const [teamsLoading, setTeamsLoading] = useState(true)
  const [teamsError, setTeamsError] = useState('')
  const [callerRoles, setCallerRoles] = useState<Record<string, string>>({})

  // Modal state
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newSlug, setNewSlug] = useState('')
  const [newDesc, setNewDesc] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')

  // Navigation state
  const section = externalSection || 'teams'
  const selectedTeamSlug = externalTeamSlug || activeTeam || null
  const teamTab = externalTeamTab || 'members'
  const selectedTeam = teams.find(t => t.slug === selectedTeamSlug) || null

  // Sidebar sections visible to this user
  const visibleSections = SIDEBAR_SECTIONS.filter(s => !s.adminOnly || isOrgAdmin)

  // Load teams
  const loadTeams = useCallback(async () => {
    setTeamsLoading(true); setTeamsError('')
    try {
      const data = await fetchTeams()
      setTeams(data)
    } catch (err) { setTeamsError(errMsg(err, 'Failed to load teams')) }
    finally { setTeamsLoading(false) }
  }, [])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setTeamsLoading(true); setTeamsError('')
      try {
        const data = await fetchTeams()
        if (cancelled) return
        setTeams(data)
      } catch (err) { if (!cancelled) setTeamsError(errMsg(err, 'Failed to load teams')) }
      finally { if (!cancelled) setTeamsLoading(false) }
    }
    load()
    return () => { cancelled = true }
  }, [])

  // Load caller role for selected team
  useEffect(() => {
    if (!selectedTeamSlug || callerRoles[selectedTeamSlug]) return
    let cancelled = false
    const load = async () => {
      try {
        const resp = await fetchTeamMembers(selectedTeamSlug)
        if (!cancelled) {
          setCallerRoles(prev => ({ ...prev, [selectedTeamSlug]: resp.callerRole }))
        }
      } catch { /* ignore */ }
    }
    load()
    return () => { cancelled = true }
  }, [selectedTeamSlug, callerRoles])

  // Navigation helpers
  const navigateToTeam = (slug: string, tab?: string) => {
    if (onNavigate) onNavigate('teams', slug, tab || 'members')
  }

  const navigateToSection = (sectionId: string) => {
    if (onNavigate) onNavigate(sectionId)
  }

  const navigateToTeamTab = (tab: string) => {
    if (onNavigate && selectedTeamSlug) onNavigate('teams', selectedTeamSlug, tab)
  }

  // Handlers
  const handleCreateTeam = async (e: React.FormEvent) => {
    e.preventDefault(); setCreating(true); setCreateError('')
    try {
      await createTeam(newName, newSlug, newDesc)
      setShowCreateModal(false); setNewName(''); setNewSlug(''); setNewDesc('')
      await loadTeams()
    } catch (err) { setCreateError(errMsg(err, 'Failed to create team')) }
    finally { setCreating(false) }
  }

  const handleDeleteTeam = async () => {
    if (!selectedTeamSlug || selectedTeamSlug === 'general') return
    try {
      await deleteTeam(selectedTeamSlug)
      await loadTeams()
      // Navigate to first available team
      navigateToTeam(teams.find(t => t.slug !== selectedTeamSlug)?.slug || 'general')
    } catch (err) { setTeamsError(errMsg(err, 'Failed to delete team')) }
  }

  return (
    <div className="flex h-full" style={{ background: 'var(--bg-primary)', color: 'var(--text-primary)' }}>
      {/* Left Sidebar */}
      <div className="w-60 flex-shrink-0 flex flex-col border-r" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        {/* Teams section header */}
        <div className="flex items-center justify-between p-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
          <div className="flex items-center gap-2">
            <Users size={18} style={{ color: 'var(--accent)' }} />
            <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>Teams</span>
          </div>
          {isOrgAdmin && (
            <button onClick={() => setShowCreateModal(true)} className="p-1.5 rounded-lg transition-colors hover:opacity-80" style={{ background: 'var(--accent-soft)', color: 'var(--accent)' }}>
              <Plus size={16} />
            </button>
          )}
        </div>

        {/* Teams list */}
        <div className="flex-1 overflow-y-auto p-2">
          {teamsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
            </div>
          ) : teamsError ? (
            <div className="flex items-center gap-2 p-3 text-sm" style={{ color: 'var(--danger)' }}>
              <AlertCircle size={14} /><span>{teamsError}</span>
            </div>
          ) : teams.map(team => {
            const isActive = section === 'teams' && selectedTeamSlug === team.slug
            return (
              <button
                key={team.slug}
                onClick={() => navigateToTeam(team.slug)}
                className="w-full flex items-center justify-between px-3 py-2.5 rounded-lg text-left text-sm transition-colors mb-0.5"
                style={{
                  background: isActive ? 'var(--accent-soft)' : 'transparent',
                  color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                }}
              >
                <div className="flex items-center gap-2 truncate">
                  <Shield size={14} /><span className="truncate">{team.name}</span>
                </div>
                {isActive && <ChevronRight size={14} />}
              </button>
            )
          })}
        </div>

        {/* Admin sections (Users, Audit) */}
        {visibleSections.length > 0 && (
          <div className="border-t p-2" style={{ borderColor: 'var(--border-color)' }}>
            <div className="px-3 pt-2 pb-1.5 text-[11px] font-semibold uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
              Admin
            </div>
            {visibleSections.map(s => {
              const Icon = s.icon
              const isActive = section === s.id
              return (
                <button
                  key={s.id}
                  onClick={() => navigateToSection(s.id)}
                  className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-left text-sm transition-colors mb-0.5"
                  style={{
                    background: isActive ? 'var(--accent-soft)' : 'transparent',
                    color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                  }}
                >
                  <Icon size={14} /><span>{s.label}</span>
                </button>
              )
            })}
          </div>
        )}
      </div>

      {/* Right content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {section === 'teams' && selectedTeam && (
          <TeamDetail
            user={user}
            team={selectedTeam}
            callerRole={callerRoles[selectedTeam.slug] || ''}
            activeTab={teamTab}
            onTabChange={navigateToTeamTab}
            onDeleteTeam={handleDeleteTeam}
            theme={theme}
          />
        )}

        {section === 'teams' && !selectedTeam && !teamsLoading && (
          <div className="flex-1 flex items-center justify-center">
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Select a team to manage</p>
          </div>
        )}

        {section === 'users' && isOrgAdmin && (
          <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
            <UserManagement theme={theme} user={user} org={org} />
          </Suspense>
        )}

        {section === 'audit' && isOrgAdmin && (
          <Suspense fallback={<div className="flex items-center justify-center h-full"><Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} /></div>}>
            <AuditViewer theme={theme} />
          </Suspense>
        )}
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
