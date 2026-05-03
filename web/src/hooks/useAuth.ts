import { useState, useEffect, useCallback } from 'react'
import { checkAuth, login as apiLogin, register as apiRegister, logout as apiLogout, refreshToken, type AuthUser, type AuthOrg } from '../api/auth'

export interface AuthState {
  isAuthenticated: boolean
  isLoading: boolean
  user: AuthUser | null
  org: AuthOrg | null
  team: string | null
}

export function useAuth(isPlatformMode: boolean) {
  const [state, setState] = useState<AuthState>({
    isAuthenticated: !isPlatformMode, // Personal mode is always authenticated
    isLoading: isPlatformMode, // Only loading if we need to check
    user: null,
    org: null,
    team: null,
  })

  // Check auth on mount (only in platform mode)
  useEffect(() => {
    if (!isPlatformMode) return

    // Reset stale personal-mode state before checking auth.
    // useState's initializer only runs once, so if isPlatformMode transitions
    // from false→true, we'd have stale isAuthenticated=true with no user data.
    setState({ isAuthenticated: false, isLoading: true, user: null, org: null, team: null })

    let cancelled = false
    checkAuth().then(result => {
      if (cancelled) return
      if (result) {
        setState({
          isAuthenticated: true,
          isLoading: false,
          user: result.user,
          org: result.org,
          team: result.team,
        })
      } else {
        setState({ isAuthenticated: false, isLoading: false, user: null, org: null, team: null })
      }
    })
    return () => { cancelled = true }
  }, [isPlatformMode])

  // Set up periodic token refresh (every 12 minutes for 15-min tokens)
  useEffect(() => {
    if (!isPlatformMode || !state.isAuthenticated) return

    const interval = setInterval(async () => {
      try {
        await refreshToken()
      } catch {
        // Refresh failed — user needs to re-login
        setState(prev => ({ ...prev, isAuthenticated: false, user: null, org: null, team: null }))
      }
    }, 12 * 60 * 1000) // 12 minutes

    return () => clearInterval(interval)
  }, [isPlatformMode, state.isAuthenticated])

  const login = useCallback(async (email: string, password: string) => {
    const result = await apiLogin(email, password)
    setState({
      isAuthenticated: true,
      isLoading: false,
      user: result.user,
      org: result.org,
      team: null,
    })
    return result
  }, [])

  const register = useCallback(async (email: string, password: string, displayName: string) => {
    const result = await apiRegister(email, password, displayName)
    setState({
      isAuthenticated: true,
      isLoading: false,
      user: result.user,
      org: result.org,
      team: null,
    })
    return result
  }, [])

  const logout = useCallback(async () => {
    await apiLogout()
    setState({
      isAuthenticated: false,
      isLoading: false,
      user: null,
      org: null,
      team: null,
    })
  }, [])

  // Re-check auth state (e.g. after migration sets cookies)
  const refresh = useCallback(async () => {
    setState(prev => ({ ...prev, isLoading: true }))
    const result = await checkAuth()
    if (result) {
      setState({
        isAuthenticated: true,
        isLoading: false,
        user: result.user,
        org: result.org,
        team: result.team,
      })
    } else {
      setState(prev => ({ ...prev, isLoading: false, isAuthenticated: false }))
    }
  }, [])

  return {
    ...state,
    login,
    register,
    logout,
    refresh,
  }
}
