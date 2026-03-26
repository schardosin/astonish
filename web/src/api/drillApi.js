/**
 * API client for Drill Management (drill suites, individual drills, reports)
 */

const DRILLS_API = '/api/drills'
const REPORTS_API = '/api/drill-reports'

/**
 * Fetch all drill suites with drill counts and last report status.
 * @returns {Promise<Array<{name: string, description: string, file: string, drill_count: number, template: string, last_status: string, last_run_at: string, last_summary: string}>>}
 */
export async function fetchDrillSuites() {
  const response = await fetch(DRILLS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill suites: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch full detail for a single drill suite.
 * @param {string} name - Suite name
 * @returns {Promise<{name: string, description: string, file: string, suite_config: object, drills: Array, last_report: object}>}
 */
export async function fetchDrillSuite(name) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill suite: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch a single drill's full config.
 * @param {string} suite - Suite name
 * @param {string} name - Drill name
 * @returns {Promise<{name: string, description: string, file: string, suite: string, tags: string[], timeout: number, step_timeout: number, on_fail: string, nodes: Array, flow: Array}>}
 */
export async function fetchDrill(suite, name) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Delete a drill suite and all its drills.
 * @param {string} name - Suite name
 * @returns {Promise<{status: string, deleted: string[]}>}
 */
export async function deleteDrillSuite(name) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete drill suite: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Delete a single drill from a suite.
 * @param {string} suite - Suite name
 * @param {string} name - Drill name
 * @returns {Promise<{status: string, deleted: string[], suite: string}>}
 */
export async function deleteDrill(suite, name) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete drill: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch all drill reports.
 * @returns {Promise<Array<{suite: string, status: string, summary: string, duration_ms: number, started_at: string, finished_at: string, drill_count: number}>>}
 */
export async function fetchDrillReports() {
  const response = await fetch(REPORTS_API)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill reports: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch full report for a specific suite.
 * @param {string} suite - Suite name
 * @returns {Promise<object>} Full SuiteReport JSON
 */
export async function fetchDrillReport(suite) {
  const response = await fetch(`${REPORTS_API}/${encodeURIComponent(suite)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill report: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch raw YAML source for a single drill.
 * @param {string} suite - Suite name
 * @param {string} name - Drill name
 * @returns {Promise<string>} Raw YAML text
 */
export async function fetchDrillYaml(suite, name) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/drills/${encodeURIComponent(name)}/yaml`)
  if (!response.ok) {
    throw new Error(`Failed to fetch drill YAML: ${response.statusText}`)
  }
  return response.text()
}

/**
 * Save raw YAML source for a single drill.
 * @param {string} suite - Suite name
 * @param {string} name - Drill name
 * @param {string} content - Raw YAML content
 * @returns {Promise<{status: string, suite: string, drill: string}>}
 */
export async function saveDrillYaml(suite, name, content) {
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

/**
 * Fetch raw YAML source for a drill suite definition.
 * @param {string} suite - Suite name
 * @returns {Promise<string>} Raw YAML text
 */
export async function fetchSuiteYaml(suite) {
  const response = await fetch(`${DRILLS_API}/${encodeURIComponent(suite)}/yaml`)
  if (!response.ok) {
    throw new Error(`Failed to fetch suite YAML: ${response.statusText}`)
  }
  return response.text()
}

/**
 * Save raw YAML source for a drill suite definition.
 * @param {string} suite - Suite name
 * @param {string} content - Raw YAML content
 * @returns {Promise<{status: string, suite: string}>}
 */
export async function saveSuiteYaml(suite, content) {
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
