import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Eye, EyeOff, Edit2, Shield, ShieldOff, Lock, AlertCircle, Check, Loader2, KeyRound, X, Copy } from 'lucide-react'
import { inputClass, inputStyle, labelStyle, hintStyle, sectionBorderStyle, saveButtonStyle } from './settingsApi'

// API helpers
const fetchCredentials = async () => {
  const res = await fetch('/api/credentials')
  if (!res.ok) throw new Error('Failed to fetch credentials')
  return res.json()
}

const revealCredential = async (name, masterKey) => {
  const headers = {}
  if (masterKey) headers['X-Master-Key'] = masterKey
  const res = await fetch(`/api/credentials/${encodeURIComponent(name)}`, { headers })
  if (res.status === 403) {
    const data = await res.json().catch(() => ({}))
    if (data.error === 'master_key_required') throw new Error('master_key_required')
    if (data.error === 'invalid_master_key') throw new Error('invalid_master_key')
    throw new Error('Access denied')
  }
  if (!res.ok) throw new Error('Failed to reveal credential')
  return res.json()
}

const revealSecret = async (key, masterKey) => {
  const headers = {}
  if (masterKey) headers['X-Master-Key'] = masterKey
  const res = await fetch(`/api/secrets/${encodeURIComponent(key)}`, { headers })
  if (res.status === 403) {
    const data = await res.json().catch(() => ({}))
    if (data.error === 'master_key_required') throw new Error('master_key_required')
    if (data.error === 'invalid_master_key') throw new Error('invalid_master_key')
    throw new Error('Access denied')
  }
  if (!res.ok) throw new Error('Failed to reveal secret')
  return res.json()
}

const saveCredential = async (name, credential) => {
  const res = await fetch('/api/credentials', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, credential })
  })
  if (!res.ok) throw new Error('Failed to save credential')
  return res.json()
}

const deleteCredential = async (name) => {
  const res = await fetch(`/api/credentials/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error('Failed to delete')
  return res.json()
}

const saveSecret = async (key, value) => {
  const res = await fetch(`/api/secrets/${encodeURIComponent(key)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ value })
  })
  if (!res.ok) throw new Error('Failed to save secret')
  return res.json()
}

const apiSetMasterKey = async (current, newKey) => {
  const res = await fetch('/api/credentials/master-key', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current, new: newKey })
  })
  if (res.status === 403) throw new Error('Invalid current master key')
  if (!res.ok) throw new Error('Failed to set master key')
  return res.json()
}

const verifyMasterKey = async (password) => {
  const res = await fetch('/api/credentials/verify-master-key', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password })
  })
  if (!res.ok) throw new Error('Failed to verify')
  const data = await res.json()
  return data.valid
}

// Credential type labels
const TYPE_LABELS = {
  api_key: 'API Key',
  bearer: 'Bearer Token',
  basic: 'Basic Auth',
  password: 'Password',
  oauth_client_credentials: 'OAuth Client Credentials',
  oauth_authorization_code: 'OAuth Auth Code'
}

const TYPE_COLORS = {
  api_key: '#a855f7',
  bearer: '#3b82f6',
  basic: '#f59e0b',
  password: '#ef4444',
  oauth_client_credentials: '#10b981',
  oauth_authorization_code: '#06b6d4'
}

// Type badge component
function TypeBadge({ type }) {
  const color = TYPE_COLORS[type] || '#6b7280'
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium"
      style={{ background: color + '20', color, border: `1px solid ${color}40` }}>
      {TYPE_LABELS[type] || type}
    </span>
  )
}

// Masked value display
function MaskedValue({ value, revealed }) {
  if (!revealed || !value) return <span className="font-mono text-xs" style={{ color: 'var(--text-muted)' }}>{'*'.repeat(Math.min(value?.length || 8, 32))}</span>
  return <span className="font-mono text-xs break-all" style={{ color: 'var(--text-primary)' }}>{value}</span>
}

