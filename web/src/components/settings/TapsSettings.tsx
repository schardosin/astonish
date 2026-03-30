import { useState, useEffect } from 'react'
import { Plus, Trash2, Check, AlertCircle, Loader2, RefreshCw } from 'lucide-react'
import { fetchTaps, addTap, removeTap } from './settingsApi'
import type { TapEntry } from './settingsApi'

export default function TapsSettings() {
  const [taps, setTaps] = useState<TapEntry[]>([])
  const [tapsLoading, setTapsLoading] = useState(false)
  const [tapsSuccess, setTapsSuccess] = useState<string | null>(null)
  const [newTapUrl, setNewTapUrl] = useState('')
  const [newTapAlias, setNewTapAlias] = useState('')
  const [tapsError, setTapsError] = useState<string | null>(null)

  useEffect(() => {
    setTapsLoading(true)
    fetchTaps()
      .then(data => setTaps(data.taps || []))
      .catch((err: any) => setTapsError(err.message))
      .finally(() => setTapsLoading(false))
  }, [])

  return (
    <div className="max-w-2xl space-y-6">
      <p style={{ color: 'var(--text-muted)' }}>
        Manage extension repositories (taps) that provide flows and MCP servers.
      </p>

      {/* Add Tap Form */}
      <div className="p-4 rounded-lg border" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}>
        <h4 className="font-medium mb-3" style={{ color: 'var(--text-primary)' }}>Add Repository</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary)' }}>Repository URL or owner/repo</label>
            <input
              type="text"
              value={newTapUrl}
              onChange={(e) => setNewTapUrl(e.target.value)}
              placeholder="schardosin/astonish-flows"
              className="w-full px-3 py-2 rounded border"
              style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            />
          </div>
          <div>
            <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary)' }}>Alias (optional)</label>
            <input
              type="text"
              value={newTapAlias}
              onChange={(e) => setNewTapAlias(e.target.value)}
              placeholder="my-flows"
              className="w-full px-3 py-2 rounded border"
              style={{ background: 'var(--bg-tertiary)', borderColor: 'var(--border-color)', color: 'var(--text-primary)' }}
            />
          </div>
          {tapsError && (
            <div className="text-red-400 text-sm flex items-center gap-2">
              <AlertCircle size={14} />
              {tapsError}
            </div>
          )}
          <button
            onClick={async () => {
              if (!newTapUrl) return
              setTapsError(null)
              setTapsLoading(true)
              try {
                await addTap(newTapUrl, newTapAlias)
                setNewTapUrl('')
                setNewTapAlias('')
                const data = await fetchTaps()
                setTaps(data.taps || [])
              } catch (err: any) {
                setTapsError(err.message)
              } finally {
                setTapsLoading(false)
              }
            }}
            disabled={tapsLoading || !newTapUrl}
            className="flex items-center gap-2 px-4 py-2 rounded-lg transition-all shadow-md hover:shadow-lg hover:scale-[1.02] active:scale-95 disabled:opacity-50"
            style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)', color: '#fff' }}
          >
            {tapsLoading ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
            Add Repository
          </button>
        </div>
      </div>

      {/* Tap List */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <h4 className="font-medium" style={{ color: 'var(--text-primary)' }}>Configured Repositories</h4>
          <div className="flex items-center gap-2">
            {tapsSuccess && (
              <span className="flex items-center gap-1 text-sm text-green-400">
                <Check size={14} />
                {tapsSuccess}
              </span>
            )}
            <button
              onClick={async () => {
                setTapsLoading(true)
                setTapsError(null)
                setTapsSuccess(null)
                try {
                  // First refresh manifests from remote
                  await fetch('/api/taps/update', { method: 'POST' })
                  // Then fetch updated taps list
                  const data = await fetchTaps()
                  setTaps(data.taps || [])
                  setTapsSuccess('Updated!')
                  setTimeout(() => setTapsSuccess(null), 3000)
                } catch (err: any) {
                  setTapsError(err.message)
                } finally {
                  setTapsLoading(false)
                }
              }}
              disabled={tapsLoading}
              className="flex items-center gap-1.5 px-2 py-1 rounded text-sm hover:bg-gray-600/30 transition-colors disabled:opacity-50"
              style={{ color: 'var(--text-muted)' }}
              title="Refresh manifests from remote"
            >
              <RefreshCw size={14} className={tapsLoading ? 'animate-spin' : ''} />
              {tapsLoading ? 'Refreshing...' : 'Refresh'}
            </button>
          </div>
        </div>
        {taps.length === 0 ? (
          <div className="text-sm p-4 rounded border border-dashed" 
               style={{ borderColor: 'var(--border-color)', color: 'var(--text-muted)' }}>
            No repositories configured. Add one above or click refresh.
          </div>
        ) : (
          taps.map((tap) => (
            <div 
              key={tap.name} 
              className="flex items-center justify-between p-3 rounded-lg border"
              style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }}
            >
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium" style={{ color: 'var(--text-primary)' }}>{tap.name}</span>
                  {tap.name === 'official' && (
                    <span className="text-xs px-2 py-0.5 rounded bg-purple-600/20 text-purple-400">official</span>
                  )}
                </div>
                <div className="text-sm" style={{ color: 'var(--text-muted)' }}>{tap.url}</div>
              </div>
              {tap.name !== 'official' && (
                <button
                  onClick={async () => {
                    try {
                      await removeTap(tap.name)
                      const data = await fetchTaps()
                      setTaps(data.taps || [])
                    } catch (err: any) {
                      setTapsError(err.message)
                    }
                  }}
                  className="p-2 text-red-400 hover:bg-red-600/20 rounded"
                >
                  <Trash2 size={16} />
                </button>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
