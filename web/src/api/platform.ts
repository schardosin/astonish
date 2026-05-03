/**
 * API client for platform-mode endpoints.
 * Covers teams, app sharing, memories, and audit queries.
 * All endpoints require authentication (cookie-based JWT).
 *
 * Team-scoped requests use teamFetch() which injects the X-Astonish-Team
 * header so the backend resolves the correct team context.
 */

import { teamFetch } from './teamContext'

// --------------------------------------------------------------------------
// Teams
// --------------------------------------------------------------------------

export interface Team {
  slug: string
  name: string
  description: string
  created_at: string
}

export interface TeamMember {
  user_id: string
  email: string
  display_name: string
  role: string
  joined_at: string
}

export interface OrgInfo {
  id: string
  name: string
  slug: string
}

export async function fetchTeams(): Promise<Team[]> {
  const res = await teamFetch('/api/teams')
  if (!res.ok) throw new Error('Failed to fetch teams')
  const data = await res.json()
  return data.teams || []
}

export async function createTeam(name: string, slug: string, description?: string): Promise<Team> {
  const res = await teamFetch('/api/teams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, slug, description: description || '' }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to create team')
  }
  return res.json()
}

export async function deleteTeam(slug: string): Promise<void> {
  const res = await teamFetch(`/api/teams/${slug}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to delete team')
  }
}

export async function fetchTeamMembers(slug: string): Promise<TeamMember[]> {
  const res = await teamFetch(`/api/teams/${slug}/members`)
  if (!res.ok) throw new Error('Failed to fetch team members')
  const data = await res.json()
  return data.members || []
}

export async function addTeamMember(slug: string, email: string, role?: string): Promise<void> {
  const res = await teamFetch(`/api/teams/${slug}/members`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, role: role || 'member' }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to add team member')
  }
}

export async function removeTeamMember(slug: string, userID: string): Promise<void> {
  const res = await teamFetch(`/api/teams/${slug}/members/${userID}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to remove team member')
  }
}

export async function setTeamMemberRole(slug: string, userID: string, role: string): Promise<void> {
  const res = await teamFetch(`/api/teams/${slug}/members/${userID}/role`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ role }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to update member role')
  }
}

export async function fetchOrg(): Promise<OrgInfo> {
  const res = await teamFetch('/api/org')
  if (!res.ok) throw new Error('Failed to fetch org info')
  return res.json()
}

// --------------------------------------------------------------------------
// App Sharing
// --------------------------------------------------------------------------

export interface AppItem {
  name: string
  description: string
  version: number
  updatedAt: string
}

export async function publishAppToTeam(slug: string): Promise<{ slug: string }> {
  const res = await teamFetch('/api/apps/publish', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ slug }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to publish app')
  }
  return res.json()
}

export async function forkApp(slug: string, source: 'team' | 'org'): Promise<{ slug: string }> {
  const res = await teamFetch('/api/apps/fork', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ slug, source }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to fork app')
  }
  return res.json()
}

export async function promoteAppToOrg(slug: string, teamSlug: string): Promise<{ slug: string }> {
  const res = await teamFetch('/api/apps/promote', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ slug, team_slug: teamSlug }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to promote app')
  }
  return res.json()
}

export async function fetchOrgApps(): Promise<AppItem[]> {
  const res = await teamFetch('/api/apps/org')
  if (!res.ok) throw new Error('Failed to fetch org apps')
  const data = await res.json()
  return data.apps || []
}

