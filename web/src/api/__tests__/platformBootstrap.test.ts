import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { initializePlatform, initializeSQLitePlatform } from '../platform'

const sqliteParams = { org_name: 'Example', org_slug: 'example', auth_mode: 'none' as const }
const pgParams = {
  host: 'localhost', port: 5432, user: 'postgres', password: 'database-password',
  ssl_mode: 'prefer', org_name: 'Example', org_slug: 'example',
}
const cases = [
  { name: 'PostgreSQL', url: '/api/platform/init', params: pgParams, initialize: (token: string) => initializePlatform(pgParams, token) },
  { name: 'SQLite', url: '/api/platform/init/sqlite', params: sqliteParams, initialize: (token: string) => initializeSQLitePlatform(sqliteParams, token) },
]
const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => vi.unstubAllGlobals())

describe.each(cases)('$name bootstrap transport', ({ url, params, initialize }) => {
  it('sends bootstrap authority only in its header, not the URL or JSON body', async () => {
    const result = { success: true, message: 'Initialized', restart_required: true }
    fetchMock.mockResolvedValue(new Response(JSON.stringify(result)))
    await expect(initialize('operator-bootstrap-secret')).resolves.toEqual(result)
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Astonish-Bootstrap-Token': 'operator-bootstrap-secret' },
      body: JSON.stringify(params),
    })
    const [requestURL, request] = fetchMock.mock.calls[0]
    expect(String(requestURL)).not.toContain('operator-bootstrap-secret')
    expect(request?.body).not.toContain('operator-bootstrap-secret')
    expect(JSON.parse(request?.body as string)).toEqual(params)
  })

  it.each(['', '   '])('rejects missing/blank authority before sending a request (%j)', async token => {
    await expect(initialize(token)).rejects.toThrow('Bootstrap token is required')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves backend authorization errors for correction and retry', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ error: 'Explicit bootstrap authorization required' }), { status: 403 }))
    await expect(initialize('wrong-token')).rejects.toThrow('Explicit bootstrap authorization required')
  })

  it('preserves network failures', async () => {
    fetchMock.mockRejectedValue(new Error('Network unavailable'))
    await expect(initialize('operator-token')).rejects.toThrow('Network unavailable')
  })
})
