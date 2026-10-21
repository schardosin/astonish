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

// Only register tracks when their real tests exist. "all" is deliberately not
// an alias for the incomplete urgent suite.
const securityPackage = 'github.com/SAP/astonish/tests/e2e/chat_auth'
const securityManifests = {
  urgent: {
    packages: [securityPackage],
    tests: ['LegacyMCPLocalAuthorization', ...[
      ...['toggle', 'refresh'].flatMap(operation => ['unauthenticated', 'invalid_jwt', 'member_default', 'member_team', 'member_org', 'member_platform', 'team_admin_org', 'team_admin_platform', 'team_admin_sibling', 'team_admin_foreign', 'org_owner_platform', 'platform_admin_team', 'platform_admin_org', 'platform_only_default', 'platform_only_org', 'effective_scope', 'unknown_scope'].map(name => `${operation}/${name}`)),
      ...['team_default', 'team_explicit', 'org', 'platform_only'].map(name => `authorized_refresh/${name}`),
      ...['team_default', 'team_explicit', 'org', 'platform_only'].map(name => `authorized_missing_toggle/${name}`),
      ...['both', 'search_only', 'extract_only', 'neither', 'other_scope_matching'].map(name => `platform_settings_install_uninstall/${name}`),
      'default_team_does_not_mutate_platform_row',
      ...['team', 'platform'].flatMap(scope => ['PATCH', 'POST'].map(method => `missing_${scope}_store/${method}`)),
      ...['explicit_team', 'omitted_means_team', 'org_owner', 'platform_only'].map(name => `persisted_toggle/${name}`),
      ...['org_admin_default', 'org_admin_team', 'org_admin_org', 'platform_only'].flatMap(scope =>
        ['create', 'update', 'toggle', 'inline_install', 'legacy_settings', 'legacy_patch', 'delete'].map(operation => `scoped_mutation/${scope}/${operation}`)),
      ...['org_admin_default', 'org_admin_team', 'org_admin_org', 'platform_only'].flatMap(scope =>
        ['modern', 'legacy'].map(route => `existing_refresh/${scope}/${route}`)),
      ...['org_admin_default', 'org_admin_team', 'org_admin_org'].map(scope => `org_admin_standard_install_uninstall/${scope}`),
      ...['create', 'update', 'delete', 'toggle', 'refresh', 'inline_install', 'legacy_settings', 'standard_install', 'standard_uninstall'].flatMap(operation =>
        ['unauthenticated', 'member_default', 'member_team', 'member_org', 'member_platform', 'org_admin_platform', 'team_admin_org', 'platform_only_default', 'effective_scope', 'unknown_scope', 'missing_default_team_store', 'missing_org_store', 'missing_team_store', 'missing_platform_store'].map(name => `remaining_rejected/${operation}/${name}`)),
      ...['legacy_patch', 'legacy_refresh'].map(operation => `remaining_rejected/${operation}/org_admin_platform`),
      // These slash-named leaves emit no intermediate group/scope/route parents.
      ...['team', 'org', 'platform'].flatMap(scope => ['modern', 'legacy', 'inline'].flatMap(route => [
        'nonexistent_env', 'nonexistent_url', 'inherited_env', 'inherited_url', 'wrong_target_env', 'wrong_target_url', 'wrong_key', 'changed_masked_url', 'changed_destination', 'changed_command',
        ...(route === 'inline' ? ['existing_mask_rejected', 'replace_credentials', 'clear_credentials']
          : ['get_mask_roundtrip', 'omitted_env', 'empty_env', 'replace_credentials', 'clear_credentials', 'changed_args', 'changed_transport', 'omitted_env_changed_destination']),
        ...(route === 'modern' ? ['omitted_env_url', 'null_env_url'] : []),
      ].map(name => `masked_update/${scope}/${route}/${name}`))),
    ].map(name => `LegacyMCPLocalAuthorization/${name}`),
      // Full-config uses nested t.Run calls, so every real parent is required.
      'FullConfigMasksLocal', ...[
        'platform_admin_masked_roundtrip', 'rejected_masks',
        ...['proxy', 'remote_cdp_url', 'embedding'].flatMap(field => [
          `rejected_masks/${field}`,
          ...(field === 'embedding'
            ? ['invalid', 'nonexistent', 'changed_base_url', 'changed_provider', 'changed_base_url_omitted_key', 'changed_provider_omitted_key', 'changed_base_url_null_key']
            : ['invalid', 'nonexistent', 'changed_host', 'changed_path', 'changed_scheme', 'changed_port', 'changed_query', 'encoded_nonexistent'])
            .map(name => `rejected_masks/${field}/${name}`),
        ]),
        'denied_roles', ...['anonymous', 'org_owner', 'org_admin', 'team_admin', 'member'].flatMap(actor => [
          `denied_roles/${actor}`, ...['masked', 'malformed'].map(name => `denied_roles/${actor}/${name}`),
        ]),
        'legitimate_updates', ...['omitted', 'null', 'clear', 'replacement', 'changed_destination_clear', 'changed_destination_replacement']
          .map(name => `legitimate_updates/${name}`),
      ].map(name => `FullConfigMasksLocal/${name}`),
      'IndependentRoleAuthority', ...['org-admin-settings', 'org-admin-providers', 'team-admin-providers', 'platform-admin-full-config'].map(name => `IndependentRoleAuthority/${name}`),
      'PlatformProviderMasks', ...['redacted-read', 'org-owner-denied', 'team-admin-denied', 'member-denied', 'exact', 'stale', 'unknown-instance', 'foreign-org-instance', 'foreign-team-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'deleted', 'clear'].map(name => `PlatformProviderMasks/${name}`),
      'MCPInstallIsolation', ...['absent_globals_default_team_install_uninstall', 'absent_globals_team_install_uninstall', 'absent_globals_org_install_uninstall', 'absent_globals_member_denied', 'member_default_team_denied', 'member_team_denied', 'member_org_denied', 'member_platform_denied', 'owner_platform_denied', 'effective_scope_denied', 'unknown_scope_denied', 'foreign_team_denied', 'default_team_install_uninstall_preserves_globals', 'team_install_uninstall_preserves_globals', 'org_install_uninstall_preserves_globals', 'team_admin_default_team_install_uninstall_preserves_globals', 'team_admin_team_install_uninstall_preserves_globals', 'team_admin_org_denied', 'team_admin_platform_denied', 'team_admin_sibling_team_denied', 'platform_admin_platform_install_uninstall_scoped_effects'].map(name => `MCPInstallIsolation/${name}`),
      'FirstRunSetup', ...['none', 'builtin'].flatMap(mode => [
        `FirstRunSetup/${mode}`,
        ...['sqlite_missing_token_no_effects', 'sqlite_wrong_token_no_effects', 'postgres_missing_token_no_effects', 'postgres_wrong_token_no_effects', 'sqlite_authorized_init_persists_local_user_org_team', 'sqlite_configured_retry_no_effects', 'postgres_configured_retry_no_effects'].map(name => `FirstRunSetup/${mode}/${name}`),
      ]), 'FirstRunSetup/builtin/builtin_restart_login_authorized_settings',
      'TeamProviderMasks', ...['patch', 'replace'].flatMap(mode => [
      `TeamProviderMasks/${mode}`,
      ...['redacted-read', 'member-denied', 'exact', 'stale', 'unknown-instance', 'inherited-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'empty', 'clear'].map(name => `TeamProviderMasks/${mode}/${name}`),
    ]), 'SameSlugCrossOrgSessions', ...['alice@acme.test', 'dave@globex.test'].flatMap(actor => [
      `SameSlugCrossOrgSessions/${actor}`,
      ...['read-list-denied', 'delete-denied', 'injection-denied', 'owner-resume'].map(name => `SameSlugCrossOrgSessions/${actor}/${name}`),
      `SameSlugCrossOrgSessions/owner-delete/${actor}`,
    ]), 'OrgProviderMasks', ...['redacted-read', 'member-denied', 'exact', 'stale', 'unknown-instance', 'foreign-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'deleted', 'clear'].map(name => `OrgProviderMasks/${name}`),
      'ConfigAuthority', 'SessionOwnership', 'ArtifactOwnership', 'MCPAuthority', 'MemoryAuthority', 'ChannelOwnership',
      'SessionOwnership/bob@acme.test', 'SessionOwnership/carol@acme.test', 'SessionOwnership/eve@globex.test',
      'ConfiguredInit', 'ConfiguredInit/postgres', 'ConfiguredInit/postgres/alice@acme.test', 'ConfiguredInit/postgres/bob@acme.test',
      'ConfiguredInit/sqlite', 'ConfiguredInit/sqlite/alice@acme.test', 'ConfiguredInit/sqlite/bob@acme.test', 'SameTeamOwnerDenied',
      'ChannelProofLifecycle', 'ChannelProofLifecycle/direct-verification', 'ChannelProofLifecycle/expired-proof',
      'ChannelProofLifecycle/concurrent-proof-and-replay', 'ChannelProofLifecycle/linked-to-other-user',
      'ScopedMemoryCleanup', ...['source', 'duplicate'].flatMap(participant =>
        ['personal', 'org'].flatMap(scope => ['success', 'failure'].map(outcome => `ScopedMemoryCleanup/${participant}/${scope}/${outcome}`))),
      'MemoryParticipants', ...['source', 'duplicate', 'discard', 'destination', 'explicit-destination'].flatMap(name => [
        `MemoryParticipants/${name}`, `MemoryParticipants/${name}/denied`, `MemoryParticipants/${name}/allowed`,
      ])]
      .map(name => `${securityPackage}/TestE2E_SecurityUrgent${name}`),
  },
}
const track = process.env.ASTONISH_SECURITY_TRACK
const storage = process.env.ASTONISH_E2E_BACKEND
const runtime = process.env.ASTONISH_E2E_SANDBOX_BACKEND
const manifest = Object.hasOwn(securityManifests, track ?? '') ? securityManifests[track] : undefined
function selectionErrors(matrix = false) {
  const errors = []
  if (!manifest) errors.push('unsupported or missing security track')
  if (matrix ? storage !== 'both' : !['postgres', 'sqlite'].includes(storage)) errors.push('unsupported or missing security storage selection')
  if (runtime !== 'k8s') errors.push('unsupported or missing security runtime')
  return errors
}
if (process.argv[2] === '--security-selection') {
  const errors = selectionErrors(true)
  if (errors.length) console.error('Security acceptance FAILED:\n' + errors.join('\n'))
  process.exit(errors.length ? 1 : 0)
}

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
const requiredSecurityTests = [...(manifest?.tests ?? []),
  ...(track === 'urgent' && storage === 'postgres'
    ? ['', '/setup', '/restart'].map(suffix => `${securityPackage}/TestE2E_SecurityPostgresFirstRunSetup${suffix}`) : []),
]
const packagesPassed = new Set()
const runs = new Set()
const passed = new Set()
const states = new Map()
const errors = strict ? selectionErrors() : []
const revision = process.env.ASTONISH_SECURITY_REVISION
const testCommand = process.env.ASTONISH_SECURITY_TEST_COMMAND
if (strict && !/^[a-f0-9]{40}(?:-dirty)?$/.test(revision ?? '')) errors.push('missing or invalid security revision')
if (strict && !testCommand?.trim()) errors.push('missing security test command')
const actions = new Set(['start', 'run', 'pause', 'cont', 'pass', 'bench', 'fail', 'output', 'skip'])
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
    if (!actions.has(event.Action) || typeof event.Package !== 'string' || !event.Package ||
        (event.Test !== undefined && !event.Test) ||
        (event.Action === 'output' && typeof event.Output !== 'string')) {
      errors.push('invalid event shape or action')
      continue
    }
    if (!manifest?.packages.includes(event.Package)) errors.push(`${event.Package}: unexpected package`)
    if (packagesPassed.has(event.Package)) errors.push(`${key}: event after package completion`)
    if (event.Action === 'fail' || event.Action === 'skip') errors.push(`${key}: ${event.Action}`)
    if (!event.Test) {
      if (['run', 'pause', 'cont', 'bench'].includes(event.Action)) errors.push(`${key}: test action without test`)
      if (event.Action === 'pass') {
        for (const [test, state] of states) {
          if (test.startsWith(`${event.Package}/`) && state !== 'pass') errors.push(`${test}: unfinished at package completion`)
        }
        packagesPassed.add(event.Package)
      }
    } else {
      const state = states.get(key)
      switch (event.Action) {
        case 'run':
          if (state) errors.push(`${key}: duplicate run`)
          states.set(key, 'run')
          runs.add(key)
          break
        case 'pause':
        case 'cont':
          if (state !== (event.Action === 'pause' ? 'run' : 'pause')) errors.push(`${key}: invalid ${event.Action}`)
          states.set(key, event.Action === 'pause' ? 'pause' : 'run')
          break
        case 'pass':
          if (state !== 'run') errors.push(`${key}: pass without active run`)
          states.set(key, 'pass')
          passed.add(key)
          break
        case 'output':
        case 'fail':
        case 'skip':
          break
        default:
          errors.push(`${key}: invalid test action`)
      }
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
  for (const pkg of manifest?.packages ?? []) {
    if (!packagesPassed.has(pkg)) errors.push(`${pkg}: security package did not finish successfully`)
  }
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
  test_command: strict ? testCommand : 'go test -tags=e2e -count=1 -p 1 -timeout=15m -json ./tests/e2e/...',
  ...(strict ? { security: {
    track, storage, runtime, revision,
    counts: { executed: runs.size, passed: passed.size, required: requiredSecurityTests.length, packages: packagesPassed.size },
  } } : {}),
  scenarios: scenarioOutcomes,
  tests: testResults,
}

writeFileSync(OUTPUT, JSON.stringify(output, null, 2) + '\n')
