// ---- Local message types used in the chat UI ----

export interface FleetMessageItem {
  type: 'fleet_message'
  id?: string
  sender: string
  text: string
  mentions?: string[]
  timestamp: number
  metadata?: Record<string, unknown>
  [key: string]: unknown
}

export interface UserMessage {
  type: 'user'
  content: string
}

export interface AgentMessage {
  type: 'agent'
  content: string
  _streaming?: boolean
}

export interface ToolCallMessage {
  type: 'tool_call'
  toolName: unknown
  toolArgs: unknown
}

export interface ToolResultMessage {
  type: 'tool_result'
  toolName: unknown
  toolResult: unknown
}

export interface ImageMessage {
  type: 'image'
  data: unknown
  mimeType: unknown
}

export interface ErrorMessage {
  type: 'error'
  content: string
}

export interface ErrorInfoMessage {
  type: 'error_info'
  title: unknown
  reason: unknown
  suggestion: unknown
  originalError: unknown
}

export interface ApprovalMessage {
  type: 'approval'
  toolName: unknown
  options: unknown
}

export interface AutoApprovedMessage {
  type: 'auto_approved'
  toolName: unknown
}

export interface ThinkingMessage {
  type: 'thinking'
  content: unknown
}

export interface SystemMessage {
  type: 'system'
  content: string
}

export interface RetryMessage {
  type: 'retry'
  attempt: unknown
  maxRetries: unknown
  reason: unknown
}

export interface FleetExecutionMessage {
  type: 'fleet_execution'
  events: FleetEvent[]
  currentPhase: string | null
  currentAgent: string | null
  status: string
}

export interface BrowserHandoffMessage {
  type: 'browser_handoff'
  vncProxyUrl: string
  pageUrl: string
  pageTitle: string
  reason: string
}

export interface FleetEvent {
  type: string
  phase?: string
  agent?: string
  detail?: string
  text?: string
  message?: string
  args?: unknown
  result?: unknown
  timestamp?: number
  [key: string]: unknown
}

export type ChatMsg =
  | FleetMessageItem
  | UserMessage
  | AgentMessage
  | ToolCallMessage
  | ToolResultMessage
  | ImageMessage
  | ErrorMessage
  | ErrorInfoMessage
  | ApprovalMessage
  | AutoApprovedMessage
  | ThinkingMessage
  | SystemMessage
  | RetryMessage
  | FleetExecutionMessage
  | BrowserHandoffMessage

// ---- Fleet info / state ----

export interface FleetInfo {
  fleet_key: string
  fleet_name: string
  agents?: unknown
}

export interface FleetStateInfo {
  state: string
  active_agent: string
}

// ---- Deferred prompt types ----

export interface DeferredPrompt {
  message: string
  systemContext: string
}

// Agent identity colors for the team conversation view
export const AGENT_COLORS: Record<string, { bg: string; border: string; text: string; label: string }> = {
  po: { bg: 'rgba(59, 130, 246, 0.1)', border: 'rgba(59, 130, 246, 0.3)', text: '#60a5fa', label: 'PO' },
  architect: { bg: 'rgba(168, 85, 247, 0.1)', border: 'rgba(168, 85, 247, 0.3)', text: '#c084fc', label: 'Architect' },
  ux: { bg: 'rgba(236, 72, 153, 0.1)', border: 'rgba(236, 72, 153, 0.3)', text: '#f472b6', label: 'UX' },
  dev: { bg: 'rgba(34, 197, 94, 0.1)', border: 'rgba(34, 197, 94, 0.3)', text: '#4ade80', label: 'Dev' },
  qa: { bg: 'rgba(234, 179, 8, 0.1)', border: 'rgba(234, 179, 8, 0.3)', text: '#facc15', label: 'QA' },
  system: { bg: 'rgba(107, 114, 128, 0.1)', border: 'rgba(107, 114, 128, 0.3)', text: '#9ca3af', label: 'System' },
}

export function getAgentColor(sender: string) {
  return AGENT_COLORS[sender] || { bg: 'rgba(6, 182, 212, 0.1)', border: 'rgba(6, 182, 212, 0.3)', text: '#22d3ee', label: sender }
}
