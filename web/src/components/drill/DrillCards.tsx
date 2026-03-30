import { useState } from 'react'
import { ChevronDown, ChevronRight, Check, X, Shield } from 'lucide-react'
import { formatDuration, statusColor, StatusDot } from './drillUtils'

// ─── Step Card ───

interface StepCardProps {
  node: Record<string, any>
  index: number
}

export function StepCard({ node, index }: StepCardProps) {
  const [expanded, setExpanded] = useState(false)

  const args = node.args || {}
  const toolName = args.tool || node.type || 'step'
  const assertion = node.assert // single object or null
  // Build display args: everything in args except the "tool" key
  const displayArgs: [string, any][] = Object.entries(args).filter(([k]) => k !== 'tool')

  return (
    <div
      className="relative pl-9 cursor-pointer"
      onClick={() => setExpanded(!expanded)}
    >
      {/* Timeline dot */}
      <div
        className="absolute left-2 top-3 w-[14px] h-[14px] rounded-full border-2 flex items-center justify-center"
        style={{ borderColor: '#f59e0b', background: 'var(--bg-primary)' }}
      >
        <span className="text-[8px] font-bold" style={{ color: '#f59e0b' }}>{index + 1}</span>
      </div>

      <div
        className="p-3 rounded-lg transition-colors"
        style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
              {node.name || `Step ${index + 1}`}
            </span>
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded" style={{ background: 'rgba(245, 158, 11, 0.1)', color: '#fbbf24' }}>
              {toolName}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {assertion && (
              <span className="text-[10px] flex items-center gap-0.5" style={{ color: 'var(--text-muted)' }}>
                <Shield size={10} /> 1 assertion
              </span>
            )}
            {expanded ? <ChevronDown size={12} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={12} style={{ color: 'var(--text-muted)' }} />}
          </div>
        </div>

        {/* Show a preview of the command/action even when collapsed */}
        {!expanded && displayArgs.length > 0 && (
          <p className="text-[11px] mt-1 font-mono truncate" style={{ color: 'var(--text-muted)' }}>
            {displayArgs.map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`).join(', ')}
          </p>
        )}

        {expanded && (
          <div className="mt-3 space-y-2">
            {/* Tool Arguments */}
            {displayArgs.length > 0 && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Arguments</span>
                <div className="mt-1 space-y-1">
                  {displayArgs.map(([key, value]) => (
                    <div key={key} className="flex items-start gap-2 text-[11px] p-1.5 rounded" style={{ background: 'rgba(0,0,0,0.2)' }}>
                      <span className="font-mono font-medium flex-shrink-0" style={{ color: '#fbbf24' }}>{key}:</span>
                      <span className="font-mono break-all" style={{ color: 'var(--text-primary)' }}>
                        {typeof value === 'string' ? value : JSON.stringify(value, null, 2)}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Assertion */}
            {assertion && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Assertion</span>
                <div className="mt-1 p-2 rounded" style={{ background: 'rgba(0,0,0,0.2)' }}>
                  <div className="flex items-start gap-2 text-[11px] font-mono">
                    <Shield size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#f59e0b' }} />
                    <div>
                      <span style={{ color: '#fbbf24' }}>{assertion.type}</span>
                      {assertion.source && assertion.source !== 'output' && (
                        <span style={{ color: 'var(--text-muted)' }}> (source: {assertion.source})</span>
                      )}
                      <span style={{ color: 'var(--text-muted)' }}> = </span>
                      <span style={{ color: 'var(--text-primary)' }}>{assertion.expected}</span>
                      {assertion.on_fail && (
                        <span className="ml-2 text-[10px]" style={{ color: 'var(--text-muted)' }}>[on_fail: {assertion.on_fail}]</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Prompt (for LLM nodes) */}
            {node.prompt && (
              <div>
                <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Prompt</span>
                <pre className="mt-1 text-[11px] font-mono p-2 rounded overflow-x-auto whitespace-pre-wrap" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-primary)' }}>
                  {node.prompt}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}


// ─── Report Step Card ───

interface ReportStepCardProps {
  step: Record<string, any>
  index: number
}

export function ReportStepCard({ step, index }: ReportStepCardProps) {
  const [expanded, setExpanded] = useState(false)
  const sc = statusColor(step.status)
  const assertions: Record<string, any>[] = step.assertion ? [step.assertion] : []

  return (
    <div
      className="p-2 rounded cursor-pointer hover:bg-white/5 transition-colors"
      style={{ background: sc.bg, border: `1px solid ${sc.border}` }}
      onClick={() => setExpanded(!expanded)}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <StatusDot status={step.status} size={6} />
          <span className="text-[11px] font-medium" style={{ color: 'var(--text-primary)' }}>
            {step.name || `Step ${index + 1}`}
          </span>
          {step.tool && (
            <span className="text-[10px] font-mono px-1 rounded" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-muted)' }}>
              {step.tool}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {step.duration > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>{formatDuration(step.duration)}</span>
          )}
          {assertions.length > 0 && (
            <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
              {assertions.filter((a: Record<string, any>) => a.passed).length}/{assertions.length}
            </span>
          )}
          {expanded ? <ChevronDown size={10} style={{ color: 'var(--text-muted)' }} /> : <ChevronRight size={10} style={{ color: 'var(--text-muted)' }} />}
        </div>
      </div>

      {expanded && (
        <div className="mt-2 space-y-2">
          {/* Assertions */}
          {assertions.length > 0 && (
            <div className="space-y-1">
              {assertions.map((a: Record<string, any>, i: number) => (
                <div key={i} className="flex items-start gap-1.5 text-[10px]">
                  {a.passed ? (
                    <Check size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#22c55e' }} />
                  ) : (
                    <X size={10} className="mt-0.5 flex-shrink-0" style={{ color: '#ef4444' }} />
                  )}
                  <span style={{ color: 'var(--text-primary)' }}>
                    {a.message || a.description || `${a.type}: ${a.expected || ''}`}
                  </span>
                  {!a.passed && a.actual && (
                    <span className="font-mono" style={{ color: '#f87171' }}> (got: {a.actual})</span>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Raw output */}
          {step.output && (
            <div>
              <span className="text-[10px] font-semibold uppercase" style={{ color: 'var(--text-muted)' }}>Output</span>
              <pre className="mt-1 text-[10px] font-mono p-2 rounded overflow-x-auto max-h-40 overflow-y-auto" style={{ background: 'rgba(0,0,0,0.2)', color: 'var(--text-primary)' }}>
                {typeof step.output === 'string' ? step.output : JSON.stringify(step.output, null, 2)}
              </pre>
            </div>
          )}

          {/* Error */}
          {step.error && (
            <div className="p-1.5 rounded text-[10px] font-mono" style={{ background: 'rgba(239, 68, 68, 0.1)', color: '#f87171' }}>
              {step.error}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
