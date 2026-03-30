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
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills')
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
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/my-suite')
    })
  })

  describe('fetchDrill', () => {
    it('calls GET /api/drills/:suite/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ name: 'd', suite: 's' })
      await fetchDrill('suite1', 'drill1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/suite1/drills/drill1')
    })

    it('encodes special characters', async () => {
      globalThis.fetch = mockFetchJson({})
      await fetchDrill('my suite', 'my drill')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/my%20suite/drills/my%20drill')
    })
  })

  describe('deleteDrillSuite', () => {
    it('calls DELETE /api/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', deleted: [] })
      await deleteDrillSuite('suite1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/suite1', { method: 'DELETE' })
    })
  })

  describe('deleteDrill', () => {
    it('calls DELETE /api/drills/:suite/drills/:name', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', deleted: [], suite: 's' })
      await deleteDrill('s', 'd')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d', { method: 'DELETE' })
    })
  })

  describe('fetchDrillReports', () => {
    it('calls GET /api/drill-reports', async () => {
      globalThis.fetch = mockFetchJson([])
      await fetchDrillReports()
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drill-reports')
    })
  })

  describe('fetchDrillReport', () => {
    it('calls GET /api/drill-reports/:suite', async () => {
      globalThis.fetch = mockFetchJson({ suite: 's' })
      await fetchDrillReport('suite1')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drill-reports/suite1')
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
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d/yaml')
    })
  })

  describe('saveDrillYaml', () => {
    it('calls PUT with text/yaml content type', async () => {
      globalThis.fetch = mockFetchJson({ status: 'ok', suite: 's', drill: 'd' })
      await saveDrillYaml('s', 'd', 'name: test')
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/drills/d/yaml', {
        method: 'PUT',
        headers: { 'Content-Type': 'text/yaml' },
        body: 'name: test',
      })
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
      expect(globalThis.fetch).toHaveBeenCalledWith('/api/drills/s/yaml', {
        method: 'PUT',
        headers: { 'Content-Type': 'text/yaml' },
        body: 'content',
      })
    })
  })
})
