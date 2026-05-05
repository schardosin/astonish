import { useState, useEffect, useCallback } from 'react'
import {
  Building2, Users, Plus, Trash2, AlertCircle, Loader2,
  Crown, UserPlus, Pause, Play, Search, X, Edit2, CheckCircle2, Ban
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
        </div>
      </div>

      {/* Content */}
      {activeTab === 'orgs' ? <OrgsTab /> : <UsersTab />}
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
            <button type="submit" disabled={submitting} className="flex-1 flex items-center justify-center gap-2 px-4 py-3 rounded-xl text-sm font-medium text-white hover:opacity-90 disabled:opacity-50" style={gradientAmber}>
              {submitting ? <Loader2 size={16} className="animate-spin" /> : 'Create Organization'}
            </button>
          </div>
        </form>
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
  const [orgSlug, setOrgSlug] = useState('')
  const [orgRole, setOrgRole] = useState('member')
  const [submitting, setSubmitting] = useState(false)
  const [localError, setLocalError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!email.trim() || !displayName.trim() || !password.trim()) {
      setLocalError('Email, display name, and password are required')
      return
    }
    setSubmitting(true); setLocalError('')
    try {
      const result = await adminApi.createUser({
        email: email.trim(),
        display_name: displayName.trim(),
        password,
        org_slug: orgSlug.trim() || undefined,
        org_role: orgRole || undefined,
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
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Password *</label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="Minimum 8 characters" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Org Slug <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
              <input type="text" value={orgSlug} onChange={e => setOrgSlug(e.target.value)} placeholder="acme-corp" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Org Role</label>
              <select value={orgRole} onChange={e => setOrgRole(e.target.value)} className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle}>
                <option value="member">Member</option>
                <option value="admin">Admin</option>
                <option value="owner">Owner</option>
              </select>
            </div>
          </div>
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
