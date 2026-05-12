import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Loader2, Edit2, Shield, ToggleLeft, ToggleRight, Globe, Eye, EyeOff } from 'lucide-react'
import { InlineError, InlineSuccess, gradientAmber, inputStyle } from './shared'

// ---------------------------------------------------------------------------
// OIDC API functions (inline, not moved to API layer)
// ---------------------------------------------------------------------------

const ADMIN_BASE = '/api/platform/admin'

async function fetchOIDCProviders() {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to fetch OIDC providers')
  }
  const data = await res.json()
  return data.providers || []
}

async function createOIDCProvider(params) {
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

async function updateOIDCProvider(id, params) {
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

async function deleteOIDCProvider(id) {
  const res = await fetch(`${ADMIN_BASE}/oidc-providers/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete OIDC provider')
  }
}

// ---------------------------------------------------------------------------
// Authentication Tab
// ---------------------------------------------------------------------------

export default function AuthTab() {
  const [providers, setProviders] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [editingProvider, setEditingProvider] = useState(null)
  const [togglingId, setTogglingId] = useState(null)

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await fetchOIDCProviders()
      setProviders(data)
    } catch (e) {
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

  const handleToggleEnabled = async (provider) => {
    setTogglingId(provider.id)
    try {
      await updateOIDCProvider(provider.id, { enabled: !provider.enabled })
      setSuccess(`Provider "${provider.name}" ${provider.enabled ? 'disabled' : 'enabled'}`)
      load()
    } catch (e) {
      setError(e.message)
    } finally {
      setTogglingId(null)
    }
  }

  const handleDelete = async (provider) => {
    if (!confirm(`Delete OIDC provider "${provider.name}"? Users linked via this provider will no longer be able to sign in with SSO.`)) return
    try {
      await deleteOIDCProvider(provider.id)
      setSuccess(`Provider "${provider.name}" deleted`)
      load()
    } catch (e) {
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

function CreateOIDCProviderModal({ onCreated, onCancel, onError, onSuccess }) {
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

  const handleSubmit = async (e) => {
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
    } catch (e) {
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

function EditOIDCProviderModal({ provider, onSaved, onCancel, onError, onSuccess }) {
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

  const handleSubmit = async (e) => {
    e.preventDefault()
    if (!issuerUrl.trim()) { setLocalError('Issuer URL is required'); return }
    if (!clientId.trim()) { setLocalError('Client ID is required'); return }
    setSubmitting(true); setLocalError('')
    try {
      const params = {}
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
    } catch (e) {
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
