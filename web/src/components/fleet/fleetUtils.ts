// ─── Local types for shapes the backend returns but the API layer types loosely ───

import type { ChatSession } from '../../api/studioChat'
import type { FleetPlanStatus, FleetThread } from '../../api/fleetChat'

/** ChatSession extended with fleet-specific fields the backend may attach. */
export interface FleetChatSession extends ChatSession {
  fleetKey?: string
  fleetName?: string
  issueNumber?: string | number
}

/** FleetPlanStatus extended with polling-error and failed-issue fields. */
export interface FleetPlanStatusExt extends FleetPlanStatus {
  last_poll_error?: string
  failed_issues?: Array<{
    issue_number: number
    session_id?: string
    failed_at?: string
    error?: string
  }>
}

/** FleetThread extended – backend may return agent_key alongside thread_key. */
export interface FleetThreadExt extends FleetThread {
  agent_key?: string
}

/** Structured plan data (fetchFleetPlan returns Record<string, unknown>). */
export interface FleetPlanData {
  name?: string
  description?: string
  created_from?: string
  channel?: {
    type?: string
    schedule?: string
    config?: Record<string, unknown>
    [k: string]: unknown
  }
  agents?: Record<string, FleetAgentDef>
  communication?: {
    flow?: CommFlowNode[]
  }
  artifacts?: Record<string, FleetArtifactDef>
  settings?: { max_turns_per_agent?: number; [k: string]: unknown }
  workspace_base_dir?: string
  project_context?: unknown
}

export interface CommFlowNode {
  role: string
  entry_point?: boolean
  talks_to?: string[]
}

export interface FleetAgentDef {
  name?: string
  mode?: string
  description?: string
  delegate?: { tool: string; [k: string]: unknown }
  behaviors?: string
  identity?: string
  [k: string]: unknown
}

export interface FleetArtifactDef {
  type?: string
  repo?: string
  path?: string
  auto_pr?: boolean
  [k: string]: unknown
}

export interface SidebarItem {
  key: string
  name: string
  subtitle?: string
  activated?: boolean
  badge?: string | null
  onDelete?: () => void
}

export interface SelectedItem {
  type: string
  key: string
}

/** Trace event – the events array inside FleetTrace is unknown[]. */
export interface TraceEvent {
  type?: string
  timestamp?: string
  session?: string
  text?: string
  tool_name?: string
  args?: unknown
  result?: unknown
  error?: string
  duration_ms?: number
  [k: string]: unknown
}

export interface AgentColor {
  bg: string
  border: string
  text: string
  label: string
}

// Agent identity colors for trace view
export const AGENT_COLORS: Record<string, AgentColor> = {
  po: { bg: 'rgba(59, 130, 246, 0.1)', border: 'rgba(59, 130, 246, 0.3)', text: '#60a5fa', label: 'PO' },
  architect: { bg: 'rgba(168, 85, 247, 0.1)', border: 'rgba(168, 85, 247, 0.3)', text: '#c084fc', label: 'Architect' },
  ux: { bg: 'rgba(236, 72, 153, 0.1)', border: 'rgba(236, 72, 153, 0.3)', text: '#f472b6', label: 'UX' },
  dev: { bg: 'rgba(34, 197, 94, 0.1)', border: 'rgba(34, 197, 94, 0.3)', text: '#4ade80', label: 'Dev' },
  qa: { bg: 'rgba(234, 179, 8, 0.1)', border: 'rgba(234, 179, 8, 0.3)', text: '#facc15', label: 'QA' },
  system: { bg: 'rgba(107, 114, 128, 0.1)', border: 'rgba(107, 114, 128, 0.3)', text: '#9ca3af', label: 'System' },
}

export function getAgentColor(name: string): AgentColor {
  return AGENT_COLORS[name] || { bg: 'rgba(6, 182, 212, 0.1)', border: 'rgba(6, 182, 212, 0.3)', text: '#22d3ee', label: name }
}

// Extract the agent role from a sub-agent session label.
// e.g., "fleet-juicytrade-po" -> "po", "fleet-myplan-architect" -> "architect"
export function extractAgentRole(sessionLabel: string): string | null {
  if (!sessionLabel) return null
  const parts = sessionLabel.split('-')
  const last = parts[parts.length - 1]
  // Check if the last segment is a known agent role
  if (AGENT_COLORS[last]) return last
  // Try last two segments for compound names (e.g., "qa-engineer" if ever used)
  if (parts.length >= 2) {
    const lastTwo = parts.slice(-2).join('-')
    if (AGENT_COLORS[lastTwo]) return lastTwo
  }
  return last
}

export function formatTimeAgo(dateStr: string): string {
  if (!dateStr) return ''
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return 'just now'
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays}d ago`
}
