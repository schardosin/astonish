import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { refreshToken } from '../auth'
import {
  teamFetch,
  setActiveTeam,
  onAuthExpired,
  onTeamRejected,
} from '../teamContext'

vi.mock('../auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../auth')>()
  return {
    ...actual,
    refreshToken: vi.fn(),
  }
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('teamFetch auth refresh', () => {
  const originalFetch = globalThis.fetch
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    setActiveTeam(null)
    onAuthExpired(() => {})
    onTeamRejected(() => {})
    fetchMock = vi.fn()
    globalThis.fetch = fetchMock as typeof globalThis.fetch
    vi.mocked(refreshToken).mockReset()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    setActiveTeam(null)
  })

  it('returns non-401 responses unchanged without refreshing', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true }, 200))

    const res = await teamFetch('/api/studio/sessions')
    expect(res.status).toBe(200)
    expect(refreshToken).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('on 401 refreshes the token and retries the original request once', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: 'unauthorized', message: 'authentication required' }, 401))
      .mockResolvedValueOnce(jsonResponse([{ id: 's1' }], 200))
    vi.mocked(refreshToken).mockResolvedValueOnce({
      user: { id: 'u1', email: 'a@b.com', display_name: 'A', role: 'member' },
      org: { id: 'o1', name: 'Org', slug: 'org' },
      expires_in: 900,
    })

    const res = await teamFetch('/api/studio/chat', {
      method: 'POST',
      body: JSON.stringify({ message: 'hi' }),
    })

    expect(res.status).toBe(200)
    expect(await res.json()).toEqual([{ id: 's1' }])
    expect(refreshToken).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/studio/chat')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/studio/chat')
  })

  it('shares a single refresh across concurrent 401s', async () => {
    let resolveRefresh!: (value: unknown) => void
    const refreshPromise = new Promise((resolve) => { resolveRefresh = resolve })
    vi.mocked(refreshToken).mockReturnValueOnce(refreshPromise as ReturnType<typeof refreshToken>)

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))
      .mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))
      .mockResolvedValueOnce(jsonResponse({ a: 1 }, 200))
      .mockResolvedValueOnce(jsonResponse({ b: 2 }, 200))

    const p1 = teamFetch('/api/studio/sessions')
    const p2 = teamFetch('/api/studio/sessions/abc')

    // Let both requests hit 401 and await the shared refresh.
    await Promise.resolve()
    await Promise.resolve()
    expect(refreshToken).toHaveBeenCalledTimes(1)

    resolveRefresh({
      user: { id: 'u1', email: 'a@b.com', display_name: 'A', role: 'member' },
      org: { id: 'o1', name: 'Org', slug: 'org' },
      expires_in: 900,
    })

    const [r1, r2] = await Promise.all([p1, p2])
    expect(r1.status).toBe(200)
    expect(r2.status).toBe(200)
    expect(refreshToken).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('fires onAuthExpired and does not retry when refresh fails', async () => {
    const expired = vi.fn()
    onAuthExpired(expired)
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))
    vi.mocked(refreshToken).mockRejectedValueOnce(new Error('Token refresh failed'))

    const res = await teamFetch('/api/studio/chat', { method: 'POST' })

    expect(res.status).toBe(401)
    expect(expired).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not attempt refresh for /api/auth/ paths', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))

    const res = await teamFetch('/api/auth/me')
    expect(res.status).toBe(401)
    expect(refreshToken).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('retries on 401 even when no active team is set', async () => {
    setActiveTeam(null)
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))
      .mockResolvedValueOnce(jsonResponse({ ok: true }, 200))
    vi.mocked(refreshToken).mockResolvedValueOnce({
      user: { id: 'u1', email: 'a@b.com', display_name: 'A', role: 'member' },
      org: { id: 'o1', name: 'Org', slug: 'org' },
      expires_in: 900,
    })

    const res = await teamFetch('/api/settings/status')
    expect(res.status).toBe(200)
    expect(refreshToken).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('injects team header and still refreshes on 401', async () => {
    setActiveTeam('cronus')
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: 'unauthorized' }, 401))
      .mockResolvedValueOnce(jsonResponse({ ok: true }, 200))
    vi.mocked(refreshToken).mockResolvedValueOnce({
      user: { id: 'u1', email: 'a@b.com', display_name: 'A', role: 'member' },
      org: { id: 'o1', name: 'Org', slug: 'org' },
      expires_in: 900,
    })

    await teamFetch('/api/memories/team')
    const firstHeaders = fetchMock.mock.calls[0][1].headers as Headers
    expect(firstHeaders.get('X-Astonish-Team')).toBe('cronus')
    const retryHeaders = fetchMock.mock.calls[1][1].headers as Headers
    expect(retryHeaders.get('X-Astonish-Team')).toBe('cronus')
  })
})
