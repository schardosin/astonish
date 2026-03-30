import { useState, useEffect, useCallback } from 'react'

// --- Types ---

export interface RouterPath {
  view: string
  params: Record<string, string>
}

export interface HashRouter {
  path: RouterPath
  navigate: (newPath: string) => void
  replaceHash: (newPath: string) => void
}

export interface BuildPathParams {
  agentName?: string
  section?: string
  sessionId?: string
  subView?: string
  subKey?: string
  subKey2?: string
}

// --- Hook ---

export function useHashRouter(): HashRouter {
  const [path, setPath] = useState<RouterPath>(() => parseHash(window.location.hash))

  useEffect(() => {
    const handleHashChange = () => {
      setPath(parseHash(window.location.hash))
    }
    
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [])

  const navigate = useCallback((newPath: string) => {
    const hash = newPath.startsWith('#') ? newPath : `#${newPath}`
    window.location.hash = hash
  }, [])

  const replaceHash = useCallback((newPath: string) => {
    const hash = newPath.startsWith('#') ? newPath : `#${newPath}`
    window.history.replaceState(null, '', hash)
    setPath(parseHash(hash))
  }, [])

  return { path, navigate, replaceHash }
}

function parseHash(hash: string): RouterPath {
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
    const subView = parts[1] || ''
    const subKey = parts[2] ? decodeURIComponent(parts[2]) : ''
    return { view: 'fleet', params: { subView, subKey } }
  }

  if (view === 'drill') {
    const subView = parts[1] || ''
    const subKey = parts[2] ? decodeURIComponent(parts[2]) : ''
    const subKey2 = parts[3] ? decodeURIComponent(parts[3]) : ''
    return { view: 'drill', params: { subView, subKey, subKey2 } }
  }

  if (view === 'home') {
    return { view: 'chat', params: {} }
  }

  return { view, params: {} }
}

export function buildPath(view: string, params: BuildPathParams = {}): string {
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
