import React, { useState, useEffect, useRef, useCallback } from 'react'
import { FileText, Filter, RefreshCw, ChevronDown, Loader2 } from 'lucide-react'
import { queryAuditLogs } from '../api/platform'
import type { AuditEntry, AuditFilter } from '../api/platform'

interface AuditViewerProps {
  theme: 'dark' | 'light'
}

const LIMIT = 50
const ACTIONS = ['All', 'GET', 'POST', 'PUT', 'DELETE'] as const

const ACTION_COLORS: Record<string, { bg: string; text: string }> = {
  GET:    { bg: 'rgba(156,163,175,0.2)', text: '#9ca3af' },
  POST:   { bg: 'rgba(34,197,94,0.2)',   text: '#22c55e' },
  PUT:    { bg: 'rgba(59,130,246,0.2)',   text: '#3b82f6' },
  DELETE: { bg: 'rgba(239,68,68,0.2)',    text: '#ef4444' },
}

function statusColor(status: number | undefined): string {
  if (!status) return 'var(--text-muted)'
  if (status >= 200 && status < 300) return '#22c55e'
  if (status >= 400 && status < 500) return '#f59e0b'
  if (status >= 500) return '#ef4444'
  return 'var(--text-muted)'
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) +
    ', ' + d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
}

function buildFilter(action: string, resource: string, since: string, until: string, off: number): AuditFilter {
  const f: AuditFilter = { limit: LIMIT, offset: off }
  if (action !== 'All') f.action = action
  if (resource.trim()) f.resource = resource.trim()
  if (since) f.since = new Date(since).toISOString()
  if (until) f.until = new Date(until).toISOString()
  return f
}

