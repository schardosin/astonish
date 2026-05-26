import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import MCPInspector from '../MCPInspector'

/**
 * Regression tests for MCPInspector scope-passing.
 *
 * Bug: The "Test" button on MCP servers installed at platform scope
 * sent requests without ?scope=platform, causing the backend to look
 * in the org-level store where the server doesn't exist → "not found in config".
 *
 * Fix: MCPInspector now accepts a `scope` prop and passes it to all API calls.
 */

describe('MCPInspector scope-passing', () => {
  let fetchCalls: { url: string; method: string }[] = []
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    fetchCalls = []
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : (input instanceof URL ? input.toString() : (input as Request).url)
      const method = init?.method?.toUpperCase() || 'GET'
      fetchCalls.push({ url, method })

      // Return a successful tools response
      if (url.includes('/tools') && method === 'GET') {
        return new Response(JSON.stringify({
          tools: [{ name: 'resolve', description: 'Resolves a library ID' }]
        }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }

      // Run tool response
      if (url.includes('/run') && method === 'POST') {
        return new Response(JSON.stringify({
          success: true, result: { content: 'test' }, time_taken: '100ms'
        }), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }

      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }) as typeof fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('sends ?scope=platform when scope prop is "platform"', async () => {
    render(
      <MCPInspector
        serverName="context7"
        scope="platform"
        onClose={() => {}}
      />
    )

    await waitFor(() => {
      expect(fetchCalls.length).toBeGreaterThan(0)
    })

    const toolsFetch = fetchCalls.find(c => c.url.includes('/tools'))
    expect(toolsFetch).toBeDefined()
    expect(toolsFetch!.url).toContain('?scope=platform')
  })

  it('sends ?scope=team when scope prop is "team"', async () => {
    render(
      <MCPInspector
        serverName="my-server"
        teamSlug="engineering"
        scope="team"
        onClose={() => {}}
      />
    )

    await waitFor(() => {
      expect(fetchCalls.length).toBeGreaterThan(0)
    })

    const toolsFetch = fetchCalls.find(c => c.url.includes('/tools'))
    expect(toolsFetch).toBeDefined()
    expect(toolsFetch!.url).toContain('?scope=team')
  })

  it('sends ?scope=team when teamSlug is set but no explicit scope', async () => {
    render(
      <MCPInspector
        serverName="team-server"
        teamSlug="engineering"
        onClose={() => {}}
      />
    )

    await waitFor(() => {
      expect(fetchCalls.length).toBeGreaterThan(0)
    })

    const toolsFetch = fetchCalls.find(c => c.url.includes('/tools'))
    expect(toolsFetch).toBeDefined()
    expect(toolsFetch!.url).toContain('?scope=team')
  })

  it('sends no scope when neither scope nor teamSlug is set', async () => {
    render(
      <MCPInspector
        serverName="org-server"
        onClose={() => {}}
      />
    )

    await waitFor(() => {
      expect(fetchCalls.length).toBeGreaterThan(0)
    })

    const toolsFetch = fetchCalls.find(c => c.url.includes('/tools'))
    expect(toolsFetch).toBeDefined()
    expect(toolsFetch!.url).not.toContain('?scope=')
    expect(toolsFetch!.url).not.toContain('scope=')
  })

  it('displays error when fetch returns error in body', async () => {
    globalThis.fetch = vi.fn(async () => {
      return new Response(JSON.stringify({ error: "server 'bad' not found in config" }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    }) as typeof fetch

    render(
      <MCPInspector
        serverName="bad"
        scope="platform"
        onClose={() => {}}
      />
    )

    await waitFor(() => {
      expect(screen.getByText(/not found in config/)).toBeInTheDocument()
    })
  })
})
