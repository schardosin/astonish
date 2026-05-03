import { useState, useEffect, useCallback } from 'react'
import { AppWindow, Upload, Download, ArrowUpRight, Trash2, Loader2, AlertCircle } from 'lucide-react'
import {
  publishAppToTeam,
  forkApp,
  promoteAppToOrg,
  deleteOrgApp,
  fetchOrgApps,
} from '../api/platform'
import type { AppItem } from '../api/platform'

type Tab = 'personal' | 'team' | 'org'

interface AppCatalogProps {
  theme: 'dark' | 'light'
  user: { id: string; email: string; display_name: string; role: string }
  activeTeam: string | null
}

async function fetchApps(): Promise<AppItem[]> {
  const res = await fetch('/api/apps')
  if (!res.ok) throw new Error('Failed to fetch apps')
  const data = await res.json()
  return data.apps || []
}

function formatDate(dateStr: string) {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  if (diffDay < 7) return `${diffDay}d ago`
  return d.toLocaleDateString()
}

function truncate(str: string, max: number) {
  if (!str) return ''
  return str.length > max ? str.slice(0, max) + '...' : str
}

export default function AppCatalog({ theme, user, activeTeam }: AppCatalogProps) {
  const [tab, setTab] = useState<Tab>('personal')
  const [personalApps, setPersonalApps] = useState<AppItem[]>([])
  const [teamApps, setTeamApps] = useState<AppItem[]>([])
  const [orgApps, setOrgApps] = useState<AppItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busySlug, setBusySlug] = useState<string | null>(null)
  const [toast, setToast] = useState<string | null>(null)

  const isAdmin = user.role === 'admin' || user.role === 'owner'

  const showToast = useCallback((msg: string) => {
    setToast(msg)
    setTimeout(() => setToast(null), 2000)
  }, [])

  const loadTab = useCallback(async (t: Tab) => {
    setLoading(true)
    setError(null)
    try {
      if (t === 'personal') {
        setPersonalApps(await fetchApps())
      } else if (t === 'team') {
        setTeamApps(await fetchApps())
      } else {
        setOrgApps(await fetchOrgApps())
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load apps')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadTab(tab)
  }, [tab, loadTab])

  const handleAction = useCallback(async (action: () => Promise<unknown>, slug: string) => {
    setBusySlug(slug)
    try {
      await action()
      showToast('Done!')
      loadTab(tab)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Action failed')
    } finally {
      setBusySlug(null)
    }
  }, [tab, loadTab, showToast])

  const apps = tab === 'personal' ? personalApps : tab === 'team' ? teamApps : orgApps

  const tabStyle = (t: Tab) => ({
    background: tab === t
      ? 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)'
      : theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'var(--bg-tertiary)',
    color: tab === t ? '#fff' : 'var(--text-secondary)',
  })

  return (
    <div className="flex-1 overflow-auto p-6" style={{ background: 'var(--bg-primary)' }}>
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="flex items-center gap-3 mb-6">
          <AppWindow size={20} style={{ color: '#a855f7' }} />
          <h1 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
            App Catalog
          </h1>
        </div>

        {/* Tabs */}
        <div className="flex items-center gap-2 mb-6">
          {(['personal', 'team', 'org'] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`flex items-center gap-2 px-3 py-2 rounded-xl text-xs font-medium transition-all cursor-pointer ${
                tab === t ? 'shadow-md' : 'hover:bg-purple-500/10'
              }`}
              style={tabStyle(t)}
            >
              {t === 'personal' ? 'Personal' : t === 'team' ? 'Team' : 'Organization'}
            </button>
          ))}
        </div>

        {/* Error banner */}
        {error && (
          <div
            className="flex items-center gap-2 px-4 py-3 rounded-xl mb-4 text-xs"
            style={{ background: 'rgba(239,68,68,0.1)', color: '#ef4444', border: '1px solid rgba(239,68,68,0.2)' }}
          >
            <AlertCircle size={14} />
            {error}
          </div>
        )}

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        )}

        {/* Empty state */}
        {!loading && apps.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16">
            <AppWindow size={40} className="mb-3" style={{ color: 'rgba(168,85,247,0.25)' }} />
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              No apps in {tab === 'personal' ? 'your personal' : tab === 'team' ? 'team' : 'organization'} catalog.
            </p>
          </div>
        )}

        {/* App grid */}
        {!loading && apps.length > 0 && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {apps.map((app) => {
              const isBusy = busySlug === app.name
              return (
                <div
                  key={app.name}
                  className="rounded-xl overflow-hidden transition-all hover:shadow-lg"
                  style={{ border: '1px solid var(--border-color)', background: 'var(--bg-secondary)' }}
                >
                  <div className="p-4">
                    <div className="flex items-start justify-between mb-2">
                      <div className="flex items-center gap-2 min-w-0">
                        <AppWindow size={16} style={{ color: '#a855f7', flexShrink: 0 }} />
                        <span
                          className="text-sm font-medium truncate"
                          style={{ color: 'var(--text-primary)' }}
                        >
                          {app.name}
                        </span>
                      </div>
                      <span
                        className="text-[10px] px-1.5 py-0.5 rounded-full shrink-0 ml-2"
                        style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}
                      >
                        v{app.version}
                      </span>
                    </div>

                    {app.description && (
                      <p className="text-xs mb-3" style={{ color: 'var(--text-muted)' }}>
                        {truncate(app.description, 100)}
                      </p>
                    )}

                    <div className="text-[10px] mb-3" style={{ color: 'var(--text-muted)' }}>
                      {formatDate(app.updatedAt)}
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-2 flex-wrap">
                      {tab === 'personal' && (
                        <button
                          disabled={isBusy}
                          onClick={() => handleAction(() => publishAppToTeam(app.name), app.name)}
                          className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[11px] font-medium text-white cursor-pointer disabled:opacity-50 transition-all"
                          style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                        >
                          {isBusy ? <Loader2 size={12} className="animate-spin" /> : <Upload size={12} />}
                          Publish to Team
                        </button>
                      )}

                      {tab === 'team' && (
                        <>
                          <button
                            disabled={isBusy}
                            onClick={() => handleAction(() => forkApp(app.name, 'team'), app.name)}
                            className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[11px] font-medium cursor-pointer disabled:opacity-50 transition-all"
                            style={{
                              background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'var(--bg-tertiary)',
                              color: 'var(--text-primary)',
                            }}
                          >
                            {isBusy ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
                            Fork to Personal
                          </button>
                          {isAdmin && activeTeam && (
                            <button
                              disabled={isBusy}
                              onClick={() =>
                                handleAction(() => promoteAppToOrg(app.name, activeTeam), app.name)
                              }
                              className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[11px] font-medium text-white cursor-pointer disabled:opacity-50 transition-all"
                              style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
                            >
                              {isBusy ? <Loader2 size={12} className="animate-spin" /> : <ArrowUpRight size={12} />}
                              Promote to Org
                            </button>
                          )}
                        </>
                      )}

                      {tab === 'org' && (
                        <>
                          <button
                            disabled={isBusy}
                            onClick={() => handleAction(() => forkApp(app.name, 'org'), app.name)}
                            className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[11px] font-medium cursor-pointer disabled:opacity-50 transition-all"
                            style={{
                              background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'var(--bg-tertiary)',
                              color: 'var(--text-primary)',
                            }}
                          >
                            {isBusy ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
                            Fork to Personal
                          </button>
                          {isAdmin && (
                            <button
                              disabled={isBusy}
                              onClick={() => handleAction(() => deleteOrgApp(app.name), app.name)}
                              className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg text-[11px] font-medium cursor-pointer disabled:opacity-50 transition-all"
                              style={{ background: 'rgba(239,68,68,0.15)', color: '#ef4444' }}
                            >
                              {isBusy ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                              Delete
                            </button>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Toast */}
      {toast && (
        <div className="fixed bottom-6 right-6 z-[100]">
          <div
            className="flex items-center gap-2 px-4 py-2.5 rounded-xl shadow-2xl text-sm font-medium text-white"
            style={{ background: 'linear-gradient(135deg, #10b981 0%, #059669 100%)' }}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            {toast}
          </div>
        </div>
      )}
    </div>
  )
}
