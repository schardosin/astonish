import { useState, useEffect, useCallback } from 'react'
import {
  Building2, Users, Plus, Trash2, AlertCircle, Loader2,
  Crown, UserPlus, Pause, Play, Search, X, Edit2, CheckCircle2, Ban,
  Shield, ToggleLeft, ToggleRight, Globe, Eye, EyeOff
} from 'lucide-react'
import * as adminApi from '../api/platformAdmin'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface PlatformAdminPanelProps {
  theme: 'dark' | 'light'
  activeTab?: string
  onTabChange?: (tab: string) => void
}

// ---------------------------------------------------------------------------
// Styles (matching UserManagement)
// ---------------------------------------------------------------------------

const gradientAmber = { background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)' }
const inputStyle = { background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }
const errorBg = { background: 'rgba(239, 68, 68, 0.1)', color: 'var(--danger)', border: '1px solid rgba(239, 68, 68, 0.2)' }
const successBg = { background: 'rgba(34, 197, 94, 0.1)', color: '#22c55e', border: '1px solid rgba(34, 197, 94, 0.2)' }

function InlineError({ msg }: { msg: string }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={errorBg}>
      <AlertCircle size={14} /><span>{msg}</span>
    </div>
  )
}

function InlineSuccess({ msg }: { msg: string }) {
  if (!msg) return null
  return (
    <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={successBg}>
      <CheckCircle2 size={14} /><span>{msg}</span>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const isActive = status === 'active'
  const isSuspended = status === 'suspended'
  const color = isActive ? '#22c55e' : isSuspended ? '#f59e0b' : '#ef4444'
  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{ background: `${color}20`, color }}
    >
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: color }} />
      {status}
    </span>
  )
}

