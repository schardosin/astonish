/**
 * Shared Fetch Mock — intelligent routing for StudioChat API calls.
 *
 * Replaces the per-file mockFetch() helpers that were duplicated across
 * studioChat.test.ts, agents.test.ts, settingsApi.test.ts, and drillApi.test.ts.
 *
 * Usage:
 *   const cleanup = setupMockFetch({ scenarioEvents: [...] })
 *   // ... render and test ...
 *   cleanup()
 */

import { createSSEResponse, type FixtureEvent } from './sseSimulator'

export interface MockFetchConfig {
  /** Events for POST /api/studio/chat — returned as SSE stream */
  scenarioEvents?: FixtureEvent[]
  /** Events for GET /api/studio/sessions/:id/stream — returned as SSE stream */
  reconnectEvents?: FixtureEvent[]
  /** Response for GET /api/studio/sessions */
  sessions?: Array<{
    id: string
    title: string
    createdAt?: string
    updatedAt?: string
    messageCount?: number
  }>
  /** Response for GET /api/studio/sessions/:id */
  sessionHistory?: {
    id: string
    title: string
    messages: unknown[]
  }
  /** Response for GET /api/studio/sessions/:id/status */
  sessionStatus?: { sessionId?: string; running: boolean; eventCount?: number }
  /** Map of artifact path -> content for GET /api/studio/artifacts/content */
  artifactContent?: Record<string, string>
  /** Response for GET /api/studio/fleet/sessions */
  fleetSessions?: unknown[]
  /** Response for GET /api/fleet-plans */
  fleetPlans?: unknown[]
  /** Response for GET /api/fleets */
  fleets?: unknown[]
  /** Additional URL handlers (url pattern -> response data) */
  customHandlers?: Record<string, () => Response | Promise<Response>>
}

/**
 * Create a simple mock Response with JSON body.
 */
export function mockJsonResponse(data: unknown, ok = true, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status: ok ? status : (status >= 400 ? status : 500),
    statusText: ok ? 'OK' : 'Error',
    headers: { 'Content-Type': 'application/json' },
  })
}

/**
 * Create a simple mock Response with text body.
 */
export function mockTextResponse(text: string, ok = true): Response {
  return new Response(text, {
    status: ok ? 200 : 500,
    statusText: ok ? 'OK' : 'Error',
    headers: { 'Content-Type': 'text/plain' },
  })
}

/**
 * Sets up globalThis.fetch with intelligent routing based on URL patterns.
 * Returns a cleanup function that restores the original fetch.
 */
export function setupMockFetch(config: MockFetchConfig = {}): () => void {
  const originalFetch = globalThis.fetch

  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : (input instanceof URL ? input.toString() : input.url)
    const method = init?.method?.toUpperCase() || 'GET'

    // Check custom handlers first
    if (config.customHandlers) {
      for (const [pattern, handler] of Object.entries(config.customHandlers)) {
        if (url.includes(pattern)) {
          return handler()
        }
      }
    }

    // POST /api/studio/chat — SSE stream
    if (method === 'POST' && url.includes('/api/studio/chat')) {
      if (!config.scenarioEvents) {
        return mockJsonResponse({ error: 'No scenario configured' }, false, 500)
      }
      return createSSEResponse(config.scenarioEvents)
    }

    // GET /api/studio/sessions/:id/stream — SSE reconnect stream
    if (method === 'GET' && url.match(/\/api\/studio\/sessions\/[^/]+\/stream/)) {
      const events = config.reconnectEvents || config.scenarioEvents
      if (!events) {
        return mockJsonResponse({ error: 'No reconnect events configured' }, false, 500)
      }
      return createSSEResponse(events)
    }

    // GET /api/studio/sessions/:id/status
    if (method === 'GET' && url.match(/\/api\/studio\/sessions\/[^/]+\/status/)) {
      return mockJsonResponse(config.sessionStatus || { sessionId: '', running: false })
    }

    // GET /api/studio/sessions/:id (session detail / history)
    if (method === 'GET' && url.match(/\/api\/studio\/sessions\/[^/]+$/) && !url.includes('/fleet/')) {
      return mockJsonResponse(config.sessionHistory || { id: '', title: '', messages: [] })
    }

    // GET /api/studio/sessions (list)
    if (method === 'GET' && url.match(/\/api\/studio\/sessions\/?$/) && !url.includes('/fleet/')) {
      return mockJsonResponse(config.sessions || [])
    }

    // DELETE /api/studio/sessions/:id
    if (method === 'DELETE' && url.includes('/api/studio/sessions/')) {
      return mockJsonResponse({ ok: true })
    }

    // POST /api/studio/sessions/:id/stop
    if (method === 'POST' && url.match(/\/api\/studio\/sessions\/[^/]+\/stop/)) {
      return mockJsonResponse({ ok: true })
    }

    // GET /api/studio/artifacts/content
    if (method === 'GET' && url.includes('/api/studio/artifacts/content')) {
      const urlObj = new URL(url, 'http://localhost')
      const path = urlObj.searchParams.get('path') || ''
      const content = config.artifactContent?.[path]
      if (content !== undefined) {
        return mockTextResponse(content)
      }
      return mockTextResponse('File content mock', true)
    }

    // GET /api/studio/artifacts (download) — return empty response
    if (method === 'GET' && url.includes('/api/studio/artifacts') && !url.includes('/content') && !url.includes('/pdf')) {
      return new Response('mock file content', {
        status: 200,
        headers: { 'Content-Type': 'application/octet-stream' },
      })
    }

    // GET /api/studio/fleet/sessions
    if (method === 'GET' && url.includes('/api/studio/fleet/sessions') && !url.match(/sessions\/[^/]+/)) {
      return mockJsonResponse({ sessions: config.fleetSessions || [] })
    }

    // GET /api/fleet-plans
    if (method === 'GET' && url.includes('/api/fleet-plans')) {
      return mockJsonResponse({ plans: config.fleetPlans || [] })
    }

    // GET /api/fleets
    if (method === 'GET' && url.includes('/api/fleets') && !url.includes('/fleet-plans')) {
      return mockJsonResponse({ fleets: config.fleets || [] })
    }

    // POST /api/studio/fleet/start
    if (method === 'POST' && url.includes('/api/studio/fleet/start')) {
      return mockJsonResponse({ session_id: 'fleet-sess-001', fleet_key: 'test', fleet_name: 'Test Fleet', agents: [] })
    }

    // POST /api/studio/fleet/sessions/:id/message
    if (method === 'POST' && url.match(/\/api\/studio\/fleet\/sessions\/[^/]+\/message/)) {
      return mockJsonResponse({ ok: true })
    }

    // POST /api/studio/fleet/sessions/:id/stop
    if (method === 'POST' && url.match(/\/api\/studio\/fleet\/sessions\/[^/]+\/stop/)) {
      return mockJsonResponse({ ok: true })
    }

    // POST /api/browser/handoff-done
    if (method === 'POST' && url.includes('/api/browser/handoff-done')) {
      return mockJsonResponse({ ok: true })
    }

    // Fallback: return 404
    console.warn(`[mockFetch] Unhandled request: ${method} ${url}`)
    return new Response('Not Found', { status: 404, statusText: 'Not Found' })
  }) as typeof fetch

  // Return cleanup function
  return () => {
    globalThis.fetch = originalFetch
  }
}
