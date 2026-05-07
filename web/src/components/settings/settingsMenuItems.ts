import {
  Settings, Key, Server, MessageSquare, Globe, Radio, Database,
  Brain, GitFork, Terminal, Wand2, Clock, Shield, Box,
  GitBranch, Store
} from 'lucide-react'

export interface SettingsMenuItem {
  id: string
  label: string
  icon: any
}

// User preferences — personal to each user
export const PREFERENCE_ITEMS: SettingsMenuItem[] = [
  { id: 'chat', label: 'Chat', icon: MessageSquare },
  { id: 'channels', label: 'Channels', icon: Radio },
]

// Team resources — shared within a team, managed from the Team detail view
// Note: Credentials has its own top-level view (not in settings/resources)
export const RESOURCE_ITEMS: SettingsMenuItem[] = [
  { id: 'skills', label: 'Skills', icon: Wand2 },
  { id: 'scheduler', label: 'Scheduler', icon: Clock },
  { id: 'taps', label: 'Repositories', icon: GitBranch },
  { id: 'flows', label: 'Flow Store', icon: Store },
]

// Administration — system config, admin/owner only
export const ADMIN_ITEMS: SettingsMenuItem[] = [
  { id: 'general', label: 'General', icon: Settings },
  { id: 'providers', label: 'Providers', icon: Key },
  { id: 'memory', label: 'Memory', icon: Brain },
  { id: 'mcp', label: 'MCP Servers', icon: Server },
  { id: 'sessions', label: 'Sessions', icon: Database },
  { id: 'sub_agents', label: 'Sub-Agents', icon: GitFork },
  { id: 'open_code', label: 'OpenCode', icon: Terminal },
  { id: 'browser', label: 'Browser', icon: Globe },
  { id: 'daemon', label: 'Daemon', icon: Shield },
  { id: 'sandbox', label: 'Sandbox', icon: Box },
]

// All items combined (for personal mode settings page)
export const ALL_MENU_ITEMS: SettingsMenuItem[] = [
  ...PREFERENCE_ITEMS,
  ...RESOURCE_ITEMS,
  ...ADMIN_ITEMS,
]

// Section keys that use the full config API
export const FULL_CONFIG_SECTIONS = [
  'chat', 'browser', 'channels', 'sessions', 'memory',
  'sub_agents', 'skills', 'scheduler', 'daemon', 'sandbox',
  'identity', 'open_code'
]