function RoleBadge({ role }: { role: string }) {
  const colors: Record<string, { bg: string; fg: string }> = {
    superadmin: { bg: 'rgba(234, 179, 8, 0.15)', fg: '#eab308' },
    owner: { bg: 'rgba(168, 85, 247, 0.15)', fg: '#a855f7' },
    admin: { bg: 'rgba(59, 130, 246, 0.15)', fg: '#3b82f6' },
    member: { bg: 'rgba(107, 114, 128, 0.15)', fg: '#6b7280' },
  }
  const c = colors[role] || colors.member
  return (
    <span className="px-2 py-0.5 rounded-full text-xs font-medium" style={{ background: c.bg, color: c.fg }}>
      {role}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export default function PlatformAdminPanel({ activeTab: externalTab, onTabChange: externalOnTabChange }: PlatformAdminPanelProps) {
  const [internalTab, setInternalTab] = useState('orgs')
  const activeTab = externalTab || internalTab
  const onTabChange = externalOnTabChange || setInternalTab

  return (
    <div className="flex flex-col h-full" style={{ background: 'var(--bg-primary)', color: 'var(--text-primary)' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b" style={{ borderColor: 'var(--border-color)' }}>
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-xl" style={gradientAmber}>
            <Crown size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>Platform Administration</h1>
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Manage organizations and users across the platform</p>
          </div>
        </div>

        {/* Tabs in header area */}
        <div className="flex items-center gap-1">
          <button
            onClick={() => onTabChange('orgs')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'orgs' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'orgs' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Building2 size={13} /> Organizations
          </button>
          <button
            onClick={() => onTabChange('users')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'users' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'users' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Users size={13} /> Users
          </button>
          <button
            onClick={() => onTabChange('auth')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'auth' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'auth' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Shield size={13} /> Authentication
          </button>
          <button
            onClick={() => onTabChange('channels')}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            style={{
              background: activeTab === 'channels' ? 'var(--accent-soft)' : 'transparent',
              color: activeTab === 'channels' ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Globe size={13} /> Channels
          </button>
        </div>
      </div>

      {/* Content */}
      {activeTab === 'orgs' && <OrgsTab />}
      {activeTab === 'users' && <UsersTab />}
      {activeTab === 'auth' && <AuthTab />}
      {activeTab === 'channels' && <ChannelsTab />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Organizations Tab
// ---------------------------------------------------------------------------

function OrgsTab() {
  const [orgs, setOrgs] = useState<adminApi.AdminOrg[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [filter, setFilter] = useState('')

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await adminApi.listOrgs()
      setOrgs(data)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  // Auto-dismiss success
  useEffect(() => {
    if (success) { const t = setTimeout(() => setSuccess(''), 3000); return () => clearTimeout(t) }
  }, [success])

  // Auto-dismiss error
  useEffect(() => {
    if (error) { const t = setTimeout(() => setError(''), 5000); return () => clearTimeout(t) }
  }, [error])

  const filtered = orgs.filter(o =>
    o.name.toLowerCase().includes(filter.toLowerCase()) ||
    o.slug.toLowerCase().includes(filter.toLowerCase())
  )

  const handleSuspend = async (slug: string) => {
    if (!confirm(`Suspend organization "${slug}"? Members will lose access.`)) return
    try {
      await adminApi.updateOrg(slug, { status: 'suspended' })
      setSuccess(`Organization "${slug}" suspended`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  const handleReactivate = async (slug: string) => {
    try {
      await adminApi.updateOrg(slug, { status: 'active' })
      setSuccess(`Organization "${slug}" reactivated`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  const handleDelete = async (slug: string) => {
    if (!confirm(`PERMANENTLY DELETE organization "${slug}"? This cannot be undone.`)) return
    try {
      await adminApi.deleteOrg(slug)
      setSuccess(`Organization "${slug}" deleted`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  return (
    <>
      {/* Status messages */}
      {(error || success) && (
        <div className="px-6 pt-3">
          {error && <InlineError msg={error} />}
          {success && <InlineSuccess msg={success} />}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {/* Filter + action bar */}
        <div className="flex items-center gap-3 mb-4">
          <div className="flex-1 relative">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
            <input
              type="text"
              placeholder="Filter organizations..."
              value={filter}
              onChange={e => setFilter(e.target.value)}
              className="w-full pl-9 pr-3 py-2 rounded-xl text-sm outline-none"
              style={inputStyle}
            />
          </div>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-white hover:opacity-90"
            style={gradientAmber}
          >
            <Plus size={14} /> New Org
          </button>
        </div>

        {/* Table */}
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : filtered.length === 0 ? (
          <p className="text-center py-12 text-sm" style={{ color: 'var(--text-muted)' }}>
            {orgs.length === 0 ? 'No organizations yet.' : 'No matches.'}
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <th className="text-left py-2 px-3 font-medium">Organization</th>
                <th className="text-left py-2 px-3 font-medium">Status</th>
                <th className="text-left py-2 px-3 font-medium">Members</th>
                <th className="text-left py-2 px-3 font-medium">Teams</th>
                <th className="text-left py-2 px-3 font-medium">Created</th>
                <th className="text-right py-2 px-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(org => (
                <tr key={org.id} className="border-t" style={{ borderColor: 'var(--border-color)' }}>
                  <td className="py-3 px-3">
                    <div className="font-medium" style={{ color: 'var(--text-primary)' }}>{org.name}</div>
                    <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{org.slug}</div>
                  </td>
                  <td className="py-3 px-3"><StatusBadge status={org.status} /></td>
                  <td className="py-3 px-3" style={{ color: 'var(--text-secondary)' }}>{org.member_count}</td>
                  <td className="py-3 px-3" style={{ color: 'var(--text-secondary)' }}>{org.team_count}</td>
                  <td className="py-3 px-3" style={{ color: 'var(--text-muted)' }}>
                    {new Date(org.created_at).toLocaleDateString()}
                  </td>
                  <td className="py-3 px-3">
                    <div className="flex items-center justify-end gap-1">
                      {org.status === 'active' && (
                        <button onClick={() => handleSuspend(org.slug)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: '#f59e0b' }} title="Suspend">
                          <Pause size={14} />
                        </button>
                      )}
                      {org.status === 'suspended' && (
                        <>
                          <button onClick={() => handleReactivate(org.slug)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: '#22c55e' }} title="Reactivate">
                            <Play size={14} />
                          </button>
                          <button onClick={() => handleDelete(org.slug)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: 'var(--danger)' }} title="Permanently Delete">
                            <Trash2 size={14} />
                          </button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Create org modal */}
      {showCreate && (
        <CreateOrgModal
          onCreated={() => { setShowCreate(false); load() }}
          onCancel={() => setShowCreate(false)}
          onError={setError}
          onSuccess={setSuccess}
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// Users Tab
// ---------------------------------------------------------------------------

function UsersTab() {
  const [users, setUsers] = useState<adminApi.AdminUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [filter, setFilter] = useState('')
  const [editingUser, setEditingUser] = useState<adminApi.AdminUser | null>(null)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await adminApi.listUsers()
      setUsers(data)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  // Auto-dismiss
  useEffect(() => {
    if (success) { const t = setTimeout(() => setSuccess(''), 3000); return () => clearTimeout(t) }
  }, [success])
  useEffect(() => {
    if (error) { const t = setTimeout(() => setError(''), 5000); return () => clearTimeout(t) }
  }, [error])

  const filtered = users.filter(u =>
    u.email.toLowerCase().includes(filter.toLowerCase()) ||
    u.display_name.toLowerCase().includes(filter.toLowerCase())
  )

  const handleDelete = async (user: adminApi.AdminUser) => {
    if (!confirm(`Delete user "${user.email}"? This action cannot be undone.`)) return
    try {
      await adminApi.deleteUser(user.id)
      setSuccess(`User "${user.email}" deleted`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  const handleToggleSuperadmin = async (user: adminApi.AdminUser) => {
    const newRole = user.platform_role === 'superadmin' ? '' : 'superadmin'
    const action = newRole === 'superadmin' ? 'Promote' : 'Demote'
    if (!confirm(`${action} "${user.email}" ${newRole ? 'to' : 'from'} platform superadmin?`)) return
    try {
      await adminApi.updateUser(user.id, { platform_role: newRole })
      setSuccess(`User "${user.email}" ${action.toLowerCase()}d`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  const handleToggleStatus = async (user: adminApi.AdminUser) => {
    const newStatus = user.status === 'active' ? 'suspended' : 'active'
    try {
      await adminApi.updateUser(user.id, { status: newStatus })
      setSuccess(`User "${user.email}" ${newStatus === 'active' ? 'reactivated' : 'suspended'}`)
      load()
    } catch (e: any) { setError(e.message) }
  }

  return (
    <>
      {/* Status messages */}
      {(error || success) && (
        <div className="px-6 pt-3">
          {error && <InlineError msg={error} />}
          {success && <InlineSuccess msg={success} />}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {/* Filter + action bar */}
        <div className="flex items-center gap-3 mb-4">
          <div className="flex-1 relative">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
            <input
              type="text"
              placeholder="Filter users..."
              value={filter}
              onChange={e => setFilter(e.target.value)}
              className="w-full pl-9 pr-3 py-2 rounded-xl text-sm outline-none"
              style={inputStyle}
            />
          </div>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-white hover:opacity-90"
            style={gradientAmber}
          >
            <UserPlus size={14} /> New User
          </button>
        </div>

        {/* Table */}
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : filtered.length === 0 ? (
          <p className="text-center py-12 text-sm" style={{ color: 'var(--text-muted)' }}>
            {users.length === 0 ? 'No users yet.' : 'No matches.'}
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr style={{ color: 'var(--text-muted)' }}>
                <th className="text-left py-2 px-3 font-medium">User</th>
                <th className="text-left py-2 px-3 font-medium">Status</th>
                <th className="text-left py-2 px-3 font-medium">Platform Role</th>
                <th className="text-left py-2 px-3 font-medium">Organizations</th>
                <th className="text-left py-2 px-3 font-medium">Joined</th>
                <th className="text-right py-2 px-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(u => (
                <tr key={u.id} className="border-t" style={{ borderColor: 'var(--border-color)' }}>
                  <td className="py-3 px-3">
                    <div className="font-medium" style={{ color: 'var(--text-primary)' }}>{u.display_name}</div>
                    <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{u.email}</div>
                  </td>
                  <td className="py-3 px-3"><StatusBadge status={u.status} /></td>
                  <td className="py-3 px-3">
                    {u.platform_role === 'superadmin' ? (
                      <RoleBadge role="superadmin" />
                    ) : (
                      <span className="text-xs" style={{ color: 'var(--text-muted)' }}>—</span>
                    )}
                  </td>
                  <td className="py-3 px-3">
                    <div className="flex flex-wrap gap-1">
                      {u.orgs && u.orgs.length > 0 ? u.orgs.map((o, i) => (
                        <RoleBadge key={i} role={o.role || 'member'} />
                      )) : (
                        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>None</span>
                      )}
                    </div>
                  </td>
                  <td className="py-3 px-3" style={{ color: 'var(--text-muted)' }}>
                    {new Date(u.created_at).toLocaleDateString()}
                  </td>
                  <td className="py-3 px-3">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => setEditingUser(u)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: 'var(--accent)' }} title="Edit">
                        <Edit2 size={14} />
                      </button>
                      <button onClick={() => handleToggleSuperadmin(u)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: u.platform_role === 'superadmin' ? '#eab308' : 'var(--text-muted)' }} title={u.platform_role === 'superadmin' ? 'Demote from Superadmin' : 'Promote to Superadmin'}>
                        <Crown size={14} />
                      </button>
                      <button onClick={() => handleToggleStatus(u)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: u.status === 'active' ? '#f59e0b' : '#22c55e' }} title={u.status === 'active' ? 'Suspend' : 'Reactivate'}>
                        {u.status === 'active' ? <Ban size={14} /> : <CheckCircle2 size={14} />}
                      </button>
                      <button onClick={() => handleDelete(u)} className="p-1.5 rounded-lg transition-opacity hover:opacity-80" style={{ color: 'var(--danger)' }} title="Delete">
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Create user modal */}
      {showCreate && (
        <CreateUserModal
          onCreated={() => { setShowCreate(false); load() }}
          onCancel={() => setShowCreate(false)}
          onError={setError}
          onSuccess={setSuccess}
        />
      )}

      {/* Edit user modal */}
      {editingUser && (
        <EditUserModal
          user={editingUser}
          onSaved={() => { setEditingUser(null); load() }}
          onCancel={() => setEditingUser(null)}
          onError={setError}
          onSuccess={setSuccess}
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// Create Org Modal
// ---------------------------------------------------------------------------

function CreateOrgModal({ onCreated, onCancel, onError, onSuccess }: { onCreated: () => void; onCancel: () => void; onError: (m: string) => void; onSuccess: (m: string) => void }) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [ownerEmail, setOwnerEmail] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) { setLocalError('Name is required'); return }
    setSubmitting(true); setLocalError('')
    try {
      const result = await adminApi.createOrg({ name: name.trim(), slug: slug.trim() || undefined, owner_email: ownerEmail.trim() || undefined })
      onSuccess(result.message)
      onCreated()
    } catch (e: any) {
      setLocalError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <div className="relative w-full max-w-md mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div className="px-6 py-5" style={gradientAmber}>
          <h2 className="text-lg font-semibold text-white">Create Organization</h2>
          <p className="text-sm text-white/70 mt-0.5">Add a new organization to the platform</p>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <InlineError msg={localError} />
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Name *</label>
            <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="Acme Corp" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Slug <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(auto-generated if empty)</span></label>
            <input type="text" value={slug} onChange={e => setSlug(e.target.value)} placeholder="acme-corp" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Owner Email <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="email" value={ownerEmail} onChange={e => setOwnerEmail(e.target.value)} placeholder="admin@acme.com" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div className="flex gap-3 pt-2">
            <button type="button" onClick={onCancel} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' }}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Save Changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Channels Tab — Platform channel adapter configuration (unified config + secrets)
// ---------------------------------------------------------------------------

function ChannelsTab() {
  const [channels, setChannels] = useState<adminApi.ChannelFullInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [expandedChannel, setExpandedChannel] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await adminApi.listChannels()
      setChannels(data || [])
    } catch (e: any) {
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

      {/* Web Services (Standard MCP Servers) */}
      <WebServicesSection />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Channel Card — unified config + secrets form for a single channel
// ---------------------------------------------------------------------------

function ChannelCard({ channel, expanded, onToggle, onSaved, onError, onDeleted }: {
  channel: adminApi.ChannelFullInfo
  expanded: boolean
  onToggle: () => void
  onSaved: (msg: string) => void
  onError: (msg: string) => void
  onDeleted: (msg: string) => void
}) {
  const [form, setForm] = useState<Record<string, any>>({})
  const [secrets, setSecrets] = useState<Record<string, string>>({})
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
    } catch (e: any) {
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
    } catch (e: any) {
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
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{expanded ? '▲' : '▼'}</span>
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
                    {s.configured && <span className="ml-1.5 text-xs" style={{ color: '#22c55e' }}>●</span>}
                  </label>
                  <input
                    type="password"
                    value={secrets[s.key] || ''}
                    onChange={e => setSecrets(prev => ({ ...prev, [s.key]: e.target.value }))}
                    placeholder={s.configured ? '(set — leave blank to keep)' : 'Enter value...'}
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

function TelegramConfigFields() {
  return (
    <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
      Telegram only requires a bot token. Create one via <span className="font-mono">@BotFather</span> on Telegram.
    </p>
  )
}

function EmailConfigFields({ form, setForm }: { form: Record<string, any>; setForm: (f: Record<string, any>) => void }) {
  const update = (key: string, value: any) => setForm({ ...form, [key]: value })

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

function SlackConfigFields({ form, setForm }: { form: Record<string, any>; setForm: (f: Record<string, any>) => void }) {
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

// ---------------------------------------------------------------------------
// Web Services Section — Standard MCP server API key management
// ---------------------------------------------------------------------------

function WebServicesSection() {
  const [services, setServices] = useState<adminApi.WebServiceInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await adminApi.listWebServices()
      setServices(data || [])
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleSave = async (id: string) => {
    if (!apiKey.trim()) {
      setError('API key cannot be empty')
      return
    }
    setSaving(true)
    setError('')
    try {
      const result = await adminApi.setWebServiceKey(id, apiKey.trim())
      setSuccess(result.message)
      setEditingId(null)
      setApiKey('')
      load()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: string, name: string) => {
    if (!confirm(`Remove API key for ${name}?`)) return
    try {
      await adminApi.deleteWebService(id)
      setSuccess(`API key removed for ${name}`)
      load()
    } catch (e: any) {
      setError(e.message)
    }
  }

  if (loading) return null

  return (
    <div className="mt-6 pt-6" style={{ borderTop: '1px solid var(--border-color)' }}>
      <div className="mb-3">
        <h3 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>Web Services</h3>
        <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
          API keys for web search and extraction MCP servers (available to all teams).
        </p>
      </div>

      {error && <InlineError msg={error} />}
      {success && <InlineSuccess msg={success} />}

      <div className="space-y-2">
        {services.map(svc => (
          <div key={svc.id} className="flex items-center justify-between p-3 rounded-lg" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <div className="flex items-center gap-3">
              <div>
                <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{svc.name}</span>
                <span className="text-xs ml-2 px-1.5 py-0.5 rounded" style={{
                  background: svc.configured ? 'rgba(34, 197, 94, 0.1)' : 'rgba(100, 100, 100, 0.1)',
                  color: svc.configured ? '#22c55e' : 'var(--text-muted)',
                }}>
                  {svc.configured ? 'Active' : 'Not set'}
                </span>
              </div>
            </div>
            <div className="flex items-center gap-2">
              {editingId === svc.id ? (
                <>
                  <input
                    type="password"
                    value={apiKey}
                    onChange={e => setApiKey(e.target.value)}
                    placeholder="Enter API key..."
                    className="px-2 py-1.5 rounded-lg text-xs outline-none font-mono w-48"
                    style={inputStyle}
                    onKeyDown={e => e.key === 'Enter' && handleSave(svc.id)}
                  />
                  <button onClick={() => handleSave(svc.id)} disabled={saving} className="px-2 py-1.5 rounded-lg text-xs font-medium text-white" style={{ background: 'var(--accent)' }}>
                    Save
                  </button>
                  <button onClick={() => { setEditingId(null); setApiKey('') }} className="px-2 py-1.5 rounded-lg text-xs" style={{ color: 'var(--text-muted)' }}>
                    <X size={12} />
                  </button>
                </>
              ) : (
                <>
                  <button onClick={() => { setEditingId(svc.id); setApiKey(''); setSuccess('') }} className="px-2 py-1.5 rounded-lg text-xs font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>
                    {svc.configured ? 'Update' : 'Set Key'}
                  </button>
                  {svc.configured && (
                    <button onClick={() => handleDelete(svc.id, svc.name)} className="px-2 py-1.5 rounded-lg text-xs" style={{ color: '#ef4444' }}>
                      <Trash2 size={12} />
                    </button>
                  )}
                </>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Create User Modal
// ---------------------------------------------------------------------------

function CreateUserModal({ onCreated, onCancel, onError, onSuccess }: { onCreated: () => void; onCancel: () => void; onError: (m: string) => void; onSuccess: (m: string) => void }) {
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!email.trim() || !displayName.trim()) {
      setLocalError('Email and display name are required')
      return
    }
    setSubmitting(true); setLocalError('')
    try {
      const result = await adminApi.createUser({
        email: email.trim(),
        display_name: displayName.trim(),
        password: password.trim() || undefined,
      })
      onSuccess(result.message)
      onCreated()
    } catch (e: any) {
      setLocalError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <div className="relative w-full max-w-md mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div className="px-6 py-5" style={gradientAmber}>
          <h2 className="text-lg font-semibold text-white">Create User</h2>
          <p className="text-sm text-white/70 mt-0.5">Add a new user to the platform</p>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <InlineError msg={localError} />
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Email *</label>
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="alice@acme.com" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Display Name *</label>
            <input type="text" value={displayName} onChange={e => setDisplayName(e.target.value)} placeholder="Alice Smith" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Password <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="Leave empty for SSO-only users" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>If empty, user can only log in via configured SSO provider.</p>
          </div>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>To assign the user to an organization, go to the org's user management page after creation.</p>
          <div className="flex gap-3 pt-2">
            <button type="button" onClick={onCancel} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={gradientAmber}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Create User'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Edit User Modal
// ---------------------------------------------------------------------------

function EditUserModal({ user, onSaved, onCancel, onError, onSuccess }: { user: adminApi.AdminUser; onSaved: () => void; onCancel: () => void; onError: (m: string) => void; onSuccess: (m: string) => void }) {
  const [displayName, setDisplayName] = useState(user.display_name)
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true); setLocalError('')
    try {
      const params: any = {}
      if (displayName !== user.display_name) params.display_name = displayName
      if (password) params.password = password
      if (Object.keys(params).length === 0) { onCancel(); return }
      await adminApi.updateUser(user.id, params)
      onSuccess(`User "${user.email}" updated`)
      onSaved()
    } catch (e: any) {
      setLocalError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <div className="relative w-full max-w-sm mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div className="px-6 py-5" style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' }}>
          <h2 className="text-lg font-semibold text-white">Edit User</h2>
          <p className="text-sm text-white/70 mt-0.5">{user.email}</p>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <InlineError msg={localError} />
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Display Name</label>
            <input type="text" value={displayName} onChange={e => setDisplayName(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>New Password <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(leave blank to keep current)</span></label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="Minimum 8 characters" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div className="flex gap-3 pt-2">
            <button type="button" onClick={onCancel} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' }}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Save Changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Authentication Tab — OIDC Provider Management
// ---------------------------------------------------------------------------

interface OIDCProvider {
  id: string
  org_id: string
  name: string
  issuer_url: string
  discovery_url?: string
  client_id: string
  client_secret?: string
  scopes: string[]
  team_claim: string
  enabled: boolean
  created_at: string
}

const ADMIN_BASE = '/api/platform/admin'

async function fetchOIDCProviders(): Promise<OIDCProvider[]> {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch OIDC providers')
  }
  const data = await res.json()
  return data.providers || []
}

async function createOIDCProvider(params: {
  name: string
  issuer_url: string
  discovery_url?: string
  client_id: string
  client_secret: string
  scopes?: string[]
  team_claim?: string
  enabled?: boolean
}): Promise<OIDCProvider> {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create OIDC provider')
  }
  const data = await res.json()
  return data.provider
}

async function updateOIDCProvider(id: string, params: Record<string, unknown>): Promise<OIDCProvider> {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update OIDC provider')
  }
  const data = await res.json()
  return data.provider
}

async function deleteOIDCProvider(id: string): Promise<void> {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete OIDC provider')
  }
}

function AuthTab() {
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [editingProvider, setEditingProvider] = useState<OIDCProvider | null>(null)
  const [togglingId, setTogglingId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await fetchOIDCProviders()
      setProviders(data)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  // Auto-dismiss
  useEffect(() => {
    if (success) { const t = setTimeout(() => setSuccess(''), 3000); return () => clearTimeout(t) }
  }, [success])
  useEffect(() => {
    if (error) { const t = setTimeout(() => setError(''), 5000); return () => clearTimeout(t) }
  }, [error])

  const handleToggleEnabled = async (provider: OIDCProvider) => {
    setTogglingId(provider.id)
    try {
      await updateOIDCProvider(provider.id, { enabled: !provider.enabled })
      setSuccess(`Provider "${provider.name}" ${provider.enabled ? 'disabled' : 'enabled'}`)
      load()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setTogglingId(null)
    }
  }

  const handleDelete = async (provider: OIDCProvider) => {
    if (!confirm(`Delete OIDC provider "${provider.name}"? Users linked via this provider will no longer be able to sign in with SSO.`)) return
    try {
      await deleteOIDCProvider(provider.id)
      setSuccess(`Provider "${provider.name}" deleted`)
      load()
    } catch (e: any) {
      setError(e.message)
    }
  }

  return (
    <>
      {/* Status messages */}
      {(error || success) && (
        <div className="px-6 pt-3">
          {error && <InlineError msg={error} />}
          {success && <InlineSuccess msg={success} />}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {/* Header description + action bar */}
        <div className="mb-4">
          <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
            Configure external identity providers for Single Sign-On (SSO). Supports any OIDC-compliant provider (SAP IAS, Azure AD, Okta, Google, Keycloak, etc.).
            Users must be pre-provisioned in Astonish before they can authenticate via SSO.
          </p>
          <div className="flex items-center gap-3">
            <button
              onClick={() => setShowCreate(true)}
              className="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-white hover:opacity-90"
              style={gradientAmber}
            >
              <Plus size={14} /> Add Provider
            </button>
          </div>
        </div>

        {/* Provider list */}
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : providers.length === 0 ? (
          <div className="text-center py-12 rounded-xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <Shield size={32} className="mx-auto mb-3" style={{ color: 'var(--text-muted)' }} />
            <p className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>No identity providers configured</p>
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
              Add an OIDC provider to enable SSO login for your users.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {providers.map(provider => (
              <div
                key={provider.id}
                className="rounded-xl p-4"
                style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="p-2 rounded-lg" style={{ background: provider.enabled ? 'rgba(34, 197, 94, 0.1)' : 'rgba(107, 114, 128, 0.1)' }}>
                      <Globe size={16} style={{ color: provider.enabled ? '#22c55e' : 'var(--text-muted)' }} />
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm truncate" style={{ color: 'var(--text-primary)' }}>{provider.name}</span>
                        <span
                          className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium"
                          style={{
                            background: provider.enabled ? 'rgba(34, 197, 94, 0.15)' : 'rgba(107, 114, 128, 0.15)',
                            color: provider.enabled ? '#22c55e' : '#6b7280',
                          }}
                        >
                          {provider.enabled ? 'Active' : 'Disabled'}
                        </span>
                      </div>
                      <div className="text-xs truncate mt-0.5" style={{ color: 'var(--text-muted)' }}>
                        {provider.issuer_url}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-1 flex-shrink-0 ml-3">
                    {/* Toggle enabled */}
                    <button
                      onClick={() => handleToggleEnabled(provider)}
                      disabled={togglingId === provider.id}
                      className="p-1.5 rounded-lg transition-opacity hover:opacity-80"
                      title={provider.enabled ? 'Disable' : 'Enable'}
                      style={{ color: provider.enabled ? '#22c55e' : 'var(--text-muted)' }}
                    >
                      {togglingId === provider.id ? (
                        <Loader2 size={14} className="animate-spin" />
                      ) : provider.enabled ? (
                        <ToggleRight size={18} />
                      ) : (
                        <ToggleLeft size={18} />
                      )}
                    </button>
                    {/* Edit */}
                    <button
                      onClick={() => setEditingProvider(provider)}
                      className="p-1.5 rounded-lg transition-opacity hover:opacity-80"
                      style={{ color: 'var(--accent)' }}
                      title="Edit"
                    >
                      <Edit2 size={14} />
                    </button>
                    {/* Delete */}
                    <button
                      onClick={() => handleDelete(provider)}
                      className="p-1.5 rounded-lg transition-opacity hover:opacity-80"
                      style={{ color: 'var(--danger)' }}
                      title="Delete"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                </div>

                {/* Details row */}
                <div className="mt-3 pt-3 border-t flex flex-wrap gap-x-6 gap-y-1" style={{ borderColor: 'var(--border-color)' }}>
                  <div className="text-xs">
                    <span style={{ color: 'var(--text-muted)' }}>Client ID: </span>
                    <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>{provider.client_id}</span>
                  </div>
                  <div className="text-xs">
                    <span style={{ color: 'var(--text-muted)' }}>Scopes: </span>
                    <span style={{ color: 'var(--text-secondary)' }}>{(provider.scopes || []).join(', ') || 'openid email profile'}</span>
                  </div>
                  {provider.team_claim && (
                    <div className="text-xs">
                      <span style={{ color: 'var(--text-muted)' }}>Team Claim: </span>
                      <span className="font-mono" style={{ color: 'var(--text-secondary)' }}>{provider.team_claim}</span>
                    </div>
                  )}
                  <div className="text-xs">
                    <span style={{ color: 'var(--text-muted)' }}>Created: </span>
                    <span style={{ color: 'var(--text-secondary)' }}>{new Date(provider.created_at).toLocaleDateString()}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Create provider modal */}
      {showCreate && (
        <CreateOIDCProviderModal
          onCreated={() => { setShowCreate(false); load() }}
          onCancel={() => setShowCreate(false)}
          onError={setError}
          onSuccess={setSuccess}
        />
      )}

      {/* Edit provider modal */}
      {editingProvider && (
        <EditOIDCProviderModal
          provider={editingProvider}
          onSaved={() => { setEditingProvider(null); load() }}
          onCancel={() => setEditingProvider(null)}
          onError={setError}
          onSuccess={setSuccess}
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// Create OIDC Provider Modal
// ---------------------------------------------------------------------------

function CreateOIDCProviderModal({ onCreated, onCancel, onError, onSuccess }: { onCreated: () => void; onCancel: () => void; onError: (m: string) => void; onSuccess: (m: string) => void }) {
  const [name, setName] = useState('')
  const [issuerUrl, setIssuerUrl] = useState('')
  const [discoveryUrl, setDiscoveryUrl] = useState('')
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [scopes, setScopes] = useState('openid email profile')
  const [teamClaim, setTeamClaim] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!issuerUrl.trim()) { setLocalError('Issuer URL is required'); return }
    if (!clientId.trim()) { setLocalError('Client ID is required'); return }
    if (!clientSecret.trim()) { setLocalError('Client Secret is required'); return }
    setSubmitting(true); setLocalError('')
    try {
      const scopeList = scopes.trim().split(/[\s,]+/).filter(Boolean)
      const result = await createOIDCProvider({
        name: name.trim() || issuerUrl.trim(),
        issuer_url: issuerUrl.trim(),
        discovery_url: discoveryUrl.trim() || undefined,
        client_id: clientId.trim(),
        client_secret: clientSecret,
        scopes: scopeList.length > 0 ? scopeList : undefined,
        team_claim: teamClaim.trim() || undefined,
        enabled: true,
      })
      onSuccess(`Provider "${result.name}" created`)
      onCreated()
    } catch (e: any) {
      setLocalError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <div className="relative w-full max-w-lg mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div className="px-6 py-5" style={{ background: 'linear-gradient(135deg, #10b981 0%, #059669 100%)' }}>
          <h2 className="text-lg font-semibold text-white">Add OIDC Provider</h2>
          <p className="text-sm text-white/70 mt-0.5">Connect an external identity provider for SSO</p>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4 max-h-[60vh] overflow-y-auto">
          <InlineError msg={localError} />
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Display Name <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(shown on login button)</span></label>
            <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. SAP IAS, Azure AD, Okta" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Issuer URL *</label>
            <input type="url" value={issuerUrl} onChange={e => setIssuerUrl(e.target.value)} placeholder="https://tenant.accounts.ondemand.com" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>The OIDC issuer URL. Must serve <code>.well-known/openid-configuration</code>.</p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Discovery URL <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="url" value={discoveryUrl} onChange={e => setDiscoveryUrl(e.target.value)} placeholder="https://subdomain.authentication.region.hana.ondemand.com" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>Only needed if the discovery endpoint differs from the issuer (e.g. SAP BTP XSUAA).</p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Client ID *</label>
            <input type="text" value={clientId} onChange={e => setClientId(e.target.value)} placeholder="your-client-id" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Client Secret *</label>
            <div className="relative">
              <input type={showSecret ? 'text' : 'password'} value={clientSecret} onChange={e => setClientSecret(e.target.value)} placeholder="your-client-secret" className="w-full px-4 py-2.5 pr-10 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
              <button type="button" onClick={() => setShowSecret(!showSecret)} className="absolute right-3 top-1/2 -translate-y-1/2 p-1" style={{ color: 'var(--text-muted)' }}>
                {showSecret ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Scopes</label>
            <input type="text" value={scopes} onChange={e => setScopes(e.target.value)} placeholder="openid email profile" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>Space or comma separated. Default: openid email profile</p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Team Claim <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="text" value={teamClaim} onChange={e => setTeamClaim(e.target.value)} placeholder="e.g. groups" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>JWT claim containing team/group info for automatic team mapping.</p>
          </div>
          <div className="flex gap-3 pt-2">
            <button type="button" onClick={onCancel} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={{ background: 'linear-gradient(135deg, #10b981 0%, #059669 100%)' }}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Create Provider'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Edit OIDC Provider Modal
// ---------------------------------------------------------------------------

function EditOIDCProviderModal({ provider, onSaved, onCancel, onError, onSuccess }: { provider: OIDCProvider; onSaved: () => void; onCancel: () => void; onError: (m: string) => void; onSuccess: (m: string) => void }) {
  const [name, setName] = useState(provider.name)
  const [issuerUrl, setIssuerUrl] = useState(provider.issuer_url)
  const [discoveryUrl, setDiscoveryUrl] = useState(provider.discovery_url || '')
  const [clientId, setClientId] = useState(provider.client_id)
  const [clientSecret, setClientSecret] = useState('')
  const [scopes, setScopes] = useState((provider.scopes || []).join(' '))
  const [teamClaim, setTeamClaim] = useState(provider.team_claim || '')
  const [showSecret, setShowSecret] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!issuerUrl.trim()) { setLocalError('Issuer URL is required'); return }
    if (!clientId.trim()) { setLocalError('Client ID is required'); return }
    setSubmitting(true); setLocalError('')
    try {
      const params: Record<string, unknown> = {}
      if (name.trim() !== provider.name) params.name = name.trim()
      if (issuerUrl.trim() !== provider.issuer_url) params.issuer_url = issuerUrl.trim()
      if (discoveryUrl.trim() !== (provider.discovery_url || '')) params.discovery_url = discoveryUrl.trim()
      if (clientId.trim() !== provider.client_id) params.client_id = clientId.trim()
      if (clientSecret) params.client_secret = clientSecret
      const scopeList = scopes.trim().split(/[\s,]+/).filter(Boolean)
      const currentScopes = (provider.scopes || []).join(' ')
      if (scopeList.join(' ') !== currentScopes) params.scopes = scopeList
      if (teamClaim.trim() !== (provider.team_claim || '')) params.team_claim = teamClaim.trim()

      if (Object.keys(params).length === 0) { onCancel(); return }

      await updateOIDCProvider(provider.id, params)
      onSuccess(`Provider "${name || provider.name}" updated`)
      onSaved()
    } catch (e: any) {
      setLocalError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <div className="relative w-full max-w-lg mx-4 rounded-2xl shadow-2xl overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div className="px-6 py-5" style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' }}>
          <h2 className="text-lg font-semibold text-white">Edit OIDC Provider</h2>
          <p className="text-sm text-white/70 mt-0.5">{provider.name}</p>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4 max-h-[60vh] overflow-y-auto">
          <InlineError msg={localError} />
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Display Name</label>
            <input type="text" value={name} onChange={e => setName(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Issuer URL *</label>
            <input type="url" value={issuerUrl} onChange={e => setIssuerUrl(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Discovery URL <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="url" value={discoveryUrl} onChange={e => setDiscoveryUrl(e.target.value)} placeholder="Leave empty for standard providers" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
            <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>Only needed if discovery endpoint differs from issuer (e.g. SAP BTP XSUAA).</p>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Client ID *</label>
            <input type="text" value={clientId} onChange={e => setClientId(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Client Secret <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(leave blank to keep current)</span></label>
            <div className="relative">
              <input type={showSecret ? 'text' : 'password'} value={clientSecret} onChange={e => setClientSecret(e.target.value)} placeholder="Leave blank to keep existing" className="w-full px-4 py-2.5 pr-10 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
              <button type="button" onClick={() => setShowSecret(!showSecret)} className="absolute right-3 top-1/2 -translate-y-1/2 p-1" style={{ color: 'var(--text-muted)' }}>
                {showSecret ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Scopes</label>
            <input type="text" value={scopes} onChange={e => setScopes(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Team Claim <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="text" value={teamClaim} onChange={e => setTeamClaim(e.target.value)} placeholder="e.g. groups" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none font-mono" style={inputStyle} />
          </div>
          <div className="flex gap-3 pt-2">
            <button type="button" onClick={onCancel} className="flex-1 px-4 py-3 rounded-xl text-sm font-medium" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>Cancel</button>
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)' }}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Save Changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
