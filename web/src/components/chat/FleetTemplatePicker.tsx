import { useState, useEffect } from 'react'
import { Users, Loader } from 'lucide-react'
import { fetchFleets } from '../../api/fleetChat'
import type { FleetDefinition } from '../../api/fleetChat'

// Dialog shown when /fleet-plan is called without a template key.
// Lists available fleet templates so the user can pick one, then re-issues
// /fleet-plan <key> to trigger the full wizard flow with system prompt injection.
export default function FleetTemplatePicker({ onSelect, onCancel }: { onSelect: (key: string) => void; onCancel: () => void }) {
  const [templates, setTemplates] = useState<FleetDefinition[]>([])
  const [selectedKey, setSelectedKey] = useState('')
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    const load = async () => {
      try {
        const data = await fetchFleets()
        const loaded = data.fleets || []
        setTemplates(loaded)
        if (loaded.length > 0) {
          setSelectedKey(loaded[0].key)
        }
      } catch (err: any) {
        console.error('Failed to load fleet templates:', err)
      } finally {
        setIsLoading(false)
      }
    }
    load()
  }, [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedKey) return
    onSelect(selectedKey)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-xl shadow-2xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
        <div className="px-6 py-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
          <h2 className="text-lg font-semibold flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            <Users size={20} className="text-cyan-400" />
            Create Fleet Plan
          </h2>
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Select a fleet template to configure
          </p>
        </div>
        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader size={18} className="animate-spin text-cyan-400" />
            </div>
          ) : templates.length === 0 ? (
            <p className="text-sm text-center py-4" style={{ color: 'var(--text-muted)' }}>No fleet templates available</p>
          ) : (
            <div>
              <label className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Fleet Template</label>
              <select
                value={selectedKey}
                onChange={(e) => setSelectedKey(e.target.value)}
                className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-cyan-500"
                style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
              >
                {templates.map(t => (
                  <option key={t.key} value={t.key}>
                    {t.name} ({t.agent_names.join(', ')})
                  </option>
                ))}
              </select>
              {selectedKey && templates.find(t => t.key === selectedKey)?.description && (
                <p className="mt-2 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {templates.find(t => t.key === selectedKey)!.description}
                </p>
              )}
            </div>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <button type="button" onClick={onCancel} className="px-4 py-2 text-sm rounded-lg hover:bg-white/5 transition-colors" style={{ color: 'var(--text-secondary)' }}>
              Cancel
            </button>
            <button type="submit" disabled={!selectedKey || isLoading} className="px-4 py-2 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
              Continue
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
