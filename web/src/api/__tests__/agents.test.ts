import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fetchAgents, fetchAgent, saveAgent, deleteAgent, fetchTools, checkMcpDependencies, getMcpStoreServer, installMcpServer, installInlineMcpServer, fetchStandardServers, installStandardServer } from '../../api/agents'

// Helper to mock fetch
function mockFetch(data: unknown, ok = true, statusText = 'OK', status = 200) {
  return vi.fn().mockResolvedValue({
    ok,
    status,
    statusText,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(typeof data === 'string' ? data : JSON.stringify(data)),
  })
}

describe('agents API', () => {
  const originalFetch = globalThis.fetch

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  describe('fetchAgents', () => {
    it('calls GET /api/agents and returns data', async () => {
      const expected = { agents: [{ id: '1', name: 'a', description: 'd', source: 's' }] }
      globalThis.fetch = mockFetch(expected)

      const result = await fetchAgents()
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/agents')
    })

    it('throws on non-ok response', async () => {
      globalThis.fetch = mockFetch(null, false, 'Not Found')
      await expect(fetchAgents()).rejects.toThrow('Failed to fetch agents: Not Found')
    })
  })

  describe('fetchAgent', () => {
    it('calls GET /api/agents/:name', async () => {
      const expected = { name: 'test', source: 's', yaml: 'y', config: {} }
      globalThis.fetch = mockFetch(expected)

      const result = await fetchAgent('test')
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/agents/test')
    })

    it('encodes agent name', async () => {
      globalThis.fetch = mockFetch({})
      await fetchAgent('my agent')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/agents/my%20agent')
    })
  })

  describe('saveAgent', () => {
    it('calls PUT /api/agents/:name with yaml body', async () => {
      const expected = { status: 'ok', path: '/path' }
      globalThis.fetch = mockFetch(expected)

      const result = await saveAgent('test', 'yaml: content')
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/agents/test', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml: 'yaml: content' }),
      })
    })
  })

  describe('deleteAgent', () => {
    it('calls DELETE /api/agents/:name', async () => {
      const expected = { status: 'ok', deleted: 'test' }
      globalThis.fetch = mockFetch(expected)

      const result = await deleteAgent('test')
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/agents/test', { method: 'DELETE' })
    })
  })

  describe('fetchTools', () => {
    it('calls GET /api/tools', async () => {
      const expected = { tools: [{ name: 't', description: 'd', source: 's' }] }
      globalThis.fetch = mockFetch(expected)

      const result = await fetchTools()
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/tools')
    })
  })

  describe('checkMcpDependencies', () => {
    it('calls POST with dependencies array', async () => {
      const deps = [{ server: 's', tools: ['t'], source: 'src' }]
      const expected = { dependencies: deps, all_installed: true, missing: 0 }
      globalThis.fetch = mockFetch(expected)

      const result = await checkMcpDependencies(deps)
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/mcp-dependencies/check', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ dependencies: deps }),
      })
    })
  })

  describe('getMcpStoreServer', () => {
    it('preserves forward slashes in store ID', async () => {
      globalThis.fetch = mockFetch({})
      await getMcpStoreServer('org/repo')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/mcp-store/org/repo')
    })
  })

  describe('installMcpServer', () => {
    it('calls POST with env', async () => {
      const expected = { status: 'ok', serverName: 's', toolsLoaded: 3 }
      globalThis.fetch = mockFetch(expected)

      const result = await installMcpServer('org/repo', { KEY: 'val' })
      expect(result).toEqual(expected)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/mcp-store/org/repo/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ env: { KEY: 'val' } }),
      })
    })

    it('throws error text on failure', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        text: () => Promise.resolve('bad request'),
      })
      await expect(installMcpServer('x')).rejects.toThrow('bad request')
    })
  })

  describe('installInlineMcpServer', () => {
    it('calls POST with serverName and config', async () => {
      const expected = { status: 'ok', serverName: 's' }
      globalThis.fetch = mockFetch(expected)

      await installInlineMcpServer('my-server', { cmd: 'echo' })
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/mcp/install-inline', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverName: 'my-server', config: { cmd: 'echo' } }),
      })
    })
  })

  describe('fetchStandardServers', () => {
    it('calls GET /api/standard-servers', async () => {
      const expected = { servers: [{ id: '1', name: 'n', description: 'd', installed: true }] }
      globalThis.fetch = mockFetch(expected)

      const result = await fetchStandardServers()
      expect(result).toEqual(expected)
    })
  })

  describe('installStandardServer', () => {
    it('calls POST with env', async () => {
      globalThis.fetch = mockFetch({ status: 'ok', serverName: 's', toolsLoaded: 1 })

      await installStandardServer('my-id', { API_KEY: 'abc' })
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/standard-servers/my-id/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ env: { API_KEY: 'abc' } }),
      })
    })
  })
})
