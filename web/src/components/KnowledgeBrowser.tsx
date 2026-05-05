import { useState, useEffect, useCallback, useRef } from 'react'
import { Search, Brain, Plus, Trash2, ArrowUpRight, Loader2, AlertCircle, BookOpen } from 'lucide-react'
import {
  searchMemories, listTeamMemories, listOrgMemories,
  saveTeamMemory, savePersonalMemory,
  deleteTeamMemory, deleteOrgMemory, promoteMemoryToOrg,
} from '../api/platform'
import type { MemoryEntry } from '../api/platform'

interface KnowledgeBrowserProps {
  theme: 'dark' | 'light'
  user: { id: string; email: string; display_name: string; role: string }
  activeTeam?: string | null
}
type Tab = 'team' | 'org' | 'add'
const SCOPE_COLORS: Record<string, string> = { personal: '#3b82f6', team: '#a855f7', org: '#10b981' }

function ScopeBadge({ scope }: { scope: string }) {
  const color = SCOPE_COLORS[scope] || '#6b7280'
  return (
    <span className="px-2 py-0.5 rounded-full text-xs font-medium"
      style={{ background: `${color}22`, color, border: `1px solid ${color}44` }}>
      {scope.charAt(0).toUpperCase() + scope.slice(1)}
    </span>
  )
}

interface MemoryCardProps {
  entry: MemoryEntry; isAdmin: boolean; showPromote: boolean
  onDelete: (id: string) => void; onPromote?: (id: string) => void
}

