import {
  Settings, Key, Server, MessageSquare, Globe, Radio, Database,
  Brain, GitFork, Terminal, Wand2, Clock, Shield, Box,
  GitBranch, Store, Users, BookOpen, UserCog, FileText, Crown, Building2
} from 'lucide-react'

export interface SettingsMenuItem {
  id: string
  label: string
  icon: any
}

// User preferences — personal to each user
export const PREFERENCE_ITEMS: SettingsMenuItem[] = [
  { id: 'channels', label: 'Channels', icon: Radio },
]

// Team resources — shared within a team (platform mode)
export const TEAM_ITEMS: SettingsMenuItem[] = [
  { id: 'team-members', label: 'Members', icon: Users },
  { id: 'team-knowledge', label: 'Knowledge', icon: BookOpen },
  { id: 'team-providers', label: 'Providers', icon: Key },
  { id: 'team-skills', label: 'Skills', icon: Wand2 },
  { id: 'team-mcp', label: 'MCP Servers', icon: Server },
  { id: 'team-scheduler', label: 'Scheduler', icon: Clock },
  { id: 'team-taps', label: 'Repositories', icon: GitBranch },
  { id: 'team-flows', label: 'Flow Store', icon: Store },
  { id: 'team-container', label: 'Container', icon: Box },
]

// Resources — personal mode (no team scoping)
export const RESOURCE_ITEMS: SettingsMenuItem[] = [
  { id: 'skills', label: 'Skills', icon: Wand2 },
  { id: 'scheduler', label: 'Scheduler', icon: Clock },
  { id: 'taps', label: 'Repositories', icon: GitBranch },
  { id: 'flows', label: 'Flow Store', icon: Store },
]

// Organization management — org admin/owner only (platform mode)
export const ORG_ITEMS: SettingsMenuItem[] = [
  { id: 'org-users', label: 'Users', icon: UserCog },
  { id: 'org-providers', label: 'Providers', icon: Key },
  { id: 'org-skills', label: 'Skills', icon: Wand2 },
  { id: 'org-mcp', label: 'MCP Servers', icon: Server },
  { id: 'org-audit', label: 'Audit', icon: FileText },
]

// Platform administration — superadmin only (platform mode)
export const PLATFORM_ITEMS: SettingsMenuItem[] = [
  { id: 'platform-orgs', label: 'Organizations', icon: Building2 },
  { id: 'platform-users', label: 'Users', icon: UserCog },
  { id: 'platform-providers', label: 'Providers', icon: Key },
  { id: 'platform-mcp', label: 'MCP Servers', icon: Server },
  { id: 'platform-channels', label: 'Channels', icon: Radio },
  { id: 'platform-auth', label: 'Authentication', icon: Crown },
]

// Administration — system config, admin/owner only
export const SYSTEM_ITEMS: SettingsMenuItem[] = [
  { id: 'general', label: 'General', icon: Settings },
  { id: 'chat', label: 'Chat', icon: MessageSquare },
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

// Legacy alias — kept for useSettingsData and other references
export const ADMIN_ITEMS = SYSTEM_ITEMS

// All items combined (for personal mode settings page)
export const ALL_MENU_ITEMS: SettingsMenuItem[] = [
  ...PREFERENCE_ITEMS,
  ...RESOURCE_ITEMS,
  ...SYSTEM_ITEMS,
]

// Section keys that use the full config API
export const FULL_CONFIG_SECTIONS = [
  'chat', 'browser', 'channels', 'sessions', 'memory',
  'sub_agents', 'skills', 'scheduler', 'daemon', 'sandbox',
  'identity', 'open_code'
]
