import { useState, useEffect } from 'react'
import { Users, Loader } from 'lucide-react'
import { fetchFleetPlans } from '../../api/fleetChat'
import type { FleetPlanSummary } from '../../api/fleetChat'

// Fleet start dialog component
export default function FleetStartDialog({ onStart, onCancel, defaultMessage = '' }: { onStart: (fleetKey: string | null, message: string, planKey: string) => void; onCancel: () => void; defaultMessage?: string }) {
  const [plans, setPlans] = useState<FleetPlanSummary[]>([])
  const [selectedKey, setSelectedKey] = useState('')
  const [initialMessage, setInitialMessage] = useState(defaultMessage)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    const load = async () => {
      try {
        const planData = await fetchFleetPlans().catch(() => ({ plans: [] as FleetPlanSummary[] }))
        // Only show chat-type plans (github_issues plans are triggered by the scheduler)
        const chatPlans = (planData.plans || []).filter(p => p.channel_type === 'chat')
        setPlans(chatPlans)
        if (chatPlans.length > 0) {
          setSelectedKey(chatPlans[0].key)
        }
      } catch (err: any) {
        console.error('Failed to load fleet plans:', err)
      } finally {
        setIsLoading(false)
      }
    }
    load()
  }, [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedKey) return
    onStart(null, initialMessage, selectedKey)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md rounded-xl shadow-2xl" style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}>
        <div className="px-6 py-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
          <h2 className="text-lg font-semibold flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            <Users size={20} className="text-cyan-400" />
            Start Fleet Session
          </h2>
          <p className="text-xs mt-1" style={{ color: 'var(--text-muted)' }}>
            Launch an autonomous agent team to collaborate on a task
          </p>
        </div>
        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader size={18} className="animate-spin text-cyan-400" />
            </div>
          ) : plans.length === 0 ? (
            <div className="text-sm text-center py-4" style={{ color: 'var(--text-muted)' }}>
              <p>No fleet plans available.</p>
              <p className="mt-1 text-xs">Create a chat fleet plan first using <span className="font-mono text-cyan-400">/fleet-plan</span></p>
            </div>
          ) : (
            <>
              <div>
                <label className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Fleet Plan</label>
                <select
                  value={selectedKey}
                  onChange={(e) => setSelectedKey(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-cyan-500"
                  style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
                >
                  {plans.map(p => (
                    <option key={p.key} value={p.key}>
                      {p.name} ({p.agent_names.join(', ')})
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium mb-1.5" style={{ color: 'var(--text-secondary)' }}>Initial request (optional)</label>
                <textarea
                  value={initialMessage}
                  onChange={(e) => setInitialMessage(e.target.value)}
                  placeholder="Describe what you want the team to work on..."
                  rows={3}
                  className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-cyan-500 resize-none"
                  style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
                />
              </div>
            </>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <button type="button" onClick={onCancel} className="px-4 py-2 text-sm rounded-lg hover:bg-white/5 transition-colors" style={{ color: 'var(--text-secondary)' }}>
              Cancel
            </button>
            <button type="submit" disabled={!selectedKey || isLoading} className="px-4 py-2 text-sm bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed">
              Start Fleet
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