export async function deleteOrgApp(name: string): Promise<void> {
  const res = await teamFetch(`/api/apps/org/${name}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to delete org app')
  }
}

// --------------------------------------------------------------------------
// Memories (Knowledge Sharing)
// --------------------------------------------------------------------------

export interface MemoryEntry {
  id: string
  snippet: string
  category: string
  scope: string
  score?: number
  created_at?: string
}

export async function searchMemories(query: string, limit?: number): Promise<MemoryEntry[]> {
  const res = await teamFetch('/api/memories/search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, max_results: limit || 20 }),
  })
  if (!res.ok) throw new Error('Failed to search memories')
  const data = await res.json()
  return data.results || []
}

export async function saveTeamMemory(snippet: string, category?: string): Promise<{ id: string }> {
  const res = await teamFetch('/api/memories/team', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ snippet, category: category || 'general' }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to save team memory')
  }
  return res.json()
}

export async function savePersonalMemory(snippet: string, category?: string): Promise<{ id: string }> {
  const res = await teamFetch('/api/memories/personal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ snippet, category: category || 'general' }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to save personal memory')
  }
  return res.json()
}

export async function listTeamMemories(): Promise<MemoryEntry[]> {
  const res = await teamFetch('/api/memories/team')
  if (!res.ok) throw new Error('Failed to list team memories')
  const data = await res.json()
  return data.results || []
}

export async function listOrgMemories(): Promise<MemoryEntry[]> {
  const res = await teamFetch('/api/memories/org')
  if (!res.ok) throw new Error('Failed to list org memories')
  const data = await res.json()
  return data.results || []
}

export async function deleteTeamMemory(id: string): Promise<void> {
  const res = await teamFetch(`/api/memories/team/${id}`, { method: 'DELETE' })
  if (!res.ok) throw new Error('Failed to delete team memory')
}

export async function deleteOrgMemory(id: string): Promise<void> {
  const res = await teamFetch(`/api/memories/org/${id}`, { method: 'DELETE' })
  if (!res.ok) throw new Error('Failed to delete org memory')
}

export async function promoteMemoryToOrg(id: string): Promise<void> {
  const res = await teamFetch('/api/memories/promote', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to promote memory')
  }
}

// --------------------------------------------------------------------------
// Audit Logs
// --------------------------------------------------------------------------

export interface AuditEntry {
  id: number
  timestamp: string
  user_id: string
  team_id: string
  action: string
  resource: string
  detail: Record<string, unknown>
  ip_address: string
  session_id: string
}

export interface AuditFilter {
  user_id?: string
  action?: string
  resource?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
}

export async function queryAuditLogs(filter: AuditFilter = {}): Promise<{ entries: AuditEntry[]; count: number }> {
  const params = new URLSearchParams()
  if (filter.user_id) params.set('user_id', filter.user_id)
  if (filter.action) params.set('action', filter.action)
  if (filter.resource) params.set('resource', filter.resource)
  if (filter.since) params.set('since', filter.since)
  if (filter.until) params.set('until', filter.until)
  if (filter.limit) params.set('limit', String(filter.limit))
  if (filter.offset) params.set('offset', String(filter.offset))

  const res = await teamFetch(`/api/audit?${params.toString()}`)
  if (!res.ok) throw new Error('Failed to query audit logs')
  return res.json()
}

// --------------------------------------------------------------------------
// Platform Setup (Deployment Mode)
// --------------------------------------------------------------------------

export interface PlatformInitParams {
  host: string
  port: number
  user: string
  password: string
  ssl_mode: string
  org_name: string
  org_slug: string
}

export interface PlatformInitResult {
  success: boolean
  message: string
  restart_required: boolean
  error?: string
}

export interface DeploymentModeInfo {
  mode: 'personal' | 'platform'
  configured: boolean
}

export async function initializePlatform(params: PlatformInitParams): Promise<PlatformInitResult> {
  const res = await fetch('/api/platform/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  const data = await res.json()
  if (!res.ok) {
    throw new Error(data.error || 'Failed to initialize platform')
  }
  return data
}

export async function getDeploymentMode(): Promise<DeploymentModeInfo> {
  const res = await fetch('/api/platform/mode')
  if (!res.ok) {
    return { mode: 'personal', configured: false }
  }
  return res.json()
}

export async function getPlatformInitStatus(): Promise<{ configured: boolean; initialized: boolean }> {
  const res = await fetch('/api/platform/init/status')
  if (!res.ok) {
    return { configured: false, initialized: false }
  }
  return res.json()
}
