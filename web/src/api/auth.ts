/**
 * API client for platform authentication.
 * Used in multi-tenant (postgres) mode only.
 */

const AUTH_BASE = '/api/auth'

// --- Types ---

export interface AuthUser {
  id: string
  email: string
  display_name: string
  role: string
  platform_role?: string
}

export interface AuthOrg {
  id: string
  name: string
  slug: string
}

export interface AuthResponse {
  user: AuthUser
  org: AuthOrg
  expires_in: number
}

export interface SetupStatus {
  initialized: boolean
  allow_registration: boolean
  auth_mode: string
}

export interface MeResponse {
  user: AuthUser
  org: AuthOrg
  team: string
}

// --- API functions ---

export async function getSetupStatus(): Promise<SetupStatus> {
  const res = await fetch(`${AUTH_BASE}/setup-status`)
  if (!res.ok) throw new Error('Failed to check setup status')
  return res.json()
}

export async function register(
  email: string,
  password: string,
  displayName: string
): Promise<AuthResponse> {
  const res = await fetch(`${AUTH_BASE}/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, display_name: displayName }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Registration failed')
  }
  return res.json()
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  const res = await fetch(`${AUTH_BASE}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Login failed')
  }
  return res.json()
}

export async function refreshToken(): Promise<AuthResponse> {
  const res = await fetch(`${AUTH_BASE}/refresh`, {
    method: 'POST',
  })
  if (!res.ok) throw new Error('Token refresh failed')
  return res.json()
}

export async function logout(): Promise<void> {
  await fetch(`${AUTH_BASE}/logout`, { method: 'POST' })
}

export async function getMe(): Promise<MeResponse> {
  const res = await fetch(`${AUTH_BASE}/me`)
  if (!res.ok) throw new Error('Not authenticated')
  return res.json()
}

// --- Organization switching ---

export interface UserOrg {
  id: string
  name: string
  slug: string
  role: string
}

export interface UserOrgsResponse {
  orgs: UserOrg[]
  active_org: string
}

export async function getUserOrgs(): Promise<UserOrgsResponse> {
  const res = await fetch('/api/orgs')
  if (!res.ok) throw new Error('Failed to fetch organizations')
  return res.json()
}

export async function switchOrg(orgSlug: string): Promise<AuthResponse> {
  const res = await fetch('/api/orgs/switch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ org_slug: orgSlug }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Failed to switch organization')
  }
  return res.json()
}

/**
 * Check if the current session is authenticated.
 * Tries /me first (fast, uses access token cookie).
 * If that fails with 401, tries refresh.
 * Returns the user info if authenticated, null otherwise.
 *
 * Deduplicates concurrent calls: rapid page refreshes or React Strict Mode
 * double-effects share a single in-flight request instead of each firing
 * their own /me + /refresh sequence (which can exhaust the auth rate limit).
 */
let _checkAuthInflight: Promise<MeResponse | null> | null = null

export function checkAuth(): Promise<MeResponse | null> {
  if (_checkAuthInflight) return _checkAuthInflight
  _checkAuthInflight = _doCheckAuth().finally(() => { _checkAuthInflight = null })
  return _checkAuthInflight
}

async function _doCheckAuth(): Promise<MeResponse | null> {
  try {
    return await getMe()
  } catch {
    // Access token expired or missing, try refresh
    try {
      await refreshToken()
      return await getMe()
    } catch {
      return null
    }
  }
}
