import { describe, it, expect, vi, afterEach } from 'vitest'
import { fetchFullConfig, saveFullConfigSection } from '../../components/settings/settingsApi'

function mockFetch(data: unknown, ok = true) {
  return vi.fn().mockResolvedValue({
    ok,
    json: () => Promise.resolve(data),
  })
}

describe('settingsApi', () => {
  const originalFetch = globalThis.fetch

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  describe('fetchFullConfig', () => {
    it('calls GET /api/settings/full', async () => {
      const config = { general: { theme: 'dark' }, credentials: {} }
      globalThis.fetch = mockFetch(config)

      const result = await fetchFullConfig()
      expect(result).toEqual(config)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/settings/full', expect.objectContaining({ headers: expect.any(Headers) }))
    })

    it('throws on error', async () => {
      globalThis.fetch = mockFetch(null, false)
      await expect(fetchFullConfig()).rejects.toThrow('Failed to fetch config')
    })
  })

  describe('saveFullConfigSection', () => {
    it('calls PUT /api/settings/full with section data', async () => {
      const response = { status: 'ok' }
      globalThis.fetch = mockFetch(response)

      const result = await saveFullConfigSection('general', { theme: 'light' })
      expect(result).toEqual(response)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/settings/full', expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({ general: { theme: 'light' } }),
      }))
      const headers = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1]?.headers as Headers
      expect(headers.get('Content-Type')).toBe('application/json')
    })

    it('throws on error', async () => {
      globalThis.fetch = mockFetch(null, false)
      await expect(saveFullConfigSection('x', {})).rejects.toThrow('Failed to save settings')
    })
  })
})
