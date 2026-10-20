#!/usr/bin/env node
/**
 * Parse go test -json output and emit .last-run.json
 *
 * Maps test function names to their COVERS: scenario IDs by scanning
 * the source files, then records pass/skip/fail status per scenario.
 *
 * Usage:
 *   node tests/scenarios/parse-run.mjs /tmp/e2e-results.json
 */

import { readFileSync, writeFileSync, readdirSync } from 'fs'
import { resolve, join } from 'path'
import { execFileSync } from 'child_process'

const ROOT = resolve(import.meta.dirname, '../..')
const OUTPUT = process.env.ASTONISH_SCENARIO_OUTPUT || resolve(import.meta.dirname, '.last-run.json')

const inputFile = process.argv[2]
if (!inputFile) {
  console.error('Usage: parse-run.mjs <go-test-json-file>')
  process.exit(1)
}

// ---------------------------------------------------------------------------
// Step 1: Build map of test function -> scenario IDs from COVERS: markers
// ---------------------------------------------------------------------------

let grepOutput = ''
try {
  grepOutput = execFileSync(
    'grep', ['-rn', 'COVERS:', '--include=*.go', `${ROOT}/tests`],
    { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }
  )
} catch { /* ignore */ }

// Parse COVERS: comments and associate them with the next test function
const funcToScenarios = {} // "TestE2E_FlowAssistant_CreateFlow" -> ["FLOWS-001"]

// For each COVERS: line, find what function it belongs to by reading the file
const coversLines = grepOutput.split('\n').filter(l => l.trim())
const fileCache = {}

for (const line of coversLines) {
  const match = line.match(/^(.+?):(\d+):.*COVERS:\s*(.+)/)
  if (!match) continue
  const [, filePath, lineNoStr, idsStr] = match
  const lineNo = parseInt(lineNoStr)
  const ids = idsStr.split(',').map(s => s.trim())

  // Read the file and find the next func declaration after this line
  if (!fileCache[filePath]) {
    try {
      fileCache[filePath] = readFileSync(filePath, 'utf8').split('\n')
    } catch { continue }
  }
  const lines = fileCache[filePath]

  // Look forward from the COVERS: line to find "func TestXxx("
  for (let i = lineNo; i < Math.min(lineNo + 5, lines.length); i++) {
    const funcMatch = lines[i].match(/^func\s+(Test\w+)\s*\(/)
    if (funcMatch) {
      const funcName = funcMatch[1]
      if (!funcToScenarios[funcName]) funcToScenarios[funcName] = []
      funcToScenarios[funcName].push(...ids)
      break
    }
  }
}

// ---------------------------------------------------------------------------
// Step 2: Parse go test -json output for terminal actions (pass/skip/fail)
// ---------------------------------------------------------------------------

const raw = readFileSync(inputFile, 'utf8')
const testResults = {} // "TestE2E_FlowAssistant_CreateFlow" -> "pass" | "skip" | "fail"

const strict = process.argv[3] === '--strict-security'
// Explicit manifest: deleting or filtering out a category must fail acceptance.
const securityPackage = 'github.com/SAP/astonish/tests/e2e/chat_auth'
const requiredSecurityTests = ['ConfigAuthority', 'SessionOwnership', 'ArtifactOwnership', 'MCPAuthority', 'MemoryAuthority', 'ChannelOwnership']
  .map(name => `${securityPackage}/TestE2E_SecurityUrgent${name}`)
const packagesPassed = new Set()
const runs = new Set()
const passed = new Set()
const errors = []
for (const line of raw.split('\n')) {
  if (!line.trim()) continue
  let event
  try { event = JSON.parse(line) } catch {
    if (strict) errors.push('invalid JSON event')
    continue
  }
  if (!event || typeof event !== 'object' || typeof event.Action !== 'string' ||
      (event.Test !== undefined && typeof event.Test !== 'string')) {
    if (strict) errors.push('invalid event shape')
    continue
  }
  if (strict) {
    const key = `${event.Package}/${event.Test}`
    if (!event.Test && event.Action === 'pass') packagesPassed.add(event.Package)
    if (event.Action === 'fail' || event.Action === 'skip') errors.push(`${key}: ${event.Action}`)
    if (event.Test?.startsWith('TestE2E_SecurityUrgent')) {
      if (event.Action === 'run') runs.add(key)
      if (event.Action === 'pass') passed.add(key)
    }
  }
  if (!event.Test) continue
  if (event.Action === 'pass' || event.Action === 'skip' || event.Action === 'fail') {
    testResults[event.Test] = event.Action
  }
}
if (strict) {
  if (runs.size === 0) errors.push('zero security tests executed')
  for (const key of runs) if (!passed.has(key)) errors.push(`${key}: missing pass`)
  for (const key of requiredSecurityTests) {
    if (!runs.has(key) || !passed.has(key)) errors.push(`${key}: required matrix entry missing`)
  }
  if (!packagesPassed.has(securityPackage)) errors.push('security package did not finish successfully')
  if (errors.length) {
    console.error('Security acceptance FAILED:\n' + errors.join('\n'))
    process.exit(1)
  }
}

// ---------------------------------------------------------------------------
// Step 3: Map scenario IDs to outcomes
// ---------------------------------------------------------------------------

const scenarioOutcomes = {} // "FLOWS-001" -> "pass" | "skip" | "fail" | "not_run"
const timestamp = new Date().toISOString()

// Initialize all known scenarios as "not_run"
// (We don't load the catalog here — the report script handles that)
// We just emit what we observed.

for (const [funcName, ids] of Object.entries(funcToScenarios)) {
  const outcome = testResults[funcName] || 'not_run'
  for (const id of ids) {
    scenarioOutcomes[id] = outcome
  }
}

// ---------------------------------------------------------------------------
// Step 4: Write output
// ---------------------------------------------------------------------------

const output = {
  timestamp,
  test_command: 'go test -tags=e2e -count=1 -p 1 -timeout=15m -json ./tests/e2e/...',
  scenarios: scenarioOutcomes,
  tests: testResults,
}

writeFileSync(OUTPUT, JSON.stringify(output, null, 2) + '\n')