// Copy button
function CopyButton({ text }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async (e) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  if (!text) return null
  return (
    <button onClick={handleCopy} className="p-1 rounded hover:bg-white/10 transition-colors" title="Copy">
      {copied ? <Check size={12} style={{ color: '#10b981' }} /> : <Copy size={12} style={{ color: 'var(--text-muted)' }} />}
    </button>
  )
}

export default function CredentialsSettings() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  // Master key state
  const [masterKey, setMasterKey] = useState(null) // cached in memory for session
  const [showMasterKeyPrompt, setShowMasterKeyPrompt] = useState(false)
  const [masterKeyInput, setMasterKeyInput] = useState('')
  const [masterKeyError, setMasterKeyError] = useState('')
  const [masterKeyCallback, setMasterKeyCallback] = useState(null)
  const [showMasterKeySetup, setShowMasterKeySetup] = useState(false)
  const [mkSetupCurrent, setMkSetupCurrent] = useState('')
  const [mkSetupNew, setMkSetupNew] = useState('')
  const [mkSetupConfirm, setMkSetupConfirm] = useState('')
  const [mkSetupError, setMkSetupError] = useState('')
  const [mkSetupSaving, setMkSetupSaving] = useState(false)

  // Revealed credentials/secrets
  const [revealedCreds, setRevealedCreds] = useState({})
  const [revealedSecrets, setRevealedSecrets] = useState({})
  const [revealingCred, setRevealingCred] = useState(null)
  const [revealingSecret, setRevealingSecret] = useState(null)

  // Add/edit modal
  const [showCredModal, setShowCredModal] = useState(false)
  const [editingCred, setEditingCred] = useState(null) // null = add, string = edit name
  const [credForm, setCredForm] = useState({ name: '', type: 'api_key', header: 'Authorization', value: '', token: '', username: '', password: '', auth_url: '', client_id: '', client_secret: '', scope: '', token_url: '', access_token: '', refresh_token: '' })
  const [credFormSaving, setCredFormSaving] = useState(false)
  const [credFormError, setCredFormError] = useState('')

  // Add secret modal
  const [showSecretModal, setShowSecretModal] = useState(false)
  const [secretForm, setSecretForm] = useState({ key: '', value: '' })
  const [secretFormSaving, setSecretFormSaving] = useState(false)
  const [secretFormError, setSecretFormError] = useState('')

  // Delete confirm
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [deleting, setDeleting] = useState(false)

  const loadData = useCallback(async () => {
    try {
      setLoading(true)
      const result = await fetchCredentials()
      setData(result)
      setError(null)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  // Master key prompt flow: call this before any reveal action
  const withMasterKey = useCallback((callback) => {
    if (!data?.has_master_key) {
      callback(null)
      return
    }
    if (masterKey) {
      callback(masterKey)
      return
    }
    // Need to prompt
    setMasterKeyInput('')
    setMasterKeyError('')
    setMasterKeyCallback(() => callback)
    setShowMasterKeyPrompt(true)
  }, [data, masterKey])

  const handleMasterKeySubmit = async () => {
    if (!masterKeyInput) {
      setMasterKeyError('Please enter the master key')
      return
    }
    try {
      const valid = await verifyMasterKey(masterKeyInput)
      if (!valid) {
        setMasterKeyError('Invalid master key')
        return
      }
      setMasterKey(masterKeyInput)
      setShowMasterKeyPrompt(false)
      if (masterKeyCallback) masterKeyCallback(masterKeyInput)
    } catch {
      setMasterKeyError('Verification failed')
    }
  }

  // Reveal credential
  const handleRevealCred = (name) => {
    if (revealedCreds[name]) {
      setRevealedCreds(prev => { const n = { ...prev }; delete n[name]; return n })
      return
    }
    withMasterKey(async (mk) => {
      try {
        setRevealingCred(name)
        const detail = await revealCredential(name, mk)
        setRevealedCreds(prev => ({ ...prev, [name]: detail }))
      } catch (err) {
        if (err.message === 'master_key_required' || err.message === 'invalid_master_key') {
          setMasterKey(null)
          setMasterKeyInput('')
          setMasterKeyError(err.message === 'invalid_master_key' ? 'Master key expired or invalid' : '')
          setMasterKeyCallback(() => () => handleRevealCred(name))
          setShowMasterKeyPrompt(true)
        } else {
          setError(err.message)
        }
      } finally {
        setRevealingCred(null)
      }
    })
  }

  // Reveal secret
  const handleRevealSecret = (key) => {
    if (revealedSecrets[key]) {
      setRevealedSecrets(prev => { const n = { ...prev }; delete n[key]; return n })
      return
    }
    withMasterKey(async (mk) => {
      try {
        setRevealingSecret(key)
        const detail = await revealSecret(key, mk)
        setRevealedSecrets(prev => ({ ...prev, [key]: detail.value }))
      } catch (err) {
        if (err.message === 'master_key_required' || err.message === 'invalid_master_key') {
          setMasterKey(null)
          setMasterKeyInput('')
          setMasterKeyError(err.message === 'invalid_master_key' ? 'Master key expired or invalid' : '')
          setMasterKeyCallback(() => () => handleRevealSecret(key))
          setShowMasterKeyPrompt(true)
        } else {
          setError(err.message)
        }
      } finally {
        setRevealingSecret(null)
      }
    })
  }

  // Add credential
  const openAddCred = () => {
    setEditingCred(null)
    setCredForm({ name: '', type: 'api_key', header: 'Authorization', value: '', token: '', username: '', password: '', auth_url: '', client_id: '', client_secret: '', scope: '', token_url: '', access_token: '', refresh_token: '' })
    setCredFormError('')
    setShowCredModal(true)
  }

  // Edit credential (must reveal first)
  const openEditCred = (name) => {
    const revealed = revealedCreds[name]
    if (!revealed) return
    setEditingCred(name)
    setCredForm({
      name,
      type: revealed.type || 'api_key',
      header: revealed.header || 'Authorization',
      value: revealed.value || '',
      token: revealed.token || '',
      username: revealed.username || '',
      password: revealed.password || '',
      auth_url: revealed.auth_url || '',
      client_id: revealed.client_id || '',
      client_secret: revealed.client_secret || '',
      scope: revealed.scope || '',
      token_url: revealed.token_url || '',
      access_token: revealed.access_token || '',
      refresh_token: revealed.refresh_token || ''
    })
    setCredFormError('')
    setShowCredModal(true)
  }

  const handleSaveCred = async () => {
    if (!credForm.name) { setCredFormError('Name is required'); return }
    setCredFormSaving(true)
    setCredFormError('')
    try {
      const cred = { type: credForm.type }
      switch (credForm.type) {
        case 'api_key': cred.header = credForm.header; cred.value = credForm.value; break
        case 'bearer': cred.token = credForm.token; break
        case 'basic': case 'password': cred.username = credForm.username; cred.password = credForm.password; break
        case 'oauth_client_credentials': cred.auth_url = credForm.auth_url; cred.client_id = credForm.client_id; cred.client_secret = credForm.client_secret; cred.scope = credForm.scope; break
        case 'oauth_authorization_code': cred.token_url = credForm.token_url; cred.client_id = credForm.client_id; cred.client_secret = credForm.client_secret; cred.access_token = credForm.access_token; cred.refresh_token = credForm.refresh_token; cred.scope = credForm.scope; break
      }
      await saveCredential(credForm.name, cred)
      setShowCredModal(false)
      setRevealedCreds(prev => { const n = { ...prev }; delete n[credForm.name]; return n })
      await loadData()
    } catch (err) {
      setCredFormError(err.message)
    } finally {
      setCredFormSaving(false)
    }
  }

  // Add secret
  const openAddSecret = () => {
    setSecretForm({ key: '', value: '' })
    setSecretFormError('')
    setShowSecretModal(true)
  }

  const handleSaveSecret = async () => {
    if (!secretForm.key) { setSecretFormError('Key is required'); return }
    if (!secretForm.value) { setSecretFormError('Value is required'); return }
    setSecretFormSaving(true)
    setSecretFormError('')
    try {
      await saveSecret(secretForm.key, secretForm.value)
      setShowSecretModal(false)
      setRevealedSecrets(prev => { const n = { ...prev }; delete n[secretForm.key]; return n })
      await loadData()
    } catch (err) {
      setSecretFormError(err.message)
    } finally {
      setSecretFormSaving(false)
    }
  }

  // Delete
  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteCredential(deleteTarget)
      setDeleteTarget(null)
      setRevealedCreds(prev => { const n = { ...prev }; delete n[deleteTarget]; return n })
      setRevealedSecrets(prev => { const n = { ...prev }; delete n[deleteTarget]; return n })
      await loadData()
    } catch (err) {
      setError(err.message)
    } finally {
      setDeleting(false)
    }
  }

  // Master key setup
  const handleMasterKeySetup = async () => {
    setMkSetupError('')
    if (data?.has_master_key && !mkSetupCurrent) {
      setMkSetupError('Current master key is required')
      return
    }
    if (mkSetupNew && mkSetupNew !== mkSetupConfirm) {
      setMkSetupError('New keys do not match')
      return
    }
    setMkSetupSaving(true)
    try {
      await apiSetMasterKey(mkSetupCurrent, mkSetupNew)
      setShowMasterKeySetup(false)
      setMasterKey(null) // clear cached key
      setRevealedCreds({})
      setRevealedSecrets({})
      await loadData()
    } catch (err) {
      setMkSetupError(err.message)
    } finally {
      setMkSetupSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
        <span className="ml-2 text-sm" style={{ color: 'var(--text-muted)' }}>Loading credentials...</span>
      </div>
    )
  }

  const credentials = data?.credentials || []
  const secrets = data?.secrets || []
  const hasMasterKey = data?.has_master_key || false

  return (
    <div className="max-w-2xl space-y-6">
      {error && (
        <div className="flex items-center gap-2 p-3 rounded-lg text-sm" style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)' }}>
          <AlertCircle size={16} style={{ color: '#ef4444' }} />
          <span style={{ color: '#ef4444' }}>{error}</span>
          <button onClick={() => setError(null)} className="ml-auto"><X size={14} style={{ color: '#ef4444' }} /></button>
        </div>
      )}

      {/* Master Key Banner */}
      <div className="flex items-center justify-between p-3 rounded-lg" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
        <div className="flex items-center gap-3">
          {hasMasterKey ? (
            <>
              <Shield size={18} style={{ color: '#10b981' }} />
              <div>
                <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>Master key enabled</div>
                <div className="text-xs" style={hintStyle}>Credential values require the master key to view.</div>
              </div>
            </>
          ) : (
            <>
              <ShieldOff size={18} style={{ color: 'var(--text-muted)' }} />
              <div>
                <div className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>No master key</div>
                <div className="text-xs" style={hintStyle}>Credential values can be viewed freely. Set a master key for extra protection.</div>
              </div>
            </>
          )}
        </div>
        <button
          onClick={() => {
            setMkSetupCurrent('')
            setMkSetupNew('')
            setMkSetupConfirm('')
            setMkSetupError('')
            setShowMasterKeySetup(true)
          }}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all hover:scale-[1.02]"
          style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}
        >
          <KeyRound size={14} />
          {hasMasterKey ? 'Change' : 'Set Master Key'}
        </button>
      </div>

      {/* HTTP Credentials */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            HTTP Credentials ({credentials.length})
          </h3>
          <button
            onClick={openAddCred}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium text-white transition-all hover:scale-[1.02]"
            style={saveButtonStyle}
          >
            <Plus size={14} /> Add
          </button>
        </div>

        {credentials.length === 0 ? (
          <div className="text-center py-8 rounded-lg" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <Lock size={24} className="mx-auto mb-2" style={{ color: 'var(--text-muted)' }} />
            <p className="text-sm" style={hintStyle}>No HTTP credentials stored.</p>
            <p className="text-xs mt-1" style={hintStyle}>Add credentials via the button above, CLI, or chat.</p>
          </div>
        ) : (
          <div className="space-y-2">
            {credentials.map(cred => {
              const revealed = revealedCreds[cred.name]
              const isRevealing = revealingCred === cred.name
              return (
                <div key={cred.name} className="rounded-lg border p-3" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{cred.name}</span>
                      <TypeBadge type={cred.type} />
                    </div>
                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => handleRevealCred(cred.name)}
                        className="p-1.5 rounded hover:bg-white/10 transition-colors"
                        title={revealed ? 'Hide' : 'Reveal'}
                        disabled={isRevealing}
                      >
                        {isRevealing ? <Loader2 size={14} className="animate-spin" style={{ color: 'var(--text-muted)' }} /> :
                          revealed ? <EyeOff size={14} style={{ color: 'var(--accent)' }} /> : <Eye size={14} style={{ color: 'var(--text-muted)' }} />}
                      </button>
                      {revealed && (
                        <button
                          onClick={() => openEditCred(cred.name)}
                          className="p-1.5 rounded hover:bg-white/10 transition-colors"
                          title="Edit"
                        >
                          <Edit2 size={14} style={{ color: 'var(--text-muted)' }} />
                        </button>
                      )}
                      <button
                        onClick={() => setDeleteTarget(cred.name)}
                        className="p-1.5 rounded hover:bg-white/10 transition-colors"
                        title="Delete"
                      >
                        <Trash2 size={14} style={{ color: 'var(--text-muted)' }} />
                      </button>
                    </div>
                  </div>
                  {/* Revealed fields */}
                  {revealed && (
                    <div className="mt-2 pt-2 border-t space-y-1.5" style={sectionBorderStyle}>
                      {revealed.type === 'api_key' && (
                        <>
                          <FieldRow label="Header" value={revealed.header} />
                          <FieldRow label="Value" value={revealed.value} secret />
                        </>
                      )}
                      {revealed.type === 'bearer' && <FieldRow label="Token" value={revealed.token} secret />}
                      {(revealed.type === 'basic' || revealed.type === 'password') && (
                        <>
                          <FieldRow label="Username" value={revealed.username} />
                          <FieldRow label="Password" value={revealed.password} secret />
                        </>
                      )}
                      {revealed.type === 'oauth_client_credentials' && (
                        <>
                          <FieldRow label="Auth URL" value={revealed.auth_url} />
                          <FieldRow label="Client ID" value={revealed.client_id} />
                          <FieldRow label="Client Secret" value={revealed.client_secret} secret />
                          {revealed.scope && <FieldRow label="Scope" value={revealed.scope} />}
                        </>
                      )}
                      {revealed.type === 'oauth_authorization_code' && (
                        <>
                          <FieldRow label="Token URL" value={revealed.token_url} />
                          <FieldRow label="Client ID" value={revealed.client_id} />
                          <FieldRow label="Client Secret" value={revealed.client_secret} secret />
                          <FieldRow label="Access Token" value={revealed.access_token} secret />
                          <FieldRow label="Refresh Token" value={revealed.refresh_token} secret />
                          {revealed.token_expiry && <FieldRow label="Token Expiry" value={revealed.token_expiry} />}
                          {revealed.scope && <FieldRow label="Scope" value={revealed.scope} />}
                        </>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Flat Secrets */}
      <div className="pt-4 border-t" style={sectionBorderStyle}>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
            Secrets ({secrets.length})
          </h3>
          <button
            onClick={openAddSecret}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium text-white transition-all hover:scale-[1.02]"
            style={saveButtonStyle}
          >
            <Plus size={14} /> Add
          </button>
        </div>
        <p className="text-xs mb-3" style={hintStyle}>
          Provider API keys, bot tokens, and other flat key-value secrets (migrated from config.yaml).
        </p>

        {secrets.length === 0 ? (
          <div className="text-center py-6 rounded-lg" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
            <p className="text-sm" style={hintStyle}>No secrets stored.</p>
          </div>
        ) : (
          <div className="space-y-1">
            {secrets.map(key => {
              const revealed = revealedSecrets[key]
              const isRevealing = revealingSecret === key
              return (
                <div key={key} className="flex items-center justify-between px-3 py-2 rounded-lg" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
                  <div className="flex-1 min-w-0 mr-3">
                    <span className="font-mono text-xs" style={{ color: 'var(--text-primary)' }}>{key}</span>
                    {revealed !== undefined && (
                      <div className="flex items-center gap-1 mt-0.5">
                        <MaskedValue value={revealed} revealed={true} />
                        <CopyButton text={revealed} />
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <button
                      onClick={() => handleRevealSecret(key)}
                      className="p-1.5 rounded hover:bg-white/10 transition-colors"
                      title={revealed !== undefined ? 'Hide' : 'Reveal'}
                      disabled={isRevealing}
                    >
                      {isRevealing ? <Loader2 size={14} className="animate-spin" style={{ color: 'var(--text-muted)' }} /> :
                        revealed !== undefined ? <EyeOff size={14} style={{ color: 'var(--accent)' }} /> : <Eye size={14} style={{ color: 'var(--text-muted)' }} />}
                    </button>
                    <button
                      onClick={() => setDeleteTarget(key)}
                      className="p-1.5 rounded hover:bg-white/10 transition-colors"
                      title="Delete"
                    >
                      <Trash2 size={14} style={{ color: 'var(--text-muted)' }} />
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Master Key Prompt Modal */}
      {showMasterKeyPrompt && (
        <Modal onClose={() => setShowMasterKeyPrompt(false)} title="Master Key Required">
          <div className="space-y-4">
            <p className="text-sm" style={hintStyle}>Enter the master key to view credential values.</p>
            <input
              type="password"
              value={masterKeyInput}
              onChange={(e) => setMasterKeyInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleMasterKeySubmit()}
              placeholder="Master key"
              className={inputClass}
              style={inputStyle}
              autoFocus
            />
            {masterKeyError && <p className="text-xs" style={{ color: '#ef4444' }}>{masterKeyError}</p>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setShowMasterKeyPrompt(false)} className="px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
              <button onClick={handleMasterKeySubmit} className="px-4 py-1.5 rounded-lg text-sm text-white font-medium" style={saveButtonStyle}>Unlock</button>
            </div>
          </div>
        </Modal>
      )}

      {/* Master Key Setup Modal */}
      {showMasterKeySetup && (
        <Modal onClose={() => setShowMasterKeySetup(false)} title={hasMasterKey ? 'Change Master Key' : 'Set Master Key'}>
          <div className="space-y-4">
            {hasMasterKey && (
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>Current Master Key</label>
                <input type="password" value={mkSetupCurrent} onChange={(e) => setMkSetupCurrent(e.target.value)} className={inputClass} style={inputStyle} />
              </div>
            )}
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>New Master Key {hasMasterKey && <span className="font-normal" style={hintStyle}>(empty to remove)</span>}</label>
              <input type="password" value={mkSetupNew} onChange={(e) => setMkSetupNew(e.target.value)} className={inputClass} style={inputStyle} />
            </div>
            {mkSetupNew && (
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>Confirm Master Key</label>
                <input type="password" value={mkSetupConfirm} onChange={(e) => setMkSetupConfirm(e.target.value)} className={inputClass} style={inputStyle} />
              </div>
            )}
            {mkSetupError && <p className="text-xs" style={{ color: '#ef4444' }}>{mkSetupError}</p>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setShowMasterKeySetup(false)} className="px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
              <button onClick={handleMasterKeySetup} disabled={mkSetupSaving} className="px-4 py-1.5 rounded-lg text-sm text-white font-medium disabled:opacity-50" style={saveButtonStyle}>
                {mkSetupSaving ? 'Saving...' : mkSetupNew ? 'Set Key' : 'Remove Key'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Add/Edit Credential Modal */}
      {showCredModal && (
        <Modal onClose={() => setShowCredModal(false)} title={editingCred ? `Edit "${editingCred}"` : 'Add Credential'}>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>Name</label>
              <input
                type="text"
                value={credForm.name}
                onChange={(e) => setCredForm({ ...credForm, name: e.target.value })}
                disabled={!!editingCred}
                placeholder="my-api-key"
                className={inputClass + ' font-mono'}
                style={{ ...inputStyle, opacity: editingCred ? 0.6 : 1 }}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>Type</label>
              <select
                value={credForm.type}
                onChange={(e) => setCredForm({ ...credForm, type: e.target.value })}
                className={inputClass}
                style={inputStyle}
              >
                <option value="api_key">API Key (custom header + value)</option>
                <option value="bearer">Bearer Token</option>
                <option value="basic">Basic Auth (HTTP)</option>
                <option value="password">Password (SSH/FTP/SMTP/database)</option>
                <option value="oauth_client_credentials">OAuth Client Credentials</option>
                <option value="oauth_authorization_code">OAuth Authorization Code</option>
              </select>
            </div>

            {/* Type-specific fields */}
            {credForm.type === 'api_key' && (
              <>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Header Name</label>
                  <input type="text" value={credForm.header} onChange={(e) => setCredForm({ ...credForm, header: e.target.value })} placeholder="Authorization" className={inputClass} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Key Value</label>
                  <input type="password" value={credForm.value} onChange={(e) => setCredForm({ ...credForm, value: e.target.value })} placeholder="sk-..." className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
              </>
            )}
            {credForm.type === 'bearer' && (
              <div>
                <label className="block text-sm font-medium mb-2" style={labelStyle}>Token</label>
                <input type="password" value={credForm.token} onChange={(e) => setCredForm({ ...credForm, token: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
              </div>
            )}
            {(credForm.type === 'basic' || credForm.type === 'password') && (
              <>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Username</label>
                  <input type="text" value={credForm.username} onChange={(e) => setCredForm({ ...credForm, username: e.target.value })} className={inputClass} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Password</label>
                  <input type="password" value={credForm.password} onChange={(e) => setCredForm({ ...credForm, password: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
              </>
            )}
            {credForm.type === 'oauth_client_credentials' && (
              <>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Auth URL</label>
                  <input type="url" value={credForm.auth_url} onChange={(e) => setCredForm({ ...credForm, auth_url: e.target.value })} placeholder="https://auth.example.com/oauth/token" className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Client ID</label>
                  <input type="text" value={credForm.client_id} onChange={(e) => setCredForm({ ...credForm, client_id: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Client Secret</label>
                  <input type="password" value={credForm.client_secret} onChange={(e) => setCredForm({ ...credForm, client_secret: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Scope <span className="font-normal" style={hintStyle}>(optional)</span></label>
                  <input type="text" value={credForm.scope} onChange={(e) => setCredForm({ ...credForm, scope: e.target.value })} className={inputClass} style={inputStyle} />
                </div>
              </>
            )}
            {credForm.type === 'oauth_authorization_code' && (
              <>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Token URL</label>
                  <input type="url" value={credForm.token_url} onChange={(e) => setCredForm({ ...credForm, token_url: e.target.value })} placeholder="https://oauth2.googleapis.com/token" className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Client ID</label>
                  <input type="text" value={credForm.client_id} onChange={(e) => setCredForm({ ...credForm, client_id: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Client Secret</label>
                  <input type="password" value={credForm.client_secret} onChange={(e) => setCredForm({ ...credForm, client_secret: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Access Token</label>
                  <input type="password" value={credForm.access_token} onChange={(e) => setCredForm({ ...credForm, access_token: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Refresh Token</label>
                  <input type="password" value={credForm.refresh_token} onChange={(e) => setCredForm({ ...credForm, refresh_token: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2" style={labelStyle}>Scope <span className="font-normal" style={hintStyle}>(optional)</span></label>
                  <input type="text" value={credForm.scope} onChange={(e) => setCredForm({ ...credForm, scope: e.target.value })} className={inputClass} style={inputStyle} />
                </div>
              </>
            )}

            {credFormError && <p className="text-xs" style={{ color: '#ef4444' }}>{credFormError}</p>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setShowCredModal(false)} className="px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
              <button onClick={handleSaveCred} disabled={credFormSaving} className="px-4 py-1.5 rounded-lg text-sm text-white font-medium disabled:opacity-50" style={saveButtonStyle}>
                {credFormSaving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Add Secret Modal */}
      {showSecretModal && (
        <Modal onClose={() => setShowSecretModal(false)} title="Add Secret">
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>Key</label>
              <input type="text" value={secretForm.key} onChange={(e) => setSecretForm({ ...secretForm, key: e.target.value })} placeholder="provider.example.api_key" className={inputClass + ' font-mono'} style={inputStyle} />
              <p className="text-xs mt-1" style={hintStyle}>Dot-notation key (e.g., provider.openai.api_key)</p>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2" style={labelStyle}>Value</label>
              <input type="password" value={secretForm.value} onChange={(e) => setSecretForm({ ...secretForm, value: e.target.value })} className={inputClass + ' font-mono'} style={inputStyle} />
            </div>
            {secretFormError && <p className="text-xs" style={{ color: '#ef4444' }}>{secretFormError}</p>}
            <div className="flex justify-end gap-2">
              <button onClick={() => setShowSecretModal(false)} className="px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
              <button onClick={handleSaveSecret} disabled={secretFormSaving} className="px-4 py-1.5 rounded-lg text-sm text-white font-medium disabled:opacity-50" style={saveButtonStyle}>
                {secretFormSaving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Delete Confirm Modal */}
      {deleteTarget && (
        <Modal onClose={() => setDeleteTarget(null)} title="Delete Credential">
          <div className="space-y-4">
            <p className="text-sm" style={{ color: 'var(--text-primary)' }}>
              Are you sure you want to delete <span className="font-mono font-medium">{deleteTarget}</span>?
            </p>
            <p className="text-xs" style={hintStyle}>This action cannot be undone. Any tools or configurations referencing this credential will stop working.</p>
            <div className="flex justify-end gap-2">
              <button onClick={() => setDeleteTarget(null)} className="px-3 py-1.5 rounded-lg text-sm" style={{ color: 'var(--text-secondary)' }}>Cancel</button>
              <button onClick={handleDelete} disabled={deleting} className="px-4 py-1.5 rounded-lg text-sm text-white font-medium disabled:opacity-50" style={{ background: '#ef4444' }}>
                {deleting ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

// Reusable field row for revealed credentials
function FieldRow({ label, value, secret }) {
  const [show, setShow] = useState(!secret)
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs w-28 flex-shrink-0" style={hintStyle}>{label}</span>
      <span className="font-mono text-xs break-all flex-1" style={{ color: 'var(--text-primary)' }}>
        {show ? value : '*'.repeat(Math.min(value?.length || 8, 32))}
      </span>
      {secret && (
        <button onClick={() => setShow(!show)} className="p-0.5 flex-shrink-0">
          {show ? <EyeOff size={12} style={{ color: 'var(--text-muted)' }} /> : <Eye size={12} style={{ color: 'var(--text-muted)' }} />}
        </button>
      )}
      {show && <CopyButton text={value} />}
    </div>
  )
}

// Simple modal wrapper
function Modal({ children, onClose, title }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center" style={{ background: 'rgba(0,0,0,0.5)' }} onClick={onClose}>
      <div className="rounded-xl shadow-2xl p-6 w-full max-w-md max-h-[80vh] overflow-y-auto" style={{ background: 'var(--bg-primary)', border: '1px solid var(--border-color)' }} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-base font-medium" style={{ color: 'var(--text-primary)' }}>{title}</h3>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X size={16} style={{ color: 'var(--text-muted)' }} /></button>
        </div>
        {children}
      </div>
    </div>
  )
}
