import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import MCPStoreModal from '../MCPStoreModal'

/**
 * Regression tests for MCPStoreModal scope-passing.
 *
 * Bug: Installing a server from the "Browse MCP" store when viewing
 * platform-scoped settings would send the install request without
 * ?scope=platform. The server would be saved to the org-level store,
 * but the subsequent reload read from the platform store — making
 * the installed server invisible.
 *
 * Fix: MCPStoreModal now accepts a `scope` prop and passes it to the
 * install endpoint as a query parameter.
 */

const mockServers = [
  {
    mcpId: '@upstash/context7-mcp',
    name: 'Context7',
    author: 'upstash',
    description: 'Resolves library documentation',
    githubStars: 500,
    githubUrl: 'https://github.com/upstash/context7-mcp',
    tags: ['documentation'],
    config: {
      command: 'npx',
      env: {}
    }
  }
]

describe('MCPStoreModal scope-passing', () => {
  let fetchCalls: { url: string; method: string; body?: string }[] = []
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    fetchCalls = []
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : (input instanceof URL ? input.toString() : (input as Request).url)
      const method = init?.method?.toUpperCase() || 'GET'
      const body = init?.body as string | undefined
      fetchCalls.push({ url, method, body })

      // MCP store list
      if (url.includes('/api/mcp-store') && !url.includes('/install') && method === 'GET') {
        return new Response(JSON.stringify({ servers: mockServers, sources: ['all'] }), {
          status: 200, headers: { 'Content-Type': 'application/json' }
        })
      }

      // MCP store install
      if (url.includes('/install') && method === 'POST') {
        return new Response(JSON.stringify({ status: 'ok', serverName: 'context7' }), {
          status: 200, headers: { 'Content-Type': 'application/json' }
        })
      }

      return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }) as typeof fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  async function selectServerAndInstall(user: ReturnType<typeof userEvent.setup>) {
    // Wait for server list to load
    await waitFor(() => {
      expect(screen.getByText('Context7')).toBeInTheDocument()
    })

    // Click on the server card to expand it (reveals the Install button)
    await user.click(screen.getByText('Context7'))

    // Now click the Install button (rendered inside the expanded details)
    const installButton = await screen.findByText('Install')
    await user.click(installButton)
  }

  it('install sends ?scope=platform when scope prop is "platform"', async () => {
    const user = userEvent.setup()
    const onInstall = vi.fn()

    render(
      <MCPStoreModal
        isOpen={true}
        onClose={() => {}}
        onInstall={onInstall}
        scope="platform"
      />
    )

    await selectServerAndInstall(user)

    await waitFor(() => {
      expect(onInstall).toHaveBeenCalled()
    })

    const installCall = fetchCalls.find(c => c.url.includes('/install') && c.method === 'POST')
    expect(installCall).toBeDefined()
    expect(installCall!.url).toContain('scope=platform')
  })

  it('install sends ?scope=team when scope is "team"', async () => {
    const user = userEvent.setup()
    const onInstall = vi.fn()

    render(
      <MCPStoreModal
        isOpen={true}
        onClose={() => {}}
        onInstall={onInstall}
        teamSlug="engineering"
        scope="team"
      />
    )

    await selectServerAndInstall(user)

    await waitFor(() => {
      expect(onInstall).toHaveBeenCalled()
    })

    const installCall = fetchCalls.find(c => c.url.includes('/install') && c.method === 'POST')
    expect(installCall).toBeDefined()
    expect(installCall!.url).toContain('scope=team')
  })

  it('install sends no scope when neither scope nor teamSlug is set', async () => {
    const user = userEvent.setup()
    const onInstall = vi.fn()

    render(
      <MCPStoreModal
        isOpen={true}
        onClose={() => {}}
        onInstall={onInstall}
      />
    )

    await selectServerAndInstall(user)

    await waitFor(() => {
      expect(onInstall).toHaveBeenCalled()
    })

    const installCall = fetchCalls.find(c => c.url.includes('/install') && c.method === 'POST')
    expect(installCall).toBeDefined()
    expect(installCall!.url).not.toContain('scope=')
  })
})
