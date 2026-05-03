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
  migration_available?: boolean
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

/**
 * Check if the current session is authenticated.
 * Tries /me first (fast, uses access token cookie).
 * If that fails with 401, tries refresh.
 * Returns the user info if authenticated, null otherwise.
 */
export async function checkAuth(): Promise<MeResponse | null> {
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

// --- Migration API ---

export interface MigrationStatus {
  migration_available: boolean
  running: boolean
  summary?: MigrationSummary
}

export interface MigrationProgress {
  category: string
  current: number
  total: number
  status: string
  error?: string
}

export interface MigrationSummary {
  success: boolean
  categories: Record<string, number>
  duration: number
  errors?: string[]
}

export async function getMigrationStatus(): Promise<MigrationStatus> {
  const res = await fetch('/api/migration/status')
  if (!res.ok) throw new Error('Failed to check migration status')
  return res.json()
}

export async function startMigration(
  email: string,
  password: string,
  displayName: string
): Promise<{ status: string; user_id: string; org: string; team: string }> {
  const res = await fetch('/api/migration/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, display_name: displayName }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }))
    throw new Error(err.message || err.error || 'Migration failed to start')
  }
  return res.json()
}

export function subscribeMigrationProgress(
  onProgress: (p: MigrationProgress) => void,
  onComplete: (s: MigrationSummary) => void,
  onError: (err: Error) => void
): () => void {
  const eventSource = new EventSource('/api/migration/progress')

  eventSource.addEventListener('progress', (e: MessageEvent) => {
    try {
      onProgress(JSON.parse(e.data))
    } catch {
      // ignore parse errors
    }
  })

  eventSource.addEventListener('complete', (e: MessageEvent) => {
    try {
      onComplete(JSON.parse(e.data))
    } catch {
      // ignore parse errors
    }
    eventSource.close()
  })

  eventSource.onerror = () => {
    onError(new Error('Migration progress connection lost'))
    eventSource.close()
  }

  // Return cleanup function
  return () => eventSource.close()
}
