/**
 * API client for Platform Administration.
 * Used by the PlatformAdminPanel component (superadmins only).
 */

const ADMIN_BASE = '/api/platform/admin'

// --- Types ---

export interface AdminOrg {
  id: string
  name: string
  slug: string
  status: string
  created_at: string
  member_count: number
  team_count: number
}

export interface AdminOrgDetail {
  organization: {
    id: string
    name: string
    slug: string
    db_name: string
    status: string
    created_at: string
  }
  members: AdminUserWithRole[]
  teams: AdminTeam[]
}

export interface AdminUserWithRole {
  id: string
  email: string
  display_name: string
  role: string
}

export interface AdminTeam {
  id: string
  name: string
  slug: string
  schema_name: string
  created_at: string
}

export interface OrgMembership {
  org_id: string
  org_name?: string
  org_slug?: string
  role: string
}

export interface AdminUser {
  id: string
  email: string
  display_name: string
  platform_role: string
  status: string
  created_at: string
  orgs: OrgMembership[]
}

// --- Organization API ---

export async function listOrgs(): Promise<AdminOrg[]> {
  const res = await fetch(`${ADMIN_BASE}/orgs`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to list organizations')
  }
  const data = await res.json()
  return data.organizations
}

export async function createOrg(params: {
  name: string
  slug?: string
  owner_email?: string
}): Promise<{ organization: AdminOrg; message: string }> {
  const res = await fetch(`${ADMIN_BASE}/orgs`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create organization')
  }
  return res.json()
}

export async function getOrg(slug: string): Promise<AdminOrgDetail> {
  const res = await fetch(`${ADMIN_BASE}/orgs/${encodeURIComponent(slug)}`, {
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Organization not found')
  }
  return res.json()
}

export async function updateOrg(
  slug: string,
  params: { name?: string; status?: string }
): Promise<{ organization: AdminOrg }> {
  const res = await fetch(`${ADMIN_BASE}/orgs/${encodeURIComponent(slug)}`, {
    method: 'PATCH',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update organization')
  }
  return res.json()
}

export async function deleteOrg(slug: string): Promise<void> {
  const res = await fetch(`${ADMIN_BASE}/orgs/${encodeURIComponent(slug)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete organization')
  }
}

// --- User API ---

export async function listUsers(): Promise<AdminUser[]> {
  const res = await fetch(`${ADMIN_BASE}/users`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to list users')
  }
  const data = await res.json()
  return data.users
}

export async function createUser(params: {
  email: string
  display_name: string
  password?: string
}): Promise<{ user: AdminUser; message: string }> {
  const res = await fetch(`${ADMIN_BASE}/users`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to create user')
  }
  return res.json()
}

export async function getUser(id: string): Promise<{ user: AdminUser; orgs: OrgMembership[] }> {
  const res = await fetch(`${ADMIN_BASE}/users/${encodeURIComponent(id)}`, {
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'User not found')
  }
  return res.json()
}

export async function updateUser(
  id: string,
  params: {
    display_name?: string
    status?: string
    platform_role?: string
    password?: string
  }
): Promise<{ user: AdminUser }> {
  const res = await fetch(`${ADMIN_BASE}/users/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to update user')
  }
  return res.json()
}

export async function deleteUser(id: string): Promise<void> {
  const res = await fetch(`${ADMIN_BASE}/users/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete user')
  }
}

export async function addUserToOrg(
  userId: string,
  params: { org_slug: string; role?: string; team_slug?: string }
): Promise<void> {
  const res = await fetch(`${ADMIN_BASE}/users/${encodeURIComponent(userId)}/orgs`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to add user to org')
  }
}

export async function removeUserFromOrg(userId: string, orgSlug: string): Promise<void> {
  const res = await fetch(
    `${ADMIN_BASE}/users/${encodeURIComponent(userId)}/orgs/${encodeURIComponent(orgSlug)}`,
    { method: 'DELETE', credentials: 'include' }
  )
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to remove user from org')
  }
}

// --- Channel Configuration Management ---

export interface ChannelSecretInfo {
  key: string
  label: string
  configured: boolean
}

export interface ChannelFullInfo {
  type: string
  description: string
  enabled: boolean
  config: Record<string, any>
  secrets: ChannelSecretInfo[]
  secrets_configured: boolean
}

export async function listChannels(): Promise<ChannelFullInfo[]> {
  const res = await fetch(`${ADMIN_BASE}/channels`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to list channels')
  }
  return res.json()
}

export async function saveChannel(
  channelType: string,
  payload: { enabled: boolean; config: Record<string, any>; secrets: Record<string, string> }
): Promise<{ message: string }> {
  const res = await fetch(`${ADMIN_BASE}/channels/${encodeURIComponent(channelType)}`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to save channel configuration')
  }
  return res.json()
}

export async function deleteChannel(channelType: string): Promise<{ message: string }> {
  const res = await fetch(`${ADMIN_BASE}/channels/${encodeURIComponent(channelType)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete channel')
  }
  return res.json()
}

// --- Web Services (Standard MCP Servers) ---

export interface WebServiceInfo {
  id: string
  name: string
  description: string
  category: string
  configured: boolean
  secret_key: string
}

export async function listWebServices(): Promise<WebServiceInfo[]> {
  const res = await fetch(`${ADMIN_BASE}/web-services`, { credentials: 'include' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to list web services')
  }
  return res.json()
}

export async function setWebServiceKey(
  id: string,
  apiKey: string
): Promise<{ message: string }> {
  const res = await fetch(`${ADMIN_BASE}/web-services/${encodeURIComponent(id)}`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ api_key: apiKey }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to save web service key')
  }
  return res.json()
}

export async function deleteWebService(id: string): Promise<void> {
  const res = await fetch(`${ADMIN_BASE}/web-services/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'include',
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Failed to delete web service')
  }
}