export default function AuditViewer({ theme }: AuditViewerProps) {
  void theme
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [count, setCount] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)

  // Filter state
  const [action, setAction] = useState('All')
  const [resource, setResource] = useState('')
  const [since, setSince] = useState('')
  const [until, setUntil] = useState('')

  // Auto-refresh
  const [autoRefresh, setAutoRefresh] = useState(false)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  // Tick counter drives the effect — bumped by auto-refresh timer and manual triggers
  const [tick, setTick] = useState(0)
  // Track whether the next load should append (for "Load More")
  const appendRef = useRef(false)

  // Single effect that fetches whenever tick/filter state changes
  useEffect(() => {
    let cancelled = false
    const append = appendRef.current
    appendRef.current = false

    queryAuditLogs(buildFilter(action, resource, since, until, append ? offset : 0))
      .then(data => {
        if (cancelled) return
        setEntries(prev => append ? [...prev, ...data.entries] : data.entries)
        setCount(data.count)
        if (!append) setOffset(0)
      })
      .catch((err: any) => {
        if (!cancelled) setError(err.message || 'Failed to load audit logs')
      })
      .finally(() => { if (!cancelled) setLoading(false) })

    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tick, action, resource, since, until])

  // Auto-refresh interval
  useEffect(() => {
    if (autoRefresh) {
      intervalRef.current = setInterval(() => setTick(t => t + 1), 10_000)
    }
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [autoRefresh])

  const handleApply = useCallback(() => { setLoading(true); setError(null); setTick(t => t + 1) }, [])
  const handleClear = useCallback(() => {
    setLoading(true); setError(null)
    setAction('All'); setResource(''); setSince(''); setUntil('')
    // Filter state change will trigger the effect
  }, [])
  const handleLoadMore = useCallback(() => {
    appendRef.current = true
    setOffset(prev => prev + LIMIT)
    setLoading(true); setTick(t => t + 1)
  }, [])

  const inputStyle: React.CSSProperties = {
    background: 'var(--bg-tertiary)',
    color: 'var(--text-primary)',
    border: '1px solid var(--border-primary)',
    borderRadius: 6,
    padding: '6px 10px',
    fontSize: 13,
    outline: 'none',
  }

  const btnStyle: React.CSSProperties = {
    ...inputStyle,
    cursor: 'pointer',
    fontWeight: 500,
  }

  return (
    <div className="flex flex-col h-full overflow-hidden" style={{ color: 'var(--text-primary)' }}>
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-3 shrink-0" style={{ borderBottom: '1px solid var(--border-primary)' }}>
        <FileText size={18} style={{ color: 'var(--text-muted)' }} />
        <h2 className="text-base font-semibold">Audit Log</h2>
        <span className="text-xs ml-auto" style={{ color: 'var(--text-muted)' }}>Admin</span>
      </div>

      {/* Filter bar */}
      <div className="flex items-center gap-3 px-4 py-2 flex-wrap shrink-0" style={{ borderBottom: '1px solid var(--border-primary)' }}>
        <Filter size={14} style={{ color: 'var(--text-muted)' }} />

        {/* Action dropdown */}
        <div className="relative">
          <select
            value={action}
            onChange={e => setAction(e.target.value)}
            style={inputStyle}
            className="appearance-none pr-7"
          >
            {ACTIONS.map(a => <option key={a} value={a}>{a}</option>)}
          </select>
          <ChevronDown size={14} className="absolute right-2 top-1/2 -translate-y-1/2 pointer-events-none" style={{ color: 'var(--text-muted)' }} />
        </div>

        <input
          type="text"
          placeholder="Resource path..."
          value={resource}
          onChange={e => setResource(e.target.value)}
          style={{ ...inputStyle, width: 180 }}
        />

        <label className="flex items-center gap-1 text-xs" style={{ color: 'var(--text-muted)' }}>
          Since
          <input type="datetime-local" value={since} onChange={e => setSince(e.target.value)} style={{ ...inputStyle, width: 180 }} />
        </label>

        <label className="flex items-center gap-1 text-xs" style={{ color: 'var(--text-muted)' }}>
          Until
          <input type="datetime-local" value={until} onChange={e => setUntil(e.target.value)} style={{ ...inputStyle, width: 180 }} />
        </label>

        <button onClick={handleApply} style={{ ...btnStyle, background: 'var(--accent-primary)', color: '#fff', border: 'none' }}>
          Apply
        </button>
        <button onClick={handleClear} style={btnStyle}>Clear</button>

        {/* Auto-refresh toggle */}
        <label className="flex items-center gap-1.5 text-xs ml-auto cursor-pointer select-none" style={{ color: 'var(--text-muted)' }}>
          <RefreshCw size={13} className={autoRefresh ? 'animate-spin' : ''} />
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={e => setAutoRefresh(e.target.checked)}
            className="accent-cyan-500"
          />
          Auto-refresh
        </label>
      </div>

      {/* Error */}
      {error && (
        <div className="px-4 py-2 text-sm" style={{ color: '#ef4444' }}>{error}</div>
      )}

      {/* Table */}
      <div className="flex-1 overflow-auto">
        <table className="w-full text-sm" style={{ borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ background: 'var(--bg-secondary)', position: 'sticky', top: 0, zIndex: 1 }}>
              {['Timestamp', 'User', 'Action', 'Resource', 'Status', 'IP'].map(h => (
                <th
                  key={h}
                  className="text-left px-4 py-2 font-medium text-xs"
                  style={{ color: 'var(--text-muted)', borderBottom: '1px solid var(--border-primary)' }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {entries.map((entry, i) => {
              const ac = ACTION_COLORS[entry.action] || ACTION_COLORS.GET
              const status = entry.detail?.status as number | undefined
              return (
                <tr
                  key={entry.id}
                  style={{ background: i % 2 === 0 ? 'var(--bg-primary)' : 'var(--bg-secondary)' }}
                >
                  <td className="px-4 py-2 whitespace-nowrap" style={{ color: 'var(--text-secondary)' }}>
                    {formatTimestamp(entry.timestamp)}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs" title={entry.user_id} style={{ color: 'var(--text-secondary)' }}>
                    {entry.user_id.slice(0, 8)}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className="inline-block px-2 py-0.5 rounded-full text-xs font-medium"
                      style={{ background: ac.bg, color: ac.text }}
                    >
                      {entry.action}
                    </span>
                  </td>
                  <td className="px-4 py-2 font-mono text-xs" style={{ color: 'var(--text-primary)' }}>
                    {entry.resource}
                  </td>
                  <td className="px-4 py-2 font-medium" style={{ color: statusColor(status) }}>
                    {status ?? '\u2014'}
                  </td>
                  <td className="px-4 py-2 text-xs" style={{ color: 'var(--text-muted)' }}>
                    {entry.ip_address}
                  </td>
                </tr>
              )
            })}
            {!loading && entries.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  No audit entries found
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Footer: count + load more */}
      <div className="flex items-center justify-between px-4 py-2 shrink-0" style={{ borderTop: '1px solid var(--border-primary)' }}>
        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
          Showing {entries.length} {count > 0 ? `of ${count}` : ''} entries
        </span>
        {entries.length > 0 && entries.length < count && (
          <button
            onClick={handleLoadMore}
            disabled={loading}
            className="flex items-center gap-1.5 text-xs font-medium"
            style={{ ...btnStyle, opacity: loading ? 0.6 : 1 }}
          >
            {loading && <Loader2 size={13} className="animate-spin" />}
            Load More
          </button>
        )}
        {loading && entries.length === 0 && (
          <Loader2 size={16} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
        )}
      </div>
    </div>
  )
}
