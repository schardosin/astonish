import { Loader, Check, Circle, AlertCircle, ListChecks } from 'lucide-react'
import type { PlanMessage } from './chatTypes'

// Flat checklist panel showing the high-level execution plan.
// Announced by the orchestrator via announce_plan before starting work.
// Steps transition through pending → running → complete/failed as work progresses.
export default function PlanPanel({ data }: { data: PlanMessage }) {
  const completedCount = data.steps.filter(s => s.status === 'complete').length
  const totalCount = data.steps.length
  const hasRunning = data.steps.some(s => s.status === 'running')
  const allDone = completedCount === totalCount && totalCount > 0

  const statusIcon = (status: string) => {
    switch (status) {
      case 'running':
        return <Loader size={14} className="animate-spin text-blue-400 shrink-0" />
      case 'complete':
        return <Check size={14} className="text-green-400 shrink-0" />
      case 'failed':
        return <AlertCircle size={14} className="text-red-400 shrink-0" />
      default:
        return <Circle size={10} className="text-gray-500 shrink-0 ml-0.5 mr-0.5" />
    }
  }

  return (
    <div
      className="rounded-lg overflow-hidden text-sm"
      style={{
        border: '1px solid var(--border-color)',
        background: 'var(--bg-secondary)',
      }}
    >
      {/* Header */}
      <div className="flex items-center gap-2 px-4 py-2.5">
        {hasRunning && !allDone ? (
          <Loader size={15} className="animate-spin text-blue-400 shrink-0" />
        ) : allDone ? (
          <Check size={15} className="text-green-400 shrink-0" />
        ) : (
          <ListChecks size={15} className="text-blue-400 shrink-0" />
        )}
        <span className="font-semibold text-sm" style={{ color: 'var(--text-primary)' }}>
          {data.goal}
        </span>
        <span className="text-xs ml-auto shrink-0" style={{ color: 'var(--text-muted)' }}>
          {completedCount}/{totalCount}
        </span>
      </div>

      {/* Steps */}
      <div
        className="px-4 pb-3 space-y-1.5"
        style={{ borderTop: '1px solid var(--border-color)' }}
      >
        {data.steps.map((step) => (
          <div key={step.name} className="flex items-center gap-2.5 py-1">
            {statusIcon(step.status)}
            <span
              className="text-xs"
              style={{
                color: step.status === 'complete'
                  ? 'var(--text-muted)'
                  : step.status === 'running'
                    ? 'var(--text-primary)'
                    : 'var(--text-secondary)',
                textDecoration: step.status === 'complete' ? 'line-through' : 'none',
                opacity: step.status === 'pending' ? 0.7 : 1,
              }}
            >
              {step.description || step.name}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
