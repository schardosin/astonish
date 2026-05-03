/**
 * Team context for platform-mode API calls.
 *
 * Provides a module-level active team slug and a fetch wrapper that
 * injects the X-Astonish-Team header into every request. The backend's
 * PlatformAuthMiddleware reads this header to override the JWT's default
 * team, enabling per-request team scoping without re-issuing tokens.
 *
 * Usage:
 *   import { setActiveTeam, teamFetch } from './teamContext'
 *
 *   // On team switch:
 *   setActiveTeam('cronus')
 *
 *   // In API calls:
 *   const res = await teamFetch('/api/memories/team')
 */

let _activeTeam: string | null = null

const STORAGE_KEY = 'astonish_active_team'

// Restore from localStorage on module load
try {
  _activeTeam = localStorage.getItem(STORAGE_KEY)
} catch { /* SSR or private browsing */ }

/** Update the active team slug. Called from App.tsx when the user switches teams. */
export function setActiveTeam(slug: string | null) {
  _activeTeam = slug
  try {
    if (slug) {
      localStorage.setItem(STORAGE_KEY, slug)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  } catch { /* ignore */ }
}

/** Get the current active team slug. */
export function getActiveTeam(): string | null {
  return _activeTeam
}

/**
 * Fetch wrapper that injects X-Astonish-Team header when an active team
 * is set. Signature matches the standard fetch() API so it's a drop-in
 * replacement. Falls through to plain fetch() when no team is active
 * (personal mode).
 */
export function teamFetch(input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]): Promise<Response> {
  if (!_activeTeam) {
    return fetch(input, init)
  }

  const headers = new Headers(init?.headers)
  // Only set if not already explicitly provided by the caller
  if (!headers.has('X-Astonish-Team')) {
    headers.set('X-Astonish-Team', _activeTeam)
  }

  return fetch(input, { ...init, headers })
}
