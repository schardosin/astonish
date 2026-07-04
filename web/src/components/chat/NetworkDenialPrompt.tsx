import { useState } from 'react'
import { Shield, ShieldAlert, Check, X, Loader2, ChevronDown, ChevronRight } from 'lucide-react'
import { approveNetworkGrant, approveNetworkGrantBroader, denyNetworkGrant } from '../../api/studioChat'
import type { NetworkDenialMessage } from './chatTypes'

type DenialItem = NetworkDenialMessage['denials'][number]

/**
 * NetworkDenialPrompt — shown inline in the chat when the sandbox's L7 proxy
 * blocks an outbound network connection. Offers the user approve/deny actions.
 *
 * Supports two modes:
 * 1. Denials from GetDraftPolicy (have chunk_id) — approve/deny specific chunks
 * 2. Denials extracted from stdout (no chunk_id) — approve-broader by host:port
 */
interface NetworkDenialPromptProps {
  denials: DenialItem[]
  sandboxName: string
  sessionId: string
  onApproved?: (host: string) => void
}

type DenialState = 'idle' | 'approving' | 'approving_broader' | 'denying' | 'approved' | 'denied'

// Stable key for a denial — use chunk_id if available, otherwise host:port
function denialKey(denial: DenialItem): string {
  if (denial.chunk_id) return denial.chunk_id
  return `${denial.host}:${denial.port}`
}

