import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Shield, ShieldOff } from 'lucide-react'
import { teamFetch } from '../../api/teamContext'
import { inputStyle, saveButtonStyle } from './settingsApi'

interface NetworkPolicyRule {
  id: string
  host: string
  port: number
  action: string
  scope?: string
}

interface NetworkPolicySettingsProps {
  scope: string
  teamSlug?: string
  readOnly?: boolean
  rules?: NetworkPolicyRule[]
  onRulesChange?: () => void
}

/**
 * NetworkPolicySettings renders the network policy rules for a single scope.
 * It handles CRUD operations against /api/network-policies?scope=<scope>.
 */
export default function NetworkPolicySettings({ scope, teamSlug, readOnly = false, rules: externalRules, onRulesChange }: NetworkPolicySettingsProps) {
  const [rules, setRules] = useState<NetworkPolicyRule[]>([])
  const [loading, setLoading] = useState(false)
  const [showAddForm, setShowAddForm] = useState(false)
  const [newHost, setNewHost] = useState('')
  const [newPort, setNewPort] = useState('')
  const [newAction, setNewAction] = useState('allow')

  const fetchRules = useCallback(async () => {
    if (externalRules) {
      setRules(externalRules)
      return
    }
    setLoading(true)
    try {
      const url = `/api/network-policies?scope=${scope}`
      const res = await teamFetch(url, undefined, scope === 'platform' ? undefined : teamSlug)
      if (res.ok) {
        const data = await res.json()
        setRules(data.rules || [])
      }
    } catch (err) {
      console.error('Failed to fetch network policies:', err)
    } finally {
      setLoading(false)
    }
  }, [scope, teamSlug, externalRules])

  useEffect(() => {
    fetchRules()
  }, [fetchRules])

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newHost.trim()) return

    const port = newPort ? parseInt(newPort, 10) : 0
    try {
      const url = `/api/network-policies?scope=${scope}`
      const res = await teamFetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host: newHost.trim(), port, action: newAction }),
      }, scope === 'platform' ? undefined : teamSlug)

      if (res.ok) {
        setNewHost('')
        setNewPort('')
        setNewAction('allow')
        setShowAddForm(false)
        fetchRules()
        onRulesChange?.()
      }
    } catch (err) {
      console.error('Failed to create network policy:', err)
    }
  }

  const handleDelete = async (id: string) => {
    try {
      const url = `/api/network-policies/${id}?scope=${scope}`
      const res = await teamFetch(url, { method: 'DELETE' }, scope === 'platform' ? undefined : teamSlug)
      if (res.ok) {
        fetchRules()
        onRulesChange?.()
      }
    } catch (err) {
      console.error('Failed to delete network policy:', err)
    }
  }

  const scopeBadgeStyle = (s: string) => {
    switch (s) {
      case 'platform':
        return { background: 'rgba(168, 85, 247, 0.15)', color: '#a855f7' }
      case 'org':
        return { background: 'rgba(59, 130, 246, 0.15)', color: '#3b82f6' }
      case 'team':
        return { background: 'rgba(34, 197, 94, 0.15)', color: '#22c55e' }
      default:
        return { background: 'rgba(100, 116, 139, 0.15)', color: '#64748b' }
    }
  }

  if (loading && !externalRules) {
    return <div className="text-sm opacity-60">Loading...</div>
  }

  return (
    <div className="space-y-3">
      {/* Rules list */}
      {rules.length === 0 && !showAddForm && (
        <div className="text-sm opacity-50 py-4 text-center">
          No network policy rules configured.
        </div>
      )}

      {rules.map((rule) => (
        <div
          key={rule.id}
          className="flex items-center gap-3 p-3 rounded-lg border"
          style={{
            borderColor: 'var(--border-color)',
            background: 'var(--bg-secondary)',
          }}
        >
          {/* Action icon */}
          {rule.action === 'allow' ? (
            <Shield size={16} className="text-green-500 flex-shrink-0" />
          ) : (
            <ShieldOff size={16} className="text-red-500 flex-shrink-0" />
          )}

          {/* Host:Port */}
          <div className="flex-1 min-w-0">
            <div className="text-sm font-mono truncate">
              {rule.host}{rule.port > 0 ? `:${rule.port}` : ''}
            </div>
          </div>

          {/* Action badge */}
          <span
            className="text-xs px-2 py-0.5 rounded-full font-medium"
            style={rule.action === 'allow'
              ? { background: 'rgba(34, 197, 94, 0.15)', color: '#22c55e' }
              : { background: 'rgba(239, 68, 68, 0.15)', color: '#ef4444' }
            }
          >
            {rule.action}
          </span>

          {/* Scope badge (for merged views) */}
          {rule.scope && rule.scope !== scope && (
            <span
              className="text-xs px-2 py-0.5 rounded-full font-medium"
              style={scopeBadgeStyle(rule.scope)}
            >
              {rule.scope}
            </span>
          )}

          {/* Delete button (only for own-scope, non-readOnly) */}
          {!readOnly && (
            <button
              onClick={() => handleDelete(rule.id)}
              className="p-1 rounded hover:bg-red-500/10 text-red-400 hover:text-red-500 transition-colors"
              title="Delete rule"
            >
              <Trash2 size={14} />
            </button>
          )}
        </div>
      ))}

      {/* Add form */}
      {!readOnly && showAddForm && (
        <form onSubmit={handleAdd} className="flex items-end gap-2 p-3 rounded-lg border" style={{ borderColor: 'var(--border-color)', background: 'var(--bg-secondary)' }}>
          <div className="flex-1">
            <label className="text-xs block mb-1" style={{ color: 'var(--text-muted)' }}>Host pattern</label>
            <input
              type="text"
              value={newHost}
              onChange={(e) => setNewHost(e.target.value)}
              placeholder="e.g., *.example.com"
              className="w-full px-3 py-1.5 text-sm rounded-lg border"
              style={inputStyle}
              autoFocus
            />
          </div>
          <div className="w-20">
            <label className="text-xs block mb-1" style={{ color: 'var(--text-muted)' }}>Port</label>
            <input
              type="number"
              value={newPort}
              onChange={(e) => setNewPort(e.target.value)}
              placeholder="any"
              min="0"
              max="65535"
              className="w-full px-3 py-1.5 text-sm rounded-lg border"
              style={inputStyle}
            />
          </div>
          <div className="w-24">
            <label className="text-xs block mb-1" style={{ color: 'var(--text-muted)' }}>Action</label>
            <select
              value={newAction}
              onChange={(e) => setNewAction(e.target.value)}
              className="w-full px-3 py-1.5 text-sm rounded-lg border"
              style={inputStyle}
            >
              <option value="allow">Allow</option>
              <option value="deny">Deny</option>
            </select>
          </div>
          <button
            type="submit"
            className="px-3 py-1.5 text-sm rounded-lg font-medium"
            style={{ ...saveButtonStyle, color: 'white' }}
          >
            Add
          </button>
          <button
            type="button"
            onClick={() => setShowAddForm(false)}
            className="px-3 py-1.5 text-sm rounded-lg opacity-60 hover:opacity-100"
          >
            Cancel
          </button>
        </form>
      )}

      {/* Add button */}
      {!readOnly && !showAddForm && (
        <button
          onClick={() => setShowAddForm(true)}
          className="flex items-center gap-2 text-sm opacity-60 hover:opacity-100 transition-opacity py-2"
        >
          <Plus size={14} />
          Add rule
        </button>
      )}
    </div>
  )
}
