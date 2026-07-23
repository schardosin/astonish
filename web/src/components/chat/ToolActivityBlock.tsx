import { useState } from 'react'
import { AlertCircle, Check, ChevronDown, ChevronRight, Loader } from 'lucide-react'
import {
  activityStats,
  activitySummary,
  formatToolPayload,
  previewValue,
  type ActivityStats,
  type ToolActivityStep,
} from './toolActivity'

interface ToolActivityBlockProps {
  steps: ToolActivityStep[]
  /** True when this block is the trailing activity during an active stream. */
  streaming?: boolean
  /** Stable key prefix for the block (e.g. activity start index). */
  blockId: string
}

function StepStatusIcon({ status }: { status: ToolActivityStep['status'] }) {
  if (status === 'running') {
    return <Loader size={12} className="animate-spin shrink-0" style={{ color: 'var(--text-muted)' }} />
  }
  if (status === 'error') {
    return <AlertCircle size={12} className="shrink-0" style={{ color: 'var(--danger)' }} />
  }
  return <Check size={12} className="shrink-0" style={{ color: 'var(--text-muted)' }} />
}

function TrailingMetric({ stats }: { stats: ActivityStats }) {
  if (stats.kind === 'diff') {
    return (
      <span className="flex items-center gap-1.5 shrink-0 text-xs font-medium tabular-nums" data-testid="activity-diff">
        {stats.added > 0 && (
          <span style={{ color: 'var(--success)' }}>+{stats.added}</span>
        )}
        {stats.removed > 0 && (
          <span style={{ color: 'var(--danger)' }}>-{stats.removed}</span>
        )}
      </span>
    )
  }
  return (
    <span
      className="shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded tabular-nums"
      data-testid="activity-badge"
      style={{
        color: 'var(--accent)',
        background: 'var(--accent-soft)',
      }}
    >
      {stats.count}
    </span>
  )
}

/**
 * Cursor-style tool activity line with accent lead, muted rest, and trailing metric.
 * Collapsed by default; expands to paired call/result steps.
 */
export default function ToolActivityBlock({ steps, streaming = false, blockId }: ToolActivityBlockProps) {
  const [expanded, setExpanded] = useState(false)
  const [expandedSteps, setExpandedSteps] = useState<Set<number>>(new Set())

  const summary = activitySummary(steps, { streaming })
  const stats = activityStats(steps)
  const showSpinner = summary.variant === 'running'

  const toggleStep = (stepIndex: number) => {
    setExpandedSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepIndex)) next.delete(stepIndex)
      else next.add(stepIndex)
      return next
    })
  }

  return (
    <div
      className="my-0.5 text-sm"
      data-testid="tool-activity-block"
      data-block-id={blockId}
    >
      <button
        type="button"
        onClick={() => setExpanded(v => !v)}
        className="group inline-flex max-w-full items-center gap-1.5 py-0.5 text-left"
        style={{
          cursor: 'pointer',
          background: 'transparent',
          border: 'none',
        }}
        aria-expanded={expanded}
        aria-label={summary.text}
      >
        {showSpinner && (
          <Loader size={12} className="animate-spin shrink-0" style={{ color: 'var(--accent)' }} />
        )}
        {summary.variant === 'error' && !showSpinner && (
          <AlertCircle size={12} className="shrink-0" style={{ color: 'var(--danger)' }} />
        )}
        <span className="text-xs truncate min-w-0">
          <span style={{ color: 'var(--accent)', fontWeight: 500 }}>
            {summary.lead}
          </span>
          {summary.rest && (
            <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>
              {summary.rest}
            </span>
          )}
          {summary.errorSuffix && (
            <span style={{ color: 'var(--danger)', fontWeight: 400 }}>
              {summary.errorSuffix}
            </span>
          )}
        </span>
        {!showSpinner && <TrailingMetric stats={stats} />}
        <span
          className="shrink-0 opacity-0 group-hover:opacity-100 transition-opacity"
          data-testid="activity-chevron"
          aria-hidden
        >
          {expanded ? (
            <ChevronDown size={14} style={{ color: 'var(--text-secondary)' }} />
          ) : (
            <ChevronRight size={14} style={{ color: 'var(--text-secondary)' }} />
          )}
        </span>
      </button>

      {expanded && (
        <div
          className="mt-1 rounded-lg overflow-hidden"
          style={{
            border: '1px solid var(--border-color)',
            background: 'var(--bg-secondary)',
          }}
        >
          {steps.map((step, stepIndex) => {
            const stepOpen = expandedSteps.has(stepIndex)
            const preview = step.status === 'running'
              ? previewValue(step.args)
              : previewValue(step.result ?? step.args)
            return (
              <div key={`${blockId}-step-${stepIndex}`}>
                {stepIndex > 0 && (
                  <div style={{ borderTop: '1px solid var(--border-color)' }} />
                )}
                <button
                  type="button"
                  onClick={() => toggleStep(stepIndex)}
                  className="w-full flex items-center gap-2 px-3 py-1.5 text-left hover:bg-black/5 dark:hover:bg-white/5"
                  style={{ color: 'var(--text-secondary)' }}
                  aria-expanded={stepOpen}
                >
                  {stepOpen ? (
                    <ChevronDown size={12} className="shrink-0" style={{ color: 'var(--text-muted)' }} />
                  ) : (
                    <ChevronRight size={12} className="shrink-0" style={{ color: 'var(--text-muted)' }} />
                  )}
                  <StepStatusIcon status={step.status} />
                  <code
                    className="text-xs px-1 py-0.5 rounded shrink-0"
                    style={{
                      background: 'var(--bg-tertiary, rgba(0,0,0,0.06))',
                      color: 'var(--text-primary)',
                    }}
                  >
                    {step.toolName}
                  </code>
                  {preview && (
                    <span className="text-xs truncate flex-1" style={{ color: 'var(--text-muted)' }}>
                      {preview}
                    </span>
                  )}
                </button>
                {stepOpen && (
                  <div className="px-3 pb-2 space-y-2">
                    {step.args !== undefined && (
                      <div>
                        <div className="text-[10px] uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>
                          Arguments
                        </div>
                        <pre
                          className="text-xs whitespace-pre-wrap break-words font-mono p-2 rounded"
                          style={{
                            background: 'var(--bg-primary, rgba(0,0,0,0.2))',
                            color: 'var(--text-secondary)',
                            maxHeight: 240,
                            overflowY: 'auto',
                          }}
                        >
                          {formatToolPayload(step.args)}
                        </pre>
                      </div>
                    )}
                    {step.result !== undefined && (
                      <div>
                        <div className="text-[10px] uppercase tracking-wide mb-1" style={{ color: 'var(--text-muted)' }}>
                          Result
                        </div>
                        <pre
                          className="text-xs whitespace-pre-wrap break-words font-mono p-2 rounded"
                          style={{
                            background: 'var(--bg-primary, rgba(0,0,0,0.2))',
                            color: 'var(--text-secondary)',
                            maxHeight: 240,
                            overflowY: 'auto',
                          }}
                        >
                          {formatToolPayload(step.result)}
                        </pre>
                      </div>
                    )}
                    {step.status === 'running' && step.result === undefined && (
                      <div className="text-xs flex items-center gap-1.5 py-1" style={{ color: 'var(--text-muted)' }}>
                        <Loader size={12} className="animate-spin" />
                        Waiting for result…
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
