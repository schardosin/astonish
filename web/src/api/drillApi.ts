/**
 * API client for Drill Management (drill suites, individual drills, reports)
 */

const DRILLS_API = '/api/drills'
const REPORTS_API = '/api/drill-reports'

// --- Types ---

export interface DrillSuiteSummary {
  name: string
  description: string
  file: string
  drill_count: number
  template: string
  last_status: string
  last_run_at: string
  last_summary: string
}

export interface DrillSuiteDetail {
  name: string
  description: string
  file: string
  suite_config: Record<string, unknown>
  drills: DrillDetail[]
  last_report: DrillReport | null
}

export interface DrillDetail {
  name: string
  description: string
  file: string
  suite: string
  tags: string[]
  timeout: number
  step_timeout: number
  step_count: number
  on_fail: string
  nodes: unknown[]
  flow: unknown[]
}

export interface DrillReportSummary {
  suite: string
  status: string
  summary: string
  duration_ms: number
  started_at: string
  finished_at: string
  drill_count: number
}

export interface DrillReport {
  suite: string
  status: string
  summary: string
  duration_ms: number
  started_at: string
  finished_at: string
  drills: DrillResult[]
  [key: string]: unknown
}

export interface DrillResult {
  name: string
  status: string
  duration_ms: number
  steps: unknown[]
  [key: string]: unknown
}

// --- API Functions ---

export async function fetchDrillSuites(): Promise<DrillSuiteSummary[]> {
  const response = await fetch(DRILLS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill suites: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchDrillSuite(name: string): Promise<DrillSuiteDetail> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill suite: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchDrill(suite: string, name: string): Promise<DrillDetail> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill: ${response.statusText}`)
  }
  return response.json()
}

export async function deleteDrillSuite(name: string): Promise<{ status: string; deleted: string[] }> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete drill suite: ${response.statusText}`)
  }
  return response.json()
}

export async function deleteDrill(suite: string, name: string): Promise<{ status: string; deleted: string[]; suite: string }> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete drill: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchDrillReports(): Promise<DrillReportSummary[]> {
  const response = await fetch(REPORTS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill reports: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchDrillReport(suite: string): Promise<DrillReport> {
  const response = await fetch(`${REPORTS_API}/${encodeURIComponent(suite)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill report: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchDrillYaml(suite: string, name: string): Promise<string> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}/yaml`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill YAML: ${response.statusText}`)
  }
  return response.text()
}

export async function saveDrillYaml(suite: string, name: string, content: string): Promise<{ status: string; suite: string; drill: string }> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}/yaml`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/yaml' },
    body: content,
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}

export async function fetchSuiteYaml(suite: string): Promise<string> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/yaml`)
  if (!response.ok) {
    throw new Error(`Failed to fetch suite YAML: ${response.statusText}`)
  }
  return response.text()
}

export async function saveSuiteYaml(suite: string, content: string): Promise<{ status: string; suite: string }> {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/yaml`, {
    method: 'PUT',
    headers: { 'Content-Type': 'text/yaml' },
    body: content,
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `HTTP ${response.status}`)
  }
  return response.json()
}
