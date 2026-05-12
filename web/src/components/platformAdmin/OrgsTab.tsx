import { useState, useEffect, useCallback, type FormEvent, type ChangeEvent } from 'react'
import { Plus, Trash2, Loader2, Pause, Play, Search } from 'lucide-react'
import * as adminApi from '../../api/platformAdmin'
import type { AdminOrg } from '../../api/platformAdmin'
import { InlineError, InlineSuccess, StatusBadge, gradientAmber, inputStyle } from './shared'

// ---------------------------------------------------------------------------
// Organizations Tab
// ---------------------------------------------------------------------------

export default function OrgsTab() {
  const [orgs, setOrgs] = useState<AdminOrg[]>([])
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<string>('')
  const [success, setSuccess] = useState<string>('')
  const [showCreate, setShowCreate] = useState<boolean>(false)
  const [filter, setFilter] = useState<string>('')

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await adminApi.listOrgs()
      setOrgs(data)
    } catch (e) {
      setError((e as Error).message)
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
    } catch (e) { setError((e as Error).message) }
  }

  const handleReactivate = async (slug: string) => {
    try {
      await adminApi.updateOrg(slug, { status: 'active' })
      setSuccess(`Organization "${slug}" reactivated`)
      load()
    } catch (e) { setError((e as Error).message) }
  }

  const handleDelete = async (slug: string) => {
    if (!confirm(`PERMANENTLY DELETE organization "${slug}"? This cannot be undone.`)) return
    try {
      await adminApi.deleteOrg(slug)
      setSuccess(`Organization "${slug}" deleted`)
      load()
    } catch (e) { setError((e as Error).message) }
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
              onChange={(e: ChangeEvent<HTMLInputElement>) => setFilter(e.target.value)}
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
// Create Org Modal
// ---------------------------------------------------------------------------

interface CreateOrgModalProps {
  onCreated: () => void
  onCancel: () => void
  onError: (msg: string) => void
  onSuccess: (msg: string) => void
}

function CreateOrgModal({ onCreated, onCancel, onError, onSuccess }: CreateOrgModalProps) {
  const [name, setName] = useState<string>('')
  const [slug, setSlug] = useState<string>('')
  const [ownerEmail, setOwnerEmail] = useState<string>('')
  const [submitting, setSubmitting] = useState<boolean>(false)
  const [localError, setLocalError] = useState<string>('')

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!name.trim()) { setLocalError('Name is required'); return }
    setSubmitting(true); setLocalError('')
    try {
      const result = await adminApi.createOrg({ name: name.trim(), slug: slug.trim() || undefined, owner_email: ownerEmail.trim() || undefined })
      onSuccess(result.message)
      onCreated()
    } catch (e) {
      setLocalError((e as Error).message)
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
            <input type="text" value={name} onChange={(e: ChangeEvent<HTMLInputElement>) => setName(e.target.value)} placeholder="Acme Corp" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} autoFocus />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Slug <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(auto-generated if empty)</span></label>
            <input type="text" value={slug} onChange={(e: ChangeEvent<HTMLInputElement>) => setSlug(e.target.value)} placeholder="acme-corp" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Owner Email <span className="text-xs font-normal" style={{ color: 'var(--text-muted)' }}>(optional)</span></label>
            <input type="email" value={ownerEmail} onChange={(e: ChangeEvent<HTMLInputElement>) => setOwnerEmail(e.target.value)} placeholder="admin@acme.com" className="w-full px-4 py-2.5 rounded-xl text-sm outline-none" style={inputStyle} />
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
