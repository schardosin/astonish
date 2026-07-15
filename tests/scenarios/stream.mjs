#!/usr/bin/env node
/**
 * stream.mjs — pretty-print `go test -json` events while preserving the raw
 * JSON stream to a file (for downstream tools like parse-run.mjs).
 *
 * Usage:
 *   go test -json ./... | node tests/scenarios/stream.mjs /tmp/e2e-results.json
 *
 * stdin:  one JSON object per line from `go test -json`
 * stdout: human-readable, color-coded progress
 * file:   raw JSON lines (verbatim copy of stdin)
 *
 * Exit code: 0 if all tests passed/skipped, 1 if any failed.
 */

import { createWriteStream } from 'fs'
import { createInterface } from 'readline'

const outputFile = process.argv[2]
if (!outputFile) {
  console.error('Usage: stream.mjs <output-json-file>')
  process.exit(2)
}

const isTTY = process.stdout.isTTY
const c = {
  reset: isTTY ? '\x1b[0m' : '',
  bold: isTTY ? '\x1b[1m' : '',
  dim: isTTY ? '\x1b[2m' : '',
  red: isTTY ? '\x1b[31m' : '',
  green: isTTY ? '\x1b[32m' : '',
  yellow: isTTY ? '\x1b[33m' : '',
  blue: isTTY ? '\x1b[34m' : '',
  cyan: isTTY ? '\x1b[36m' : '',
  gray: isTTY ? '\x1b[90m' : '',
}

// Output lines from `go test -json` are forwarded only when they look
// "interesting" — bootstrap progress, warnings, errors. Filters out the
// noisy chi/INFO lines and the redundant `--- PASS/FAIL/SKIP` lines (already
// rendered as colored result lines).
const INTERESTING_OUTPUT_RE = /^\s*(\[e2eboot\]|\[seed\]|WARN|ERROR|panic:)/
const SKIP_OUTPUT_RE = /^\s*---\s+(PASS|FAIL|SKIP):/

const startTime = Date.now()
let totals = { passed: 0, failed: 0, skipped: 0 }
let currentPackage = null
let currentTest = null
const failedTests = []
const skippedTests = []
// pkg+":"+test -> array of raw output strings for that test (used to extract
// skip/fail reasons emitted by t.Skip / t.Fatal / t.Errorf).
const testOutputs = new Map()

// Matches the reason line emitted by t.Skip/t.Fatal/t.Errorf, e.g.:
//   "    chat_core_test.go:553: expected reply to contain Zephyr"
const REASON_RE = /^\s*[\w./-]+\.go:\d+:\s*(.+)$/
// Bootstrap/seed framing lines also satisfy REASON_RE (they pass `t.Logf`
// from helpers like e2eboot.Bootstrap) — exclude them so we surface the
// actual failure/skip reason emitted by the test body.
const REASON_NOISE_RE = /\[(e2eboot[^\]]*|seed)\]/

function extractReason(pkg, test) {
  const key = pkg + '::' + test
  const lines = testOutputs.get(key) || []
  // The failure/skip reason is typically the LAST `<file>.go:<line>: <msg>`
  // line before the `--- FAIL/SKIP:` marker. Walk backwards.
  for (let i = lines.length - 1; i >= 0; i--) {
    const ln = lines[i].replace(/\n$/, '')
    if (SKIP_OUTPUT_RE.test(ln)) continue
    if (REASON_NOISE_RE.test(ln)) continue
    const m = ln.match(REASON_RE)
    if (m) return m[1].trim()
  }
  return ''
}

const raw = createWriteStream(outputFile)

function shortPackage(pkg) {
  // github.com/SAP/astonish/tests/e2e/chat_core -> tests/e2e/chat_core
  const idx = pkg.indexOf('/tests/e2e/')
  return idx >= 0 ? pkg.slice(idx + 1) : pkg
}

function formatElapsed(secs) {
  if (secs == null) return ''
  if (secs < 10) return `${secs.toFixed(2)}s`
  return `${secs.toFixed(1)}s`
}

function printPackageHeader(pkg) {
  const short = shortPackage(pkg)
  process.stdout.write(`\n${c.bold}${c.blue}▶ ${short}${c.reset}\n`)
}

function printTestStart(name) {
  process.stdout.write(`  ${c.gray}· ${name}${c.reset}\n`)
}

function printTestOutput(line) {
  // Already has trailing newline from the JSON Output field.
  process.stdout.write(`      ${c.dim}${line.replace(/\n$/, '')}${c.reset}\n`)
}

function printTestResult(name, action, elapsed) {
  let label, color
  switch (action) {
    case 'pass':
      label = 'PASS'
      color = c.green
      totals.passed++
      break
    case 'skip':
      label = 'SKIP'
      color = c.yellow
      totals.skipped++
      break
    case 'fail':
      label = 'FAIL'
      color = c.red
      totals.failed++
      break
    default:
      return
  }
  const t = formatElapsed(elapsed)
  process.stdout.write(`  ${color}${label}${c.reset} ${name} ${c.gray}(${t})${c.reset}\n`)
}