export default function NetworkDenialPrompt({ denials, sandboxName, sessionId, onApproved }: NetworkDenialPromptProps) {
  const [states, setStates] = useState<Record<string, DenialState>>({})
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  if (!denials || denials.length === 0) return null

  const getState = (denial: DenialItem): DenialState => states[denialKey(denial)] || 'idle'

  const handleApprove = async (denial: DenialItem) => {
    const key = denialKey(denial)
    setStates(prev => ({ ...prev, [key]: 'approving' }))
    try {
      if (denial.chunk_id) {
        // Have a chunk_id — use the specific approve endpoint
        await approveNetworkGrant(sessionId, denial.chunk_id, sandboxName)
      } else {
        // No chunk_id (stdout-extracted) — approve by host:port directly
        await approveNetworkGrantBroader(sessionId, denial.host, denial.port, sandboxName)
      }
      setStates(prev => ({ ...prev, [key]: 'approved' }))
      onApproved?.(denial.host)
    } catch {
      setStates(prev => ({ ...prev, [key]: 'idle' }))
    }
  }

  const handleApproveBroader = async (denial: DenialItem) => {
    const key = denialKey(denial)
    setStates(prev => ({ ...prev, [key]: 'approving_broader' }))
    try {
      await approveNetworkGrantBroader(sessionId, denial.broader_pattern || denial.host, denial.port, sandboxName)
      setStates(prev => ({ ...prev, [key]: 'approved' }))
      onApproved?.(denial.broader_pattern || denial.host)
    } catch {
      setStates(prev => ({ ...prev, [key]: 'idle' }))
    }
  }

  const handleDeny = async (denial: DenialItem) => {
    const key = denialKey(denial)
    setStates(prev => ({ ...prev, [key]: 'denying' }))
    try {
      if (denial.chunk_id) {
        await denyNetworkGrant(sessionId, denial.chunk_id, sandboxName)
      }
      setStates(prev => ({ ...prev, [key]: 'denied' }))
    } catch {
      setStates(prev => ({ ...prev, [key]: 'idle' }))
    }
  }

  const toggleExpand = (denial: DenialItem) => {
    const key = denialKey(denial)
    setExpanded(prev => ({ ...prev, [key]: !prev[key] }))
  }

  return (
    <div className="my-2 space-y-2">
      {denials.map(denial => {
        const key = denialKey(denial)
        const state = getState(denial)
        const isExpanded = expanded[key]
        const isTerminal = state === 'approved' || state === 'denied'
        const isBusy = state === 'approving' || state === 'approving_broader' || state === 'denying'

        return (
          <div
            key={key}
            className="rounded-lg p-3"
            style={{
              background: isTerminal
                ? state === 'approved'
                  ? 'rgba(34, 197, 94, 0.08)'
                  : 'rgba(239, 68, 68, 0.08)'
                : 'rgba(234, 179, 8, 0.08)',
              border: `1px solid ${
                isTerminal
                  ? state === 'approved'
                    ? 'rgba(34, 197, 94, 0.25)'
                    : 'rgba(239, 68, 68, 0.25)'
                  : 'rgba(234, 179, 8, 0.25)'
              }`,
            }}
          >
            {/* Header */}
            <div className="flex items-center gap-2 mb-1.5">
              {isTerminal ? (
                state === 'approved' ? (
                  <Shield size={14} style={{ color: '#22c55e' }} />
                ) : (
                  <X size={14} style={{ color: '#ef4444' }} />
                )
              ) : (
                <ShieldAlert size={14} style={{ color: '#eab308' }} />
              )}
              <span
                className="text-xs font-medium"
                style={{
                  color: isTerminal
                    ? state === 'approved' ? '#22c55e' : '#ef4444'
                    : '#eab308',
                }}
              >
                {isTerminal
                  ? state === 'approved' ? 'Network access granted' : 'Network access denied'
                  : 'Network access blocked'}
              </span>
            </div>

            {/* Endpoint info */}
            <div className="text-xs mb-2" style={{ color: 'var(--text-secondary)' }}>
              <code
                className="px-1.5 py-0.5 rounded"
                style={{ background: 'rgba(255,255,255,0.06)' }}
              >
                {denial.host}:{denial.port}
              </code>
              {denial.binary && (
                <span className="ml-2" style={{ color: 'var(--text-muted)' }}>
                  via <code className="px-1 py-0.5 rounded" style={{ background: 'rgba(255,255,255,0.04)' }}>{denial.binary}</code>
                </span>
              )}
            </div>

            {/* Expandable details */}
            {(denial.rationale || denial.security_notes) && (
              <button
                onClick={() => toggleExpand(denial)}
                className="flex items-center gap-1 text-xs mb-2 transition-colors"
                style={{ color: 'var(--text-muted)' }}
              >
                {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                Details
              </button>
            )}
            {isExpanded && (
              <div className="text-xs mb-2 pl-4 space-y-1" style={{ color: 'var(--text-muted)' }}>
                {denial.rationale && <p>{denial.rationale}</p>}
                {denial.security_notes && (
                  <p style={{ color: 'rgba(234, 179, 8, 0.7)' }}>{denial.security_notes}</p>
                )}
              </div>
            )}

            {/* Action buttons */}
            {!isTerminal && (
              <div className="flex items-center gap-2 flex-wrap">
                <button
                  onClick={() => handleApprove(denial)}
                  disabled={isBusy}
                  className="flex items-center gap-1 px-2.5 py-1 rounded-md text-xs transition-colors"
                  style={{
                    background: 'rgba(34, 197, 94, 0.15)',
                    color: '#4ade80',
                    border: '1px solid rgba(34, 197, 94, 0.3)',
                    cursor: isBusy ? 'wait' : 'pointer',
                    opacity: isBusy ? 0.6 : 1,
                  }}
                >
                  {state === 'approving' ? (
                    <Loader2 size={11} className="animate-spin" />
                  ) : (
                    <Check size={11} />
                  )}
                  Allow {denial.host}
                </button>

                {denial.broader_pattern && (
                  <button
                    onClick={() => handleApproveBroader(denial)}
                    disabled={isBusy}
                    className="flex items-center gap-1 px-2.5 py-1 rounded-md text-xs transition-colors"
                    style={{
                      background: 'rgba(59, 130, 246, 0.12)',
                      color: '#60a5fa',
                      border: '1px solid rgba(59, 130, 246, 0.25)',
                      cursor: isBusy ? 'wait' : 'pointer',
                      opacity: isBusy ? 0.6 : 1,
                    }}
                  >
                    {state === 'approving_broader' ? (
                      <Loader2 size={11} className="animate-spin" />
                    ) : (
                      <Shield size={11} />
                    )}
                    Allow {denial.broader_pattern}
                  </button>
                )}

                <button
                  onClick={() => handleDeny(denial)}
                  disabled={isBusy}
                  className="flex items-center gap-1 px-2.5 py-1 rounded-md text-xs transition-colors"
                  style={{
                    background: 'rgba(239, 68, 68, 0.12)',
                    color: '#f87171',
                    border: '1px solid rgba(239, 68, 68, 0.25)',
                    cursor: isBusy ? 'wait' : 'pointer',
                    opacity: isBusy ? 0.6 : 1,
                  }}
                >
                  {state === 'denying' ? (
                    <Loader2 size={11} className="animate-spin" />
                  ) : (
                    <X size={11} />
                  )}
                  Deny
                </button>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
