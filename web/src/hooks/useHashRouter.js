import { useState, useEffect, useCallback } from 'react'

/**
 * Custom hook for hash-based routing
 * Supports paths like:
 * - #/agent/my-agent
 * - #/settings/general
 * - #/settings/providers
 * - #/settings/mcp
 */
export function useHashRouter() {
  const [path, setPath] = useState(() => parseHash(window.location.hash))

  useEffect(() => {
    const handleHashChange = () => {
      setPath(parseHash(window.location.hash))
    }
    
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [])

  const navigate = useCallback((newPath) => {
    const hash = newPath.startsWith('#') ? newPath : `#${newPath}`
    window.location.hash = hash
  }, [])

  const replaceHash = useCallback((newPath) => {
    const hash = newPath.startsWith('#') ? newPath : `#${newPath}`
    // Replace without triggering hashchange
    window.history.replaceState(null, '', hash)
    setPath(parseHash(hash))
  }, [])

  return { path, navigate, replaceHash }
}

/**
 * Parse hash into structured path object
 */
function parseHash(hash) {
  const cleanHash = hash.replace(/^#\/?/, '')
  const parts = cleanHash.split('/').filter(Boolean)
  
  if (parts.length === 0) {
    return { view: 'home', params: {} }
  }

  const view = parts[0]

  if (view === 'agent' && parts[1]) {
    return { view: 'agent', params: { agentName: decodeURIComponent(parts[1]) } }
  }

  if (view === 'settings') {
    return { 
      view: 'settings', 
      params: { section: parts[1] || 'general' }
    }
  }

  return { view, params: {} }
}

/**
 * Build a hash path from components
 */
export function buildPath(view, params = {}) {
  switch (view) {
    case 'agent':
      return `/agent/${encodeURIComponent(params.agentName || '')}`
    case 'settings':
      return `/settings/${params.section || 'general'}`
    default:
      return '/'
  }
}