function printSummary() {
  const elapsed = ((Date.now() - startTime) / 1000).toFixed(1)
  const total = totals.passed + totals.failed + totals.skipped
  process.stdout.write('\n')
  process.stdout.write(`${c.bold}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${c.reset}\n`)
  process.stdout.write(`${c.bold}E2E test summary${c.reset}\n`)
  process.stdout.write(`  Total:    ${total}\n`)
  process.stdout.write(`  ${c.green}Passed:   ${totals.passed}${c.reset}\n`)
  if (totals.skipped > 0) {
    process.stdout.write(`  ${c.yellow}Skipped:  ${totals.skipped}${c.reset}\n`)
  }
  if (totals.failed > 0) {
    process.stdout.write(`  ${c.red}Failed:   ${totals.failed}${c.reset}\n`)
  }
  if (skippedTests.length > 0) {
    process.stdout.write(`\n  ${c.yellow}${c.bold}Skipped:${c.reset}\n`)
    for (const s of skippedTests) {
      const reason = s.reason ? ` ${c.gray}— ${s.reason}${c.reset}` : ''
      process.stdout.write(`    ${c.yellow}○${c.reset} ${s.pkg} :: ${s.test}${reason}\n`)
    }
  }
  if (failedTests.length > 0) {
    process.stdout.write(`\n  ${c.red}${c.bold}Failures:${c.reset}\n`)
    for (const f of failedTests) {
      const reason = f.reason ? ` ${c.gray}— ${f.reason}${c.reset}` : ''
      process.stdout.write(`    ${c.red}✗${c.reset} ${f.pkg} :: ${f.test}${reason}\n`)
    }
  }
  process.stdout.write(`  Elapsed:  ${elapsed}s\n`)
  process.stdout.write(`${c.bold}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${c.reset}\n`)
  process.stdout.write(`\n${c.dim}Run \`make scenario-coverage\` for catalog coverage report.${c.reset}\n`)
  process.stdout.write(`${c.dim}Raw JSON: ${outputFile}${c.reset}\n`)
}

const seenPackages = new Set()
const lastTestPerPackage = new Map() // pkg -> test name (for "current line" tracking)

const rl = createInterface({ input: process.stdin, terminal: false })

rl.on('line', (line) => {
  if (!line.trim()) return
  // Always preserve the raw line.
  raw.write(line + '\n')

  let evt
  try {
    evt = JSON.parse(line)
  } catch {
    // Non-JSON line (rare — go test sometimes emits a final summary line).
    process.stdout.write(`${c.dim}${line}${c.reset}\n`)
    return
  }

  const { Action: action, Package: pkg, Test: test, Output: output, Elapsed: elapsed } = evt

  // Package-level event (no Test field).
  if (!test) {
    if (action === 'start' && !seenPackages.has(pkg)) {
      seenPackages.add(pkg)
      printPackageHeader(pkg)
    } else if (action === 'fail' && pkg && !seenPackages.has(pkg + ':done')) {
      // Package failed — already shown via individual test fails.
      seenPackages.add(pkg + ':done')
    }
    return
  }

  // Test-level event. Only show top-level tests (no "/" in name).
  const isSubtest = test.includes('/')

  if (action === 'run' && !isSubtest) {
    currentPackage = pkg
    currentTest = test
    printTestStart(test)
    return
  }

  if (action === 'output' && !isSubtest && output) {
    // Capture every output line for this test so we can mine skip/fail
    // reasons in the summary.
    const key = pkg + '::' + test
    if (!testOutputs.has(key)) testOutputs.set(key, [])
    testOutputs.get(key).push(output)
    if (SKIP_OUTPUT_RE.test(output)) return
    if (INTERESTING_OUTPUT_RE.test(output)) {
      printTestOutput(output)
    }
    return
  }

  if ((action === 'pass' || action === 'skip' || action === 'fail') && !isSubtest) {
    printTestResult(test, action, elapsed)
    if (action === 'fail') {
      failedTests.push({ pkg: shortPackage(pkg), test, reason: extractReason(pkg, test) })
    } else if (action === 'skip') {
      skippedTests.push({ pkg: shortPackage(pkg), test, reason: extractReason(pkg, test) })
    }
    return
  }

  // For subtests, only surface failures (so users see specific assertion lines)
  if (isSubtest && action === 'fail') {
    process.stdout.write(`    ${c.red}↳ FAIL${c.reset} ${test}\n`)
    return
  }
})

rl.on('close', () => {
  raw.end()
  printSummary()
  process.exit(totals.failed > 0 ? 1 : 0)
})
