import { useState, useMemo } from 'react'
import { ChevronRight, ChevronDown, Loader, Check, Wrench } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { FleetExecutionMessage, FleetEvent } from './chatTypes'

// Collapsible fleet execution panel showing real-time progress of fleet phases.
// The orchestrator renders inline (no collapsible header). Agent phases are
// collapsible, but their contents (tool calls, text, etc.) are always visible
// with truncated output and a "Show more" button for long content.
export default function FleetExecutionPanel({ data }: { data: FleetExecutionMessage }) {
  const [expanded, setExpanded] = useState(true)
  const [expandedPhases, setExpandedPhases] = useState<Record<string, boolean>>({})
  const [expandedContent, setExpandedContent] = useState<Set<string>>(new Set())

  interface Phase {
    name: string
    agent: string
    events: FleetEvent[]
    status: string
  }

  // Group events by phase
  const phases = useMemo(() => {
    const phaseMap: Record<string, Phase> = {}
    const phaseOrder: string[] = []
    for (const evt of data.events) {
      if (evt.type === 'fleet_start' || evt.type === 'fleet_complete') continue
      const key = evt.phase || '_orchestrator'
      if (!phaseMap[key]) {
        phaseMap[key] = { name: key, agent: evt.agent || '', events: [], status: 'pending' }
        phaseOrder.push(key)
      }
      phaseMap[key].events.push(evt)
      if (evt.type === 'phase_start') phaseMap[key].status = 'running'
      if (evt.type === 'phase_complete') phaseMap[key].status = 'complete'
      if (evt.type === 'phase_failed') phaseMap[key].status = 'failed'
      if (evt.type === 'conversation_start') phaseMap[key].status = 'running'
      if (evt.type === 'conversation_complete') phaseMap[key].status = 'complete'
      if (evt.type === 'conversation_turn_failed') phaseMap[key].status = 'failed'
      if (evt.agent) phaseMap[key].agent = evt.agent
    }
    return phaseOrder.map(k => phaseMap[k])
  }, [data.events])

  // Count non-orchestrator phases for the header
  const agentPhaseCount = phases.filter(p => p.name !== '_orchestrator').length

  const togglePhase = (name: string) => {
    setExpandedPhases(prev => ({ ...prev, [name]: !prev[name] }))
  }

  const toggleContent = (eventKey: string) => {
    setExpandedContent(prev => {
      const next = new Set(prev)
      if (next.has(eventKey)) next.delete(eventKey)
      else next.add(eventKey)
      return next
    })
  }

  const statusIcon = (status: string) => {
    if (status === 'running') return <Loader size={12} className="animate-spin text-cyan-400" />
    if (status === 'complete') return <Check size={12} className="text-green-400" />
    if (status === 'failed') return <span className="text-red-400 text-xs font-bold">!</span>
    return <span className="text-gray-500 text-xs">-</span>
  }

  // Content truncation threshold (characters). Content longer than this shows
  // a truncated preview with a "Show more" button.
  const TRUNCATE_THRESHOLD = 800

  // Render the content area of a tool card. Always visible (no collapse toggle).
  // Long content is truncated with a "Show more" / "Show less" button.
  const renderCardContent = (cardData: unknown, eventKey: string) => {
    if (cardData == null || cardData === undefined) return null
    const text = typeof cardData === 'string' ? cardData : JSON.stringify(cardData, null, 2)
    if (!text) return null

    const isTruncatable = text.length > TRUNCATE_THRESHOLD
    const isFullyExpanded = expandedContent.has(eventKey)
    const displayText = (isTruncatable && !isFullyExpanded) ? text.slice(0, TRUNCATE_THRESHOLD) : text

    return (
      <div className="px-3 pb-2">
        <div className="relative">
          <pre
            className="text-xs whitespace-pre-wrap break-words font-mono p-2 rounded"
            style={{
              background: 'rgba(0,0,0,0.3)',
              color: 'var(--text-secondary)',
              maxHeight: isFullyExpanded ? 'none' : '200px',
              overflowY: 'hidden',
            }}
          >
            {displayText}
          </pre>
          {isTruncatable && !isFullyExpanded && (
            <div
              className="absolute bottom-0 left-0 right-0 h-10 flex items-end justify-center rounded-b"
              style={{ background: 'linear-gradient(transparent, rgba(0,0,0,0.5))' }}
            >
              <button
                onClick={() => toggleContent(eventKey)}
                className="text-[10px] text-cyan-400 hover:text-cyan-300 px-2 py-0.5 mb-1 rounded bg-black/50 cursor-pointer"
              >
                Show more ({Math.ceil(text.length / 1000)}k chars)
              </button>
            </div>
          )}
          {isTruncatable && isFullyExpanded && (
            <div className="flex justify-center mt-1">
              <button
                onClick={() => toggleContent(eventKey)}
                className="text-[10px] text-cyan-400 hover:text-cyan-300 px-2 py-0.5 rounded bg-black/30 cursor-pointer"
              >
                Show less
              </button>
            </div>
          )}
        </div>
      </div>
    )
  }

  // Render a tool call/result card — always expanded, no collapse toggle
  const renderFleetToolCard = (evt: FleetEvent, eventKey: string) => {
    const isCall = evt.type === 'worker_tool_call' || evt.type === 'tool_call' || evt.type === 'opencode_tool_call'
    const name = evt.detail || 'unknown'
    const isOpenCode = evt.type.startsWith('opencode_')
    const cardData = isCall ? (evt.args || evt.text || null) : (evt.result || evt.text || null)

    return (
      <div
        key={eventKey}
        className="my-1.5 rounded-lg overflow-hidden"
        style={{ border: `1px solid ${isOpenCode ? 'rgba(6,182,212,0.3)' : 'var(--border-color)'}`, background: isOpenCode ? 'rgba(6,182,212,0.03)' : 'rgba(255,255,255,0.03)' }}
      >
        <div className="flex items-center gap-2 px-3 py-1.5">
          <Wrench size={12} className={isOpenCode ? 'text-cyan-400' : isCall ? 'text-purple-400' : 'text-green-400'} />
          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
            {isOpenCode && <span className="text-cyan-400 mr-1">OpenCode</span>}
            {isCall ? 'Tool Call' : 'Tool Result'}: <code className={`${isOpenCode ? 'bg-cyan-500/15 text-cyan-300' : 'bg-purple-500/15 text-purple-300'} px-1 py-0.5 rounded text-[11px]`}>{name}</code>
          </span>
        </div>
        {renderCardContent(cardData, eventKey)}
      </div>
    )
  }

  // Render agent text
  const renderFleetText = (evt: FleetEvent, eventKey: string) => {
    const textContent = evt.text || evt.message || ''
    if (!textContent) return null
    const isOpenCode = evt.type === 'opencode_text'

    return (
      <div key={eventKey} className="my-1.5">
        <div
          className="p-3 rounded-lg text-sm"
          style={{
            background: isOpenCode ? 'rgba(6,182,212,0.05)' : 'rgba(255,255,255,0.05)',
            border: `1px solid ${isOpenCode ? 'rgba(6,182,212,0.2)' : 'var(--border-color)'}`,
          }}
        >
          {isOpenCode && (
            <div className="text-[10px] font-medium text-cyan-400 mb-1">OpenCode</div>
          )}
          <div className="markdown-body text-xs" style={{ color: 'var(--text-primary)' }}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{textContent}</ReactMarkdown>
          </div>
        </div>
      </div>
    )
  }

  // Render a conversation turn indicator
  const renderConversationTurn = (evt: FleetEvent, eventKey: string) => {
    const isComplete = evt.type === 'conversation_turn_complete'
    const isFailed = evt.type === 'conversation_turn_failed'
    const role = evt.detail || 'agent'

    return (
      <div key={eventKey} className="my-1.5">
        <div
          className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs"
          style={{
            background: isFailed ? 'rgba(239,68,68,0.1)' : 'rgba(139,92,246,0.1)',
            border: `1px solid ${isFailed ? 'rgba(239,68,68,0.3)' : 'rgba(139,92,246,0.2)'}`,
          }}
        >
          {isComplete ? (
            <Check size={12} className="text-green-400" />
          ) : isFailed ? (
            <span className="text-red-400 text-xs font-bold">!</span>
          ) : (
            <Loader size={12} className="animate-spin text-purple-400" />
          )}
          <span className="text-purple-300 font-medium">{evt.agent || 'agent'}</span>
          <span className="text-gray-400">({role})</span>
          {isComplete && evt.text && (
            <span className="text-gray-500 ml-2 truncate max-w-[300px]">{evt.text}</span>
          )}
        </div>
      </div>
    )
  }

  // Render a single event based on its type
  const renderPhaseEvent = (evt: FleetEvent, phaseIdx: number, evtIdx: number) => {
    const eventKey = `fleet-${phaseIdx}-${evtIdx}`
    if (evt.type === 'phase_start' || evt.type === 'phase_complete' || evt.type === 'phase_failed') {
      return null
    }
    if (evt.type === 'conversation_start' || evt.type === 'conversation_complete') {
      return null
    }
    if (evt.type === 'conversation_turn' || evt.type === 'conversation_turn_complete' || evt.type === 'conversation_turn_failed') {
      return renderConversationTurn(evt, eventKey)
    }
    if (evt.type === 'worker_tool_call' || evt.type === 'tool_call' || evt.type === 'opencode_tool_call') {
      return renderFleetToolCard(evt, eventKey)
    }
    if (evt.type === 'worker_tool_result' || evt.type === 'tool_result' || evt.type === 'opencode_tool_result') {
      return renderFleetToolCard(evt, eventKey)
    }
    if (evt.type === 'worker_text' || evt.type === 'text' || evt.type === 'opencode_text') {
      return renderFleetText(evt, eventKey)
    }
    if (evt.type === 'opencode_step_start' || evt.type === 'opencode_step_finish') {
      const isStart = evt.type === 'opencode_step_start'
      return (
        <div key={eventKey} className="my-1 flex items-center gap-2 px-2 py-1 text-[10px]" style={{ color: 'var(--text-muted)' }}>
          {isStart ? (
            <Loader size={10} className="animate-spin text-cyan-400" />
          ) : (
            <Check size={10} className="text-cyan-400" />
          )}
           <span className="text-cyan-400/70">{evt.message || (isStart ? 'OpenCode step started' : 'OpenCode step finished')}</span>
        </div>
      )
    }
    return null
  }

  return (
    <div className="rounded-lg border border-cyan-500/30 bg-cyan-500/5 overflow-hidden text-sm">
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-cyan-400 hover:bg-cyan-500/10 transition-colors cursor-pointer"
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        {data.status === 'running' && <Loader size={14} className="animate-spin" />}
        {data.status === 'complete' && <Check size={14} className="text-green-400" />}
        <span className="font-medium">Fleet Execution</span>
        <span className="text-cyan-400/60 text-xs ml-auto">{agentPhaseCount} phase{agentPhaseCount !== 1 ? 's' : ''}</span>
      </button>

      {/* Content */}
      {expanded && (
        <div className="border-t border-cyan-500/20 px-2 py-1">
          {phases.map((phase, phaseIdx) => {
            // Orchestrator events render inline — no collapsible header
            if (phase.name === '_orchestrator') {
              return (
                <div key={phase.name} className="pb-1">
                  {phase.events.map((evt, evtIdx) => renderPhaseEvent(evt, phaseIdx, evtIdx))}
                </div>
              )
            }

            // Agent phases render as collapsible sections
            return (
              <div key={phase.name} className="my-1">
                {/* Phase header */}
                <button
                  onClick={() => togglePhase(phase.name)}
                  className="w-full flex items-center gap-2 px-2 py-1.5 rounded hover:bg-cyan-500/10 transition-colors cursor-pointer"
                >
                  {expandedPhases[phase.name] ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  {statusIcon(phase.status)}
                  <span className="text-gray-200 font-medium">{phase.name}</span>
                  {phase.agent && <span className="text-gray-500 text-xs">({phase.agent})</span>}
                  {phase.status === 'running' && <span className="text-cyan-400/60 text-xs ml-auto">running</span>}
                </button>

                {/* Phase contents — collapsed by default, auto-expand running phase */}
                {(expandedPhases[phase.name] || (phase.status === 'running' && expandedPhases[phase.name] !== false)) && (
                  <div className="ml-4 pl-2 border-l border-cyan-500/15 pb-1">
                    {phase.events.map((evt, evtIdx) => renderPhaseEvent(evt, phaseIdx, evtIdx))}
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
