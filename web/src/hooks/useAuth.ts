import { useState, useEffect, useCallback } from 'react'
import { checkAuth, login as apiLogin, register as apiRegister, logout as apiLogout, refreshToken, getUserOrgs, switchOrg as apiSwitchOrg, type AuthUser, type AuthOrg, type UserOrg } from '../api/auth'

// --- Org persistence in localStorage ---
const ORG_STORAGE_KEY = 'astonish_active_org'

function getStoredOrg(): string | null {
  try { return localStorage.getItem(ORG_STORAGE_KEY) } catch { return null }
}

function setStoredOrg(slug: string | null) {
  try {
    if (slug) localStorage.setItem(ORG_STORAGE_KEY, slug)
    else localStorage.removeItem(ORG_STORAGE_KEY)
  } catch { /* ignore */ }
}

export interface AuthState {
  isAuthenticated: boolean
  isLoading: boolean
  user: AuthUser | null
  org: AuthOrg | null
  orgs: UserOrg[] | null
  team: string | null
  /** Set when registration or login reveals that email verification is pending */
  pendingVerificationEmail: string | null
  /** Set when login reveals the user has no team membership */
  noTeamMembership: boolean
}

export function useAuth(isPlatformMode: boolean) {
  const [state, setState] = useState<AuthState>({
    isAuthenticated: !isPlatformMode, // Personal mode is always authenticated
    isLoading: isPlatformMode, // Only loading if we need to check
    user: null,
    org: null,
    orgs: null,
    team: null,
    pendingVerificationEmail: null,
    noTeamMembership: false,
  })

  // Check auth on mount (only in platform mode)
  useEffect(() => {
    if (!isPlatformMode) return

    // Reset stale personal-mode state before checking auth.
    // useState's initializer only runs once, so if isPlatformMode transitions
    // from false→true, we'd have stale isAuthenticated=true with no user data.
    setState({ isAuthenticated: false, isLoading: true, user: null, org: null, orgs: null, team: null, pendingVerificationEmail: null, noTeamMembership: false })

    let cancelled = false
    checkAuth().then(async result => {
      if (cancelled) return
      if (result) {
        // Fetch user's orgs list
        let orgs: UserOrg[] | null = null
        try {
          const orgsResp = await getUserOrgs()
          orgs = orgsResp.orgs
        } catch { /* non-fatal */ }
        if (cancelled) return

        // Keep org localStorage in sync with JWT context
        if (result.org?.slug) setStoredOrg(result.org.slug)

        setState({
          isAuthenticated: true,
          isLoading: false,
          user: result.user,
          org: result.org,
          orgs,
          team: result.team,
          pendingVerificationEmail: null,
          noTeamMembership: false,
        })
      } else {
        setState({ isAuthenticated: false, isLoading: false, user: null, org: null, orgs: null, team: null, pendingVerificationEmail: null, noTeamMembership: false })
      }
    })
    return () => { cancelled = true }
  }, [isPlatformMode])

  // Clear local auth state without calling logout (e.g. refresh cookie gone).
  const markSessionExpired = useCallback(() => {
    setState(prev => ({
      ...prev,
      isAuthenticated: false,
      user: null,
      org: null,
      orgs: null,
      team: null,
      pendingVerificationEmail: null,
      noTeamMembership: false,
    }))
  }, [])

  // Set up periodic token refresh (every 12 minutes for 15-min tokens)
  useEffect(() => {
    if (!isPlatformMode || !state.isAuthenticated) return

    const interval = setInterval(async () => {
      try {
        await refreshToken()
      } catch {
        // Refresh failed — user needs to re-login
        markSessionExpired()
      }
    }, 12 * 60 * 1000) // 12 minutes

    return () => clearInterval(interval)
  }, [isPlatformMode, state.isAuthenticated, markSessionExpired])

  // Refresh when a backgrounded tab becomes visible again. Browsers often
  // throttle setInterval, so the 12-minute timer may miss a 15-minute expiry.
  useEffect(() => {
    if (!isPlatformMode || !state.isAuthenticated) return

    const onVisibility = () => {
      if (document.visibilityState !== 'visible') return
      refreshToken().catch(() => {
        // Benign: teamFetch's 401→refresh path is the hard fallback.
      })
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [isPlatformMode, state.isAuthenticated])

  const login = useCallback(async (email: string, password: string) => {
    const result = await apiLogin(email, password)

    // Handle pending verification state
    if (result.error === 'email_not_verified' || result.requires_verification) {
      setState(prev => ({
        ...prev,
        pendingVerificationEmail: result.email || email,
        noTeamMembership: false,
      }))
      return result
    }

    // Handle no team membership state
    if (result.error === 'no_team_membership') {
      setState(prev => ({
        ...prev,
        noTeamMembership: true,
        pendingVerificationEmail: null,
      }))
      return result
    }

    // Normal login with full auth response
    if (!result.user || !result.org) {
      throw new Error(result.message || 'Login failed')
    }

    // Fetch orgs after successful login
    let orgs: UserOrg[] | null = null
    try {
      const orgsResp = await getUserOrgs()
      orgs = orgsResp.orgs
    } catch { /* non-fatal */ }

    // Check if the user was previously on a different org (stored in localStorage).
    // If so, and they're still a member, switch to that org so the session resumes
    // where they left off.
    const storedOrg = getStoredOrg()
    if (storedOrg && storedOrg !== result.org.slug && orgs?.some(o => o.slug === storedOrg)) {
      try {
        const switchResult = await apiSwitchOrg(storedOrg)
        // Re-fetch orgs to be safe (tokens changed)
        try {
          const orgsResp2 = await getUserOrgs()
          orgs = orgsResp2.orgs
        } catch { /* non-fatal */ }
        setState({
          isAuthenticated: true,
          isLoading: false,
          user: switchResult.user,
          org: switchResult.org,
          orgs,
          team: null,
          pendingVerificationEmail: null,
          noTeamMembership: false,
        })
        return switchResult
      } catch {
        // Switch failed — fall through to use default org from login
      }
    }

    // Persist the org from login
    setStoredOrg(result.org.slug)

    setState({
      isAuthenticated: true,
      isLoading: false,
      user: result.user,
      org: result.org,
      orgs,
      team: null,
      pendingVerificationEmail: null,
      noTeamMembership: false,
    })
    return result
  }, [])

  const register = useCallback(async (email: string, password: string, displayName: string) => {
    const result = await apiRegister(email, password, displayName)

    // Handle verification required (non-first user with email verification)
    if (result.requires_verification) {
      setState(prev => ({
        ...prev,
        pendingVerificationEmail: result.email || email,
        noTeamMembership: false,
      }))
      return result
    }

    // Handle no-team state (verification disabled, but no auto-assign)
    if (result.no_team) {
      setState(prev => ({
        ...prev,
        noTeamMembership: true,
        pendingVerificationEmail: null,
      }))
      return result
    }

    // First user bootstrap — full auth response
    if (!result.user || !result.org) {
      throw new Error(result.message || 'Registration failed')
    }

    // After registration, user has exactly one org
    let orgs: UserOrg[] | null = null
    try {
      const orgsResp = await getUserOrgs()
      orgs = orgsResp.orgs
    } catch { /* non-fatal */ }

    // Persist org
    setStoredOrg(result.org.slug)

    setState({
      isAuthenticated: true,
      isLoading: false,
      user: result.user,
      org: result.org,
      orgs,
      team: null,
      pendingVerificationEmail: null,
      noTeamMembership: false,
    })
    return result
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    // Clear org persistence on logout
    setStoredOrg(null)
    setState({
      isAuthenticated: false,
      isLoading: false,
      user: null,
      org: null,
      orgs: null,
      team: null,
      pendingVerificationEmail: null,
      noTeamMembership: false,
    })
  }, [])

  // Re-check auth state (e.g. after migration sets cookies)
  const refresh = useCallback(async () => {
    setState(prev => ({ ...prev, isLoading: true }))
    const result = await checkAuth()
    if (result) {
      let orgs: UserOrg[] | null = null
      try {
        const orgsResp = await getUserOrgs()
        orgs = orgsResp.orgs
      } catch { /* non-fatal */ }
      // Keep org persistence in sync
      if (result.org?.slug) setStoredOrg(result.org.slug)
      setState({
        isAuthenticated: true,
        isLoading: false,
        user: result.user,
        org: result.org,
        orgs,
        team: result.team,
        pendingVerificationEmail: null,
        noTeamMembership: false,
      })
    } else {
      setState(prev => ({ ...prev, isLoading: false, isAuthenticated: false }))
    }
  }, [])

  // Switch to a different organization — re-issues tokens on the backend
  const switchOrg = useCallback(async (orgSlug: string) => {
    const result = await apiSwitchOrg(orgSlug)
    // Persist the new org
    setStoredOrg(orgSlug)
    // After switch, refresh orgs list and update state
    let orgs: UserOrg[] | null = null
    try {
      const orgsResp = await getUserOrgs()
      orgs = orgsResp.orgs
    } catch { /* non-fatal */ }
    setState({
      isAuthenticated: true,
      isLoading: false,
      user: result.user,
      org: result.org,
      orgs,
      team: null, // Team resets on org switch — will be resolved by App
      pendingVerificationEmail: null,
      noTeamMembership: false,
    })
    return result
  }, [])

  return {
    ...state,
    login,
    register,
    logout,
    refresh,
    switchOrg,
    markSessionExpired,
  }
}
