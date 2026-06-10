#!/usr/bin/env node
/**
 * E2E Scenario Coverage Report
 *
 * Reads YAML catalog files under tests/scenarios/, cross-references with
 * COVERS: markers in Go E2E test files, and optionally reads .last-run.json
 * to verify that marked-as-covered tests actually passed.
 *
 * Usage:
 *   node tests/scenarios/report.mjs
 *   make scenario-coverage
 */

import { readFileSync, readdirSync, existsSync } from 'fs'
import { join, resolve } from 'path'
import { execFileSync } from 'child_process'

const ROOT = resolve(import.meta.dirname, '../..')
const SCENARIOS_DIR = resolve(import.meta.dirname)
const LAST_RUN_FILE = join(SCENARIOS_DIR, '.last-run.json')

// ---------------------------------------------------------------------------
// YAML Parser (minimal — handles our flat list-of-maps format)
// ---------------------------------------------------------------------------

function parseScenarioYaml(content) {
  const scenarios = []
  let current = null

  for (const line of content.split('\n')) {
    if (line.match(/^\s*#/) || line.trim() === '') continue

    const itemMatch = line.match(/^\s+-\s+id:\s*(.+)/)
    if (itemMatch) {
      if (current) scenarios.push(current)
      current = { id: itemMatch[1].trim().replace(/^"|"$/g, '') }
      continue
    }

    if (!current) continue

    const kvMatch = line.match(/^\s+(\w+):\s*(.*)/)
    if (kvMatch) {
      const [, key, rawVal] = kvMatch
      const val = rawVal.trim().replace(/^"|"$/g, '')

      if (key === 'test_refs') {
        current.test_refs = val === '[]' ? [] : []
      } else if (['priority', 'status', 'title', 'id'].includes(key)) {
        current[key] = val
      }
      continue
    }

    const arrMatch = line.match(/^\s+-\s+"?([^"]*)"?/)
    if (arrMatch && current.test_refs !== undefined) {
      current.test_refs.push(arrMatch[1])
    }
  }
  if (current) scenarios.push(current)
  return scenarios
}

// ---------------------------------------------------------------------------
// Load catalog
// ---------------------------------------------------------------------------

const catalogFiles = readdirSync(SCENARIOS_DIR)
  .filter(f => f.endsWith('.yaml'))
  .sort()

const allScenarios = []
const viewMap = {}

for (const file of catalogFiles) {
  const view = file.replace('.yaml', '')
  const content = readFileSync(join(SCENARIOS_DIR, file), 'utf8')
  const scenarios = parseScenarioYaml(content)
  viewMap[view] = scenarios
  for (const s of scenarios) {
    s.view = view
    allScenarios.push(s)
  }
}

// ---------------------------------------------------------------------------
// Scan for COVERS: markers in Go E2E test files
// ---------------------------------------------------------------------------

let coversOutput = ''
try {
  coversOutput = execFileSync(
    'grep', ['-rn', 'COVERS:', '--include=*.go', `${ROOT}/tests`],
    { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }
  )
} catch { /* ignore */ }

const coversMap = {}
for (const line of coversOutput.split('\n')) {
  const match = line.match(/^(.+?):(\d+):.*COVERS:\s*(.+)/)
  if (!match) continue
  const [, file, lineNo, ids] = match
  const relFile = file.replace(ROOT + '/', '')
  for (const rawId of ids.split(',')) {
    const id = rawId.trim()
    if (!coversMap[id]) coversMap[id] = []
    coversMap[id].push(`${relFile}:${lineNo}`)
  }
}

// ---------------------------------------------------------------------------
// Load last run results (if available)
// ---------------------------------------------------------------------------

let lastRun = null
if (existsSync(LAST_RUN_FILE)) {
  try {
    lastRun = JSON.parse(readFileSync(LAST_RUN_FILE, 'utf8'))
  } catch { /* ignore corrupt file */ }
}

// ---------------------------------------------------------------------------
// Compute statistics
// ---------------------------------------------------------------------------

const stats = {
  total: allScenarios.length,
  covered: 0,
  missing: 0,
  byView: {},
  byPriority: { P0: { total: 0, covered: 0 }, P1: { total: 0, covered: 0 }, P2: { total: 0, covered: 0 } },
}

const issues = []
const drifts = []

for (const view of Object.keys(viewMap).sort()) {
  const scenarios = viewMap[view]
  const vs = { total: scenarios.length, covered: 0, missing: 0, P0: { total: 0, covered: 0 }, P1: { total: 0, covered: 0 }, P2: { total: 0, covered: 0 } }

  for (const s of scenarios) {
    const p = s.priority || 'P2'
    vs[p].total++
    stats.byPriority[p].total++

    if (s.status === 'covered') {
      vs.covered++
      vs[p].covered++
      stats.covered++
      stats.byPriority[p].covered++
    } else {
      vs.missing++
      stats.missing++
    }

    // Cross-check: covered but no COVERS: marker and no test_refs
    if (s.status === 'covered' && (!s.test_refs || s.test_refs.length === 0) && !coversMap[s.id]) {
      issues.push(`STALE: ${s.id} marked 'covered' but no test_refs or COVERS: marker found`)
    }

    // Drift check: scenario says covered, but last run says skip/fail/not_run
    if (lastRun && s.status === 'covered') {
      const outcome = lastRun.scenarios?.[s.id]
      if (!outcome || outcome === 'not_run') {
        drifts.push({ id: s.id, title: s.title, reason: 'test not found in last run' })
      } else if (outcome === 'skip') {
        drifts.push({ id: s.id, title: s.title, reason: 'test was SKIPPED' })
      } else if (outcome === 'fail') {
        drifts.push({ id: s.id, title: s.title, reason: 'test FAILED' })
      }
    }
  }

  stats.byView[view] = vs
}

// Orphan check
const allIds = new Set(allScenarios.map(s => s.id))
for (const id of Object.keys(coversMap)) {
  if (!allIds.has(id)) {
    issues.push(`ORPHAN: COVERS: ${id} in ${coversMap[id].join(', ')} not in catalog`)
  }
}

// ---------------------------------------------------------------------------
// Print Report
// ---------------------------------------------------------------------------

const RESET = '\x1b[0m'
const BOLD = '\x1b[1m'
const GREEN = '\x1b[32m'
const RED = '\x1b[31m'
const YELLOW = '\x1b[33m'
const DIM = '\x1b[2m'

function pct(n, d) { return d === 0 ? '  -' : `${Math.round((n / d) * 100).toString().padStart(3)}%` }
function pad(s, n) { return s.padEnd(n) }

console.log('')
console.log(`${BOLD}E2E Scenario Coverage Report${RESET}`)
console.log('═'.repeat(60))
console.log('')

// Per-view table
console.log(`${BOLD}${pad('View', 10)} ${pad('Total', 7)} ${pad('Covered', 9)} ${pad('Missing', 9)} ${pad('P0', 10)} ${pad('P1', 10)} P2${RESET}`)
console.log('─'.repeat(60))

for (const view of Object.keys(stats.byView).sort()) {
  const v = stats.byView[view]
  const color = v.covered === v.total ? GREEN : v.covered > 0 ? YELLOW : RED

  console.log(
    `${pad(view, 10)} ` +
    `${pad(String(v.total), 7)} ` +
    `${color}${pad(String(v.covered), 9)}${RESET} ` +
    `${pad(String(v.missing), 9)} ` +
    `${pad(`${v.P0.covered}/${v.P0.total}`, 10)} ` +
    `${pad(`${v.P1.covered}/${v.P1.total}`, 10)} ` +
    `${v.P2.covered}/${v.P2.total}`
  )
}

console.log('─'.repeat(60))
const totalColor = stats.covered === stats.total ? GREEN : stats.covered > 0 ? YELLOW : RED
console.log(
  `${BOLD}${pad('TOTAL', 10)} ` +
  `${pad(String(stats.total), 7)} ` +
  `${totalColor}${pad(String(stats.covered), 9)}${RESET} ` +
  `${pad(String(stats.missing), 9)} ` +
  `${pad(`${stats.byPriority.P0.covered}/${stats.byPriority.P0.total}`, 10)} ` +
  `${pad(`${stats.byPriority.P1.covered}/${stats.byPriority.P1.total}`, 10)} ` +
  `${stats.byPriority.P2.covered}/${stats.byPriority.P2.total}${RESET}`
)

console.log('')
console.log(`${BOLD}Overall: ${totalColor}${stats.covered}/${stats.total} (${pct(stats.covered, stats.total).trim()})${RESET}`)
console.log(`${BOLD}P0 only: ${stats.byPriority.P0.covered === stats.byPriority.P0.total ? GREEN : YELLOW}${stats.byPriority.P0.covered}/${stats.byPriority.P0.total} (${pct(stats.byPriority.P0.covered, stats.byPriority.P0.total).trim()})${RESET}`)

// Last run info
if (lastRun) {
  const ts = new Date(lastRun.timestamp).toLocaleString()
  const passCount = Object.values(lastRun.scenarios || {}).filter(v => v === 'pass').length
  const skipCount = Object.values(lastRun.scenarios || {}).filter(v => v === 'skip').length
  const failCount = Object.values(lastRun.scenarios || {}).filter(v => v === 'fail').length
  console.log('')
  console.log(`${DIM}Last run: ${ts}${RESET}`)
  console.log(`${DIM}Results:  ${GREEN}${passCount} passed${RESET}${DIM}, ${YELLOW}${skipCount} skipped${RESET}${DIM}, ${RED}${failCount} failed${RESET}`)
} else {
  console.log('')
  console.log(`${DIM}No last run data. Run 'make test-e2e' first to collect results.${RESET}`)
}

// Covered scenarios
const coveredScenarios = allScenarios.filter(s => s.status === 'covered')
if (coveredScenarios.length > 0) {
  console.log('')
  console.log(`${BOLD}${GREEN}Covered Scenarios (${coveredScenarios.length})${RESET}`)
  console.log('─'.repeat(60))
  let lastView = ''
  for (const s of coveredScenarios) {
    if (s.view !== lastView) {
      if (lastView) console.log('')
      lastView = s.view
    }
    let statusStr = ''
    if (lastRun && lastRun.scenarios) {
      const outcome = lastRun.scenarios[s.id]
      if (outcome === 'pass') statusStr = `${GREEN}PASS${RESET}`
      else if (outcome === 'skip') statusStr = `${YELLOW}SKIP${RESET}`
      else if (outcome === 'fail') statusStr = `${RED}FAIL${RESET}`
      else statusStr = `${DIM}no data${RESET}`
    }
    console.log(`  ${GREEN}${pad(s.id, 12)}${RESET} ${s.title}${statusStr ? '  [' + statusStr + ']' : ''}`)
  }
  console.log('')
}

// Drift
if (drifts.length > 0) {
  console.log(`${BOLD}${RED}DRIFT: Scenarios marked 'covered' but not passing in last run${RESET}`)
  console.log('─'.repeat(60))
  for (const d of drifts) {
    console.log(`  ${RED}${pad(d.id, 12)}${RESET} ${d.title}`)
    console.log(`  ${DIM}           → ${d.reason}${RESET}`)
  }
  console.log('')
}

// Missing P0
const missingP0 = allScenarios.filter(s => s.priority === 'P0' && s.status !== 'covered')
if (missingP0.length > 0) {
  console.log(`${BOLD}${YELLOW}Missing P0 Scenarios${RESET}`)
  console.log('─'.repeat(60))
  for (const s of missingP0) {
    console.log(`  ${YELLOW}${pad(s.id, 12)}${RESET} ${s.title}`)
  }
  console.log('')
}

// Missing P1
const missingP1 = allScenarios.filter(s => s.priority === 'P1' && s.status !== 'covered')
if (missingP1.length > 0) {
  console.log(`${DIM}Missing P1 Scenarios (${missingP1.length})${RESET}`)
  console.log('─'.repeat(60))
  for (const s of missingP1) {
    console.log(`  ${DIM}${pad(s.id, 12)} ${s.title}${RESET}`)
  }
  console.log('')
}

// Issues
if (issues.length > 0) {
  console.log(`${BOLD}${RED}Issues${RESET}`)
  console.log('─'.repeat(60))
  for (const issue of issues) {
    console.log(`  ${issue}`)
  }
  console.log('')
}

// Exit code: non-zero only with --strict flag (for CI)
if (drifts.length > 0 && process.argv.includes('--strict')) {
  process.exit(1)
}
