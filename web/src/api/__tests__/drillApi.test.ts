import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  fetchDrillSuites,
  fetchDrillSuite,
  fetchDrill,
  deleteDrillSuite,
  deleteDrill,
  fetchDrillReports,
  fetchDrillReport,
  fetchDrillYaml,
  saveDrillYaml,
  fetchSuiteYaml,
  saveSuiteYaml,
} from '../../api/drillApi'

function mockFetchJson(data: unknown, ok = true, statusText = 'OK') {
  return vi.fn().mockResolvedValue({
    ok,
    statusText,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(typeof data === 'string' ? data : JSON.stringify(data)),
  })
}

// Helper to extract headers from the fetch mock call
function getCallHeaders(fetchMock: ReturnType<typeof vi.fn>, callIndex = 0): Headers {
  return fetchMock.mock.calls[callIndex][1]?.headers as Headers
}

describe('drillApi', () => {
  const originalFetch = globalThis.fetch

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  describe('fetchDrillSuites', () => {
    it('calls GET /api/drills', async () => {
      const suites = [{ name: 's', description: 'd', file: 'f', drill_count: 1, template: 't', last_status: '', last_run_at: '', last_summary: '' }]
      globalThis.fetch = mockFetchJson(suites)

      const result = await fetchDrillSuites()
      expect(result).toEqual(suites)
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills', expect.objectContaining({ headers: expect.any(Headers) }))
    })

    it('throws on error', async () => {
      globalThis.fetch = mockFetchJson(null, false, 'Error')
      await expect(fetchDrillSuites()).rejects.toThrow('Failed to fetch drill suites')
    })
  })

  describe('fetchDrillSuite', () => {
    it('calls GET /api/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ name: 's', drills: [] })
      await fetchDrillSuite('my-suite')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/my-suite', expect.objectContaining({ headers: expect.any(Headers) }))
    })
  })

  describe('fetchDrill', () => {
    it('calls GET /api/drills/:suite/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ name: 'd', suite: 's' })
      await fetchDrill('suite1', 'drill1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/suite1/drills/drill1', expect.objectContaining({ headers: expect.any(Headers) }))
    })

    it('encodes special characters', async () => {
      globalThis.fetch = mockFetchJson({})
      await fetchDrill('my suite', 'my drill')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/my%20suite/drills/my%20drill', expect.objectContaining({ headers: expect.any(Headers) }))
    })
  })

  describe('deleteDrillSuite', () => {
    it('calls DELETE /api/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', deleted: [] })
      await deleteDrillSuite('suite1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/suite1', expect.objectContaining({ method: 'DELETE' }))
    })
  })

  describe('deleteDrill', () => {
    it('calls DELETE /api/drills/:suite/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', deleted: [], suite: 's' })
      await deleteDrill('s', 'd')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d', expect.objectContaining({ method: 'DELETE' }))
    })
  })

  describe('fetchDrillReports', () => {
    it('calls GET /api/drill-reports', async () => {
      globalThis.fetch = mockFetchJson([])
      await fetchDrillReports()
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drill-reports', expect.objectContaining({ headers: expect.any(Headers) }))
    })
  })

  describe('fetchDrillReport', () => {
    it('calls GET /api/drill-reports/:suite', async () => {
      globalThis.fetch = mockFetchJson({ suite: 's' })
      await fetchDrillReport('suite1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drill-reports/suite1', expect.objectContaining({ headers: expect.any(Headers) }))
    })
  })

  describe('fetchDrillYaml', () => {
    it('calls GET and returns text', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        text: () => Promise.resolve('name: test'),
      })
      const result = await fetchDrillYaml('s', 'd')
      expect(result).toBe('name: test')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d/yaml', expect.objectContaining({ headers: expect.any(Headers) }))
    })
  })

  describe('saveDrillYaml', () => {
    it('calls PUT with text/yaml content type', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', suite: 's', drill: 'd' })
      await saveDrillYaml('s', 'd', 'name: test')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d/yaml', expect.objectContaining({
        method: 'PUT',
        body: 'name: test',
      }))
      const headers = getCallHeaders(globalThis.fetch as ReturnType<typeof vi.fn>)
      expect(headers.get('Content-Type')).toBe('text/yaml')
    })

    it('throws error text on failure', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        text: () => Promise.resolve('Invalid YAML'),
      })
      await expect(saveDrillYaml('s', 'd', 'bad')).rejects.toThrow('Invalid YAML')
    })
  })

  describe('fetchSuiteYaml', () => {
    it('calls GET and returns text', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        text: () => Promise.resolve('suite yaml'),
      })
      const result = await fetchSuiteYaml('s')
      expect(result).toBe('suite yaml')
    })
  })

  describe('saveSuiteYaml', () => {
    it('calls PUT with text/yaml content type', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', suite: 's' })
      await saveSuiteYaml('s', 'content')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/yaml', expect.objectContaining({
        method: 'PUT',
        body: 'content',
      }))
      const headers = getCallHeaders(globalThis.fetch as ReturnType<typeof vi.fn>)
      expect(headers.get('Content-Type')).toBe('text/yaml')
    })
  })
})
