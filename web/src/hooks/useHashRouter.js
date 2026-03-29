import { useState, useEffect, useCallback } from 'react'

/**
 * Custom hook for hash-based routing
 * Supports paths like:
 * - #/chat
 * - #/chat/{sessionId}
 * - #/canvas
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
    return { view: 'chat', params: {} }
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

  if (view === 'chat') {
    return { view: 'chat', params: { sessionId: parts[1] ? decodeURIComponent(parts[1]) : '' } }
  }

  if (view === 'canvas') {
    return { view: 'canvas', params: {} }
  }

  if (view === 'fleet') {
    // #/fleet, #/fleet/plan/{key}, #/fleet/session/{id}, #/fleet/template/{key}
    const subView = parts[1] || ''
    const subKey = parts[2] ? decodeURIComponent(parts[2]) : ''
    return { view: 'fleet', params: { subView, subKey } }
  }

  if (view === 'drill') {
    // #/drill, #/drill/suite/{name}, #/drill/drill/{suite}/{drill}, #/drill/report/{name}
    const subView = parts[1] || ''
    const subKey = parts[2] ? decodeURIComponent(parts[2]) : ''
    const subKey2 = parts[3] ? decodeURIComponent(parts[3]) : ''
    return { view: 'drill', params: { subView, subKey, subKey2 } }
  }

  // Legacy: #/home redirects to chat
  if (view === 'home') {
    return { view: 'chat', params: {} }
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
    case 'chat':
      if (params.sessionId) {
        return `/chat/${encodeURIComponent(params.sessionId)}`
      }
      return '/chat'
    case 'canvas':
      return '/canvas'
    case 'fleet':
      if (params.subView && params.subKey) {
        return `/fleet/${params.subView}/${encodeURIComponent(params.subKey)}`
      }
      return '/fleet'
    case 'drill':
      if (params.subView === 'drill' && params.subKey && params.subKey2) {
        return `/drill/drill/${encodeURIComponent(params.subKey)}/${encodeURIComponent(params.subKey2)}`
      }
      if (params.subView && params.subKey) {
        return `/drill/${params.subView}/${encodeURIComponent(params.subKey)}`
      }
      return '/drill'
    default:
      return '/chat'
  }
}