function MemoryCard({ entry, isAdmin, showPromote, onDelete, onPromote }: MemoryCardProps) {
  const snippet = entry.snippet.length > 200 ? entry.snippet.slice(0, 200) + '...' : entry.snippet
  const canDelete = entry.scope === 'team' || (entry.scope === 'org' && isAdmin)

  return (
    <div className="p-4 rounded-lg border"
      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
      <p className="text-sm mb-3 leading-relaxed" style={{ color: 'var(--text-primary)' }}>{snippet}</p>
      <div className="flex items-center gap-2 flex-wrap">
        <ScopeBadge scope={entry.scope} />
        {entry.category && (
          <span className="px-2 py-0.5 rounded-full text-xs font-medium"
            style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)', border: '1px solid var(--border-color)' }}>
            {entry.category}
          </span>
        )}
        {entry.score != null && (
          <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {(entry.score * 100).toFixed(0)}% match</span>
        )}
        <div className="flex items-center gap-1 ml-auto">
          {showPromote && isAdmin && entry.scope === 'team' && onPromote && (
            <button
              onClick={() => onPromote(entry.id)}
              title="Promote to Org"
              className="p-1.5 rounded-md transition-colors hover:opacity-80"
              style={{ color: SCOPE_COLORS.org }}
            >
              <ArrowUpRight size={15} />
            </button>
          )}
          {canDelete && (
            <button
              onClick={() => onDelete(entry.id)}
              title="Delete"
              className="p-1.5 rounded-md transition-colors hover:opacity-80"
              style={{ color: 'var(--danger, #ef4444)' }}
            >
              <Trash2 size={15} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ─── Main Component ───
export default function KnowledgeBrowser({ theme, user, activeTeam }: KnowledgeBrowserProps) {
  const isAdmin = user.role === 'admin'
  const [tab, setTab] = useState<Tab>('team')
  const [query, setQuery] = useState('')
  const [searchResults, setSearchResults] = useState<MemoryEntry[] | null>(null)
  const [teamEntries, setTeamEntries] = useState<MemoryEntry[]>([])
  const [orgEntries, setOrgEntries] = useState<MemoryEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Add-form state
  const [snippet, setSnippet] = useState('')
  const [category, setCategory] = useState('')
  const [saveScope, setSaveScope] = useState<'personal' | 'team'>('personal')
  const [saving, setSaving] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const loadTab = useCallback(async (t: Tab) => {
    if (t === 'add') return
    setLoading(true)
    setError(null)
    try {
      if (t === 'team') setTeamEntries(await listTeamMemories(activeTeam || undefined))
      else setOrgEntries(await listOrgMemories(activeTeam || undefined))
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [activeTeam])

  // Load initial data on mount
  const mountedRef = useRef<boolean | null>(null)
  if (mountedRef.current == null) {
    mountedRef.current = true
    loadTab(tab)
  }

  const switchTab = useCallback((t: Tab) => {
    setTab(t)
    loadTab(t)
  }, [loadTab])

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (query.length < 3) {
      debounceRef.current = setTimeout(() => setSearchResults(null), 0)
      return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
    }
    debounceRef.current = setTimeout(async () => {
      setLoading(true)
      setError(null)
      try {
        setSearchResults(await searchMemories(query, 20, activeTeam || undefined))
      } catch (err: any) {
        setError(err.message)
      } finally {
        setLoading(false)
      }
    }, 300)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [query, activeTeam])

  const handleDelete = useCallback(async (id: string, scope: string) => {
    setError(null)
    try {
      if (scope === 'team') await deleteTeamMemory(id, activeTeam || undefined)
      else if (scope === 'org') await deleteOrgMemory(id, activeTeam || undefined)
      // Refresh the current view
      if (searchResults) {
        setSearchResults(prev => prev ? prev.filter(e => e.id !== id) : null)
      }
      loadTab(tab)
    } catch (err: any) {
      setError(err.message)
    }
  }, [searchResults, tab, loadTab, activeTeam])

  const handlePromote = useCallback(async (id: string) => {
    setError(null)
    try {
      await promoteMemoryToOrg(id, activeTeam || undefined)
      loadTab(tab)
    } catch (err: any) {
      setError(err.message)
    }
  }, [tab, loadTab, activeTeam])

  const handleSave = useCallback(async () => {
    if (!snippet.trim()) return
    setSaving(true)
    setError(null)
    try {
      const cat = category.trim() || 'general'
      if (saveScope === 'team') await saveTeamMemory(snippet, cat, activeTeam || undefined)
      else await savePersonalMemory(snippet, cat, activeTeam || undefined)
      setSnippet('')
      setCategory('')
      switchTab('team')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }, [snippet, category, saveScope, switchTab, activeTeam])

  const entries = tab === 'team' ? teamEntries : orgEntries
  const displayList = searchResults ?? entries
  const showPromote = tab === 'org'
  const tabDefs: { key: Tab; label: string; icon: typeof Brain }[] = [
    { key: 'team', label: 'Team Memories', icon: Brain },
    { key: 'org', label: 'Org Memories', icon: BookOpen },
    { key: 'add', label: 'Add New', icon: Plus },
  ]

  return (
    <div className="flex flex-col flex-1 overflow-hidden" data-theme={theme}
      style={{ background: 'var(--bg-primary)' }}>
      {/* Header */}
      <div className="px-6 pt-6 pb-4">
        <h1 className="text-2xl font-bold mb-1" style={{ color: 'var(--text-primary)' }}>
          Knowledge Browser
        </h1>
        <p className="text-sm mb-5" style={{ color: 'var(--text-muted)' }}>
          Search and manage memories across personal, team, and org scopes.
        </p>

        {/* Search bar */}
        <div className="relative">
          <Search size={20} className="absolute left-4 top-1/2 -translate-y-1/2"
            style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            placeholder="Search across all memory tiers..."
            value={query}
            onChange={e => setQuery(e.target.value)}
            className="w-full pl-12 pr-4 py-3 rounded-xl text-base outline-none transition-colors"
            style={{
              background: 'var(--bg-secondary)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-color)',
            }}
          />
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="mx-6 mb-3 flex items-center gap-2 px-4 py-2 rounded-lg text-sm"
          style={{ background: 'rgba(239,68,68,0.12)', color: 'var(--danger, #ef4444)' }}
        >
          <AlertCircle size={16} /> {error}
        </div>
      )}

      {/* Tabs (hidden when showing search results) */}
      {!searchResults && (
        <div className="flex gap-1 px-6 mb-4">
          {tabDefs.map(t => {
            const Icon = t.icon
            const active = tab === t.key
            return (
              <button
                key={t.key}
                onClick={() => switchTab(t.key)}
                className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                style={{
                  background: active
                    ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)'
                    : 'var(--bg-secondary)',
                  color: active ? '#fff' : 'var(--text-secondary)',
                  border: `1px solid ${active ? 'transparent' : 'var(--border-color)'}`,
                }}
              >
                <Icon size={16} />
                {t.label}
              </button>
            )
          })}
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 pb-6">
        {/* Loading indicator */}
        {loading && (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin" style={{ color: 'var(--accent)' }} />
            <span className="ml-2 text-sm" style={{ color: 'var(--text-muted)' }}>Loading...</span>
          </div>
        )}

        {/* Search results header */}
        {searchResults && !loading && (
          <div className="flex items-center justify-between mb-4">
            <span className="text-sm font-medium" style={{ color: 'var(--text-muted)' }}>
              {searchResults.length} result{searchResults.length !== 1 ? 's' : ''} for "{query}"
            </span>
            <button
              onClick={() => { setQuery(''); setSearchResults(null) }}
              className="text-xs px-3 py-1 rounded-md transition-colors"
              style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)', border: '1px solid var(--border-color)' }}
            >
              Clear search
            </button>
          </div>
        )}

        {/* Memory list (search results or browse tab) */}
        {(searchResults || tab !== 'add') && !loading && (
          <div className="flex flex-col gap-3">
            {displayList.length === 0 && (
              <div className="text-center py-12" style={{ color: 'var(--text-muted)' }}>
                <Brain size={40} className="mx-auto mb-3 opacity-30" />
                <p className="text-sm">No memories found.</p>
              </div>
            )}
            {displayList.map(entry => (
              <MemoryCard
                key={entry.id}
                entry={entry}
                isAdmin={isAdmin}
                showPromote={showPromote && !searchResults}
                onDelete={id => handleDelete(id, entry.scope)}
                onPromote={handlePromote}
              />
            ))}
          </div>
        )}

        {/* Add New tab form */}
        {!searchResults && tab === 'add' && !loading && (
          <div
            className="max-w-lg mx-auto p-6 rounded-xl border"
            style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
          >
            <h2 className="text-lg font-semibold mb-4" style={{ color: 'var(--text-primary)' }}>
              Save a Memory
            </h2>

            <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>
              Snippet
            </label>
            <textarea
              rows={5}
              value={snippet}
              onChange={e => setSnippet(e.target.value)}
              placeholder="Paste knowledge, a tip, or any useful text..."
              className="w-full rounded-lg px-3 py-2 text-sm outline-none resize-y mb-4"
              style={{
                background: 'var(--bg-tertiary)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-color)',
              }}
            />

            <label className="block text-sm font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>
              Category <span style={{ color: 'var(--text-muted)' }}>(optional)</span>
            </label>
            <input
              type="text"
              value={category}
              onChange={e => setCategory(e.target.value)}
              placeholder="general"
              className="w-full rounded-lg px-3 py-2 text-sm outline-none mb-4"
              style={{
                background: 'var(--bg-tertiary)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-color)',
              }}
            />

            <fieldset className="mb-5">
              <legend className="text-sm font-medium mb-2" style={{ color: 'var(--text-secondary)' }}>
                Scope
              </legend>
              <label className="flex items-center gap-2 mb-2 cursor-pointer">
                <input
                  type="radio"
                  name="scope"
                  checked={saveScope === 'personal'}
                  onChange={() => setSaveScope('personal')}
                  style={{ accentColor: SCOPE_COLORS.personal }}
                />
                <span className="text-sm" style={{ color: 'var(--text-primary)' }}>Save for me only</span>
                <ScopeBadge scope="personal" />
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="scope"
                  checked={saveScope === 'team'}
                  onChange={() => setSaveScope('team')}
                  style={{ accentColor: SCOPE_COLORS.team }}
                />
                <span className="text-sm" style={{ color: 'var(--text-primary)' }}>Share with team</span>
                <ScopeBadge scope="team" />
              </label>
            </fieldset>

            <button
              onClick={handleSave}
              disabled={!snippet.trim() || saving}
              className="flex items-center justify-center gap-2 w-full py-2.5 rounded-lg text-sm font-medium text-white transition-opacity disabled:opacity-40"
              style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
            >
              {saving ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
              {saving ? 'Saving...' : 'Save Memory'}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
