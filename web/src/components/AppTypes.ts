// --- Local Types for App.tsx ---

export interface FlowNodeData {
  label?: string
  nodeType?: string
  yaml?: Record<string, any>
  [key: string]: any
}

export interface FlowNode {
  id: string
  type: string
  position: { x: number; y: number }
  data: FlowNodeData
}

export interface FlowEdge {
  id: string
  source: string
  target: string
  animated: boolean
  style: Record<string, unknown>
  type: string
  label?: string
  labelStyle?: Record<string, unknown>
  labelBgStyle?: Record<string, unknown>
  labelBgPadding?: number[]
  data?: Record<string, unknown>
}

export interface ToastData {
  message: string
  type: 'success' | 'error' | 'info'
  action?: { label: string; onClick: () => void }
  persistent?: boolean
}

export interface UpgradeDialogData {
  version: string
  url: string
}

export interface UpdateInfo {
  version: string
  url: string
}

export interface ChatMessage {
  type: string
  content?: string
  preserveWhitespace?: boolean
  nodeName?: string
  options?: any
  attempt?: any
  maxRetries?: any
  reason?: any
  title?: string
  suggestion?: string
  originalError?: any
  toolName?: string
}

export interface FocusedNodeData {
  name: string
  type: string
  data: Record<string, any>
}

export interface VariableGroup {
  nodeName: string
  nodeType?: string
  variables: string[]
}

export interface InstallModalServerData {
  name: string
  description: string
  config?: Record<string, any>
  _depContext?: any
  _isInline?: boolean
  [key: string]: any
}

// Extended Agent type used within App (includes extra fields for newly-created agents)
import type { Agent } from '../api/agents'

export interface AppAgent extends Agent {
  isNew?: boolean
  tapName?: string
}

// YamlData parsed from yaml.load
export type YamlData = Record<string, any>

// Default YAML for new agents
export const defaultYaml = `description: New Agent

nodes: []

flow: []
`
