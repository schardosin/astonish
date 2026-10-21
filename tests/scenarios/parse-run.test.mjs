import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, readFileSync, rmSync, mkdirSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'

const Package = 'github.com/SAP/astonish/tests/e2e/chat_auth'
const event = (Action, suffix = 'ConfigAuthority') => ({ Action, Package, Test: `TestE2E_SecurityUrgent${suffix}` })
const scopedMemoryEntries = ['ScopedMemoryCleanup', ...['source', 'duplicate'].flatMap(participant =>
  ['personal', 'org'].flatMap(scope => ['success', 'failure'].map(outcome => `ScopedMemoryCleanup/${participant}/${scope}/${outcome}`)))]
const memoryCases = ['source', 'duplicate', 'discard', 'destination', 'explicit-destination']
const memoryEntries = ['MemoryParticipants', ...memoryCases.flatMap(name => [
  `MemoryParticipants/${name}`, `MemoryParticipants/${name}/denied`, `MemoryParticipants/${name}/allowed`,
])]
const providerCases = ['redacted-read', 'member-denied', 'exact', 'stale', 'unknown-instance', 'foreign-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'deleted', 'clear']
const providerEntries = ['OrgProviderMasks', ...providerCases.map(name => `OrgProviderMasks/${name}`)]
const collisionActors = ['alice@acme.test', 'dave@globex.test']
const collisionCases = ['read-list-denied', 'delete-denied', 'injection-denied', 'owner-resume']
const collisionEntries = ['SameSlugCrossOrgSessions', ...collisionActors.flatMap(actor => [
  `SameSlugCrossOrgSessions/${actor}`, ...collisionCases.map(name => `SameSlugCrossOrgSessions/${actor}/${name}`),
  `SameSlugCrossOrgSessions/owner-delete/${actor}`,
])]
const teamProviderCases = ['redacted-read', 'member-denied', 'exact', 'stale', 'unknown-instance', 'inherited-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'empty', 'clear']
const teamProviderEntries = ['TeamProviderMasks', ...['patch', 'replace'].flatMap(mode => [
  `TeamProviderMasks/${mode}`, ...teamProviderCases.map(name => `TeamProviderMasks/${mode}/${name}`),
])]
const platformProviderCases = ['redacted-read', 'org-owner-denied', 'team-admin-denied', 'member-denied', 'exact', 'stale', 'unknown-instance', 'foreign-org-instance', 'foreign-team-instance', 'unknown-field', 'empty-stored', 'mixed-invalid', 'replacement', 'omitted', 'deleted', 'clear']
const platformProviderEntries = ['PlatformProviderMasks', ...platformProviderCases.map(name => `PlatformProviderMasks/${name}`)]
const independentRoleEntries = ['IndependentRoleAuthority', ...['org-admin-settings', 'org-admin-providers', 'team-admin-providers', 'platform-admin-full-config'].map(name => `IndependentRoleAuthority/${name}`)]
const installCases = ['absent_globals_default_team_install_uninstall', 'absent_globals_team_install_uninstall', 'absent_globals_org_install_uninstall', 'absent_globals_member_denied', 'member_default_team_denied', 'member_team_denied', 'member_org_denied', 'member_platform_denied', 'owner_platform_denied', 'effective_scope_denied', 'unknown_scope_denied', 'foreign_team_denied', 'default_team_install_uninstall_preserves_globals', 'team_install_uninstall_preserves_globals', 'org_install_uninstall_preserves_globals', 'team_admin_default_team_install_uninstall_preserves_globals', 'team_admin_team_install_uninstall_preserves_globals', 'team_admin_org_denied', 'team_admin_platform_denied', 'team_admin_sibling_team_denied', 'platform_admin_platform_install_uninstall_scoped_effects']
const installEntries = ['MCPInstallIsolation', ...installCases.map(name => `MCPInstallIsolation/${name}`)]
const setupCases = ['sqlite_missing_token_no_effects', 'sqlite_wrong_token_no_effects', 'postgres_missing_token_no_effects', 'postgres_wrong_token_no_effects', 'sqlite_authorized_init_persists_local_user_org_team', 'sqlite_configured_retry_no_effects', 'postgres_configured_retry_no_effects']
const setupEntries = ['FirstRunSetup', ...['none', 'builtin'].flatMap(mode => [
  `FirstRunSetup/${mode}`, ...setupCases.map(name => `FirstRunSetup/${mode}/${name}`),
]), 'FirstRunSetup/builtin/builtin_restart_login_authorized_settings']
const legacyMCPCases = ['unauthenticated', 'invalid_jwt', 'member_default', 'member_team', 'member_org', 'member_platform', 'team_admin_org', 'team_admin_platform', 'team_admin_sibling', 'team_admin_foreign', 'org_owner_platform', 'platform_admin_team', 'platform_admin_org', 'platform_only_default', 'platform_only_org', 'effective_scope', 'unknown_scope']
const newHTTPMCPCases = [
  ...['org_admin_default', 'org_admin_team', 'org_admin_org', 'platform_only'].flatMap(scope =>
    ['create', 'update', 'toggle', 'inline_install', 'legacy_settings', 'legacy_patch', 'delete'].map(operation => `scoped_mutation/${scope}/${operation}`)),
  ...['org_admin_default', 'org_admin_team', 'org_admin_org', 'platform_only'].flatMap(scope =>
    ['modern', 'legacy'].map(route => `existing_refresh/${scope}/${route}`)),
  ...['org_admin_default', 'org_admin_team', 'org_admin_org'].map(scope => `org_admin_standard_install_uninstall/${scope}`),
  ...['create', 'update', 'delete', 'toggle', 'refresh', 'inline_install', 'legacy_settings', 'standard_install', 'standard_uninstall'].flatMap(operation =>
    ['unauthenticated', 'member_default', 'member_team', 'member_org', 'member_platform', 'org_admin_platform', 'team_admin_org', 'platform_only_default', 'effective_scope', 'unknown_scope', 'missing_default_team_store', 'missing_org_store', 'missing_team_store', 'missing_platform_store'].map(name => `remaining_rejected/${operation}/${name}`)),
  ...['legacy_patch', 'legacy_refresh'].map(operation => `remaining_rejected/${operation}/org_admin_platform`),
]
const fullConfigEntries = ['FullConfigMasksLocal', ...[
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
].map(name => `FullConfigMasksLocal/${name}`)]
const maskedMCPCases = ['team', 'org', 'platform'].flatMap(scope =>
  ['modern', 'legacy', 'inline'].flatMap(route => [
    'nonexistent_env', 'nonexistent_url', 'inherited_env', 'inherited_url', 'wrong_target_env', 'wrong_target_url', 'wrong_key', 'changed_masked_url', 'changed_destination', 'changed_command',
    ...(route === 'inline' ? ['existing_mask_rejected', 'replace_credentials', 'clear_credentials']
      : ['get_mask_roundtrip', 'omitted_env', 'empty_env', 'replace_credentials', 'clear_credentials', 'changed_args', 'changed_transport', 'omitted_env_changed_destination']),
    ...(route === 'modern' ? ['omitted_env_url', 'null_env_url'] : []),
  ].map(name => `masked_update/${scope}/${route}/${name}`)))
const maskedMCPEntries = maskedMCPCases.map(name => `LegacyMCPLocalAuthorization/${name}`)
const legacyMCPEntries = ['LegacyMCPLocalAuthorization', ...[
  ...maskedMCPCases,
  ...newHTTPMCPCases,
  ...['toggle', 'refresh'].flatMap(operation => legacyMCPCases.map(name => `${operation}/${name}`)),
  ...['team_default', 'team_explicit', 'org', 'platform_only'].map(name => `authorized_refresh/${name}`),
  ...['team_default', 'team_explicit', 'org', 'platform_only'].map(name => `authorized_missing_toggle/${name}`),
  ...['both', 'search_only', 'extract_only', 'neither', 'other_scope_matching'].map(name => `platform_settings_install_uninstall/${name}`),
  'default_team_does_not_mutate_platform_row',
  ...['team', 'platform'].flatMap(scope => ['PATCH', 'POST'].map(method => `missing_${scope}_store/${method}`)),
  ...['explicit_team', 'omitted_means_team', 'org_owner', 'platform_only'].map(name => `persisted_toggle/${name}`),
].map(name => `LegacyMCPLocalAuthorization/${name}`)]
const complete = [...fullConfigEntries, ...scopedMemoryEntries, ...legacyMCPEntries, ...independentRoleEntries, ...platformProviderEntries, ...installEntries, ...setupEntries, ...teamProviderEntries, ...collisionEntries, ...providerEntries, ...memoryEntries, 'ConfigAuthority', 'SessionOwnership', 'ArtifactOwnership', 'MCPAuthority', 'MemoryAuthority', 'ChannelOwnership',
  'SessionOwnership/bob@acme.test', 'SessionOwnership/carol@acme.test', 'SessionOwnership/eve@globex.test',
    'ConfiguredInit', 'ConfiguredInit/postgres', 'ConfiguredInit/postgres/alice@acme.test', 'ConfiguredInit/postgres/bob@acme.test',
    'ConfiguredInit/sqlite', 'ConfiguredInit/sqlite/alice@acme.test', 'ConfiguredInit/sqlite/bob@acme.test', 'SameTeamOwnerDenied',
    'ChannelProofLifecycle', 'ChannelProofLifecycle/direct-verification', 'ChannelProofLifecycle/expired-proof',
    'ChannelProofLifecycle/concurrent-proof-and-replay', 'ChannelProofLifecycle/linked-to-other-user']
  .flatMap(name => [event('run', name), event('pass', name)])
const packagePass = { Action: 'pass', Package }
const postgresFirstRunEntries = ['', '/setup', '/restart'].map(suffix => `TestE2E_SecurityPostgresFirstRunSetup${suffix}`)
for (const Test of postgresFirstRunEntries) test(`strict parser requires PostgreSQL first-run ${Test}`, () => {
  const pgEvents = postgresFirstRunEntries.filter(name => name !== Test)
    .flatMap(name => ['run', 'pass'].map(Action => ({ Action, Package, Test: name })))
  const result = parse([...complete, ...pgEvents, packagePass], true, { ASTONISH_E2E_BACKEND: 'postgres' })
  assert.equal(result.status, 1, result.stderr)
  assert.ok(result.stderr.includes(`${Package}/${Test}: required matrix entry missing`), result.stderr)
})

const metadata = {
  ASTONISH_SECURITY_TRACK: 'urgent', ASTONISH_E2E_BACKEND: 'sqlite',
  ASTONISH_E2E_SANDBOX_BACKEND: 'k8s', ASTONISH_SECURITY_REVISION: 'a'.repeat(40),
  ASTONISH_SECURITY_TEST_COMMAND: "go test -tags=e2e -count=1 -p 1 -timeout=15m -json -run '^TestE2E_SecurityUrgent' ./tests/e2e/chat_auth",
}
function parse(events, strict = true, overrides = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'security-parser-'))
  try {
    const file = join(dir, 'events.json')
    writeFileSync(file, events.map(e => typeof e === 'string' ? e : JSON.stringify(e)).join('\n'))
    const result = spawnSync(process.execPath, ['tests/scenarios/parse-run.mjs', file, ...(strict ? ['--strict-security'] : [])], {
      encoding: 'utf8', env: { ...process.env, ...metadata, ...overrides, ASTONISH_SCENARIO_OUTPUT: join(dir, 'report.json') },
    })
    if (result.status === 0) result.report = JSON.parse(readFileSync(join(dir, 'report.json'), 'utf8'))
    return result
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
for (const [name, events] of [
  ['zero matches', []],
  ['skip', [...complete, event('skip'), packagePass]],
  ['fail', [...complete, event('fail'), packagePass]],
  ['incomplete', [event('run')]],
  ['package failure', [...complete, { Action: 'fail', Package }]],
  ['missing package completion', complete],
  ['only one category', [event('run'), event('pass'), packagePass]],
  ['pass without run', [...complete.filter(e => e.Action !== 'run'), packagePass]],
  ['wrong package', [...complete, packagePass].map(e => ({ ...e, Package: 'wrong' }))],
  ['skipped subtest', [...complete, event('skip', 'MemoryAuthority/negative'), packagePass]],
  ['unfinished subtest', [...complete, event('run', 'MemoryAuthority/negative'), packagePass]],
  ['missing required subcase', [...complete.filter(e => !e.Test.endsWith('/bob@acme.test')), packagePass]],
  ['missing init actor subcase', [...complete.filter(e => !e.Test.endsWith('/postgres/alice@acme.test')), packagePass]],
  ['missing owner direction', [...complete.filter(e => !e.Test.endsWith('SameTeamOwnerDenied')), packagePass]],
  ['missing concurrent channel proof', [...complete.filter(e => !e.Test.endsWith('/concurrent-proof-and-replay')), packagePass]],
  ['missing expired channel proof', [...complete.filter(e => !e.Test.endsWith('/expired-proof')), packagePass]],
  ['missing direct channel rejection', [...complete.filter(e => !e.Test.endsWith('/direct-verification')), packagePass]],
  ['missing conflicting channel proof', [...complete.filter(e => !e.Test.endsWith('/linked-to-other-user')), packagePass]],
  ['unknown action', [...complete, { Action: 'unknown', Package }, packagePass]],
  ['missing package', [...complete, { Action: 'output', Output: 'text' }, packagePass]],
  ['malformed output', [...complete, { Action: 'output', Package, Output: 12 }, packagePass]],
  ['duplicate run', [...complete, event('run'), event('pass'), packagePass]],
  ['duplicate pass', [...complete, event('pass'), packagePass]],
  ['late test', [packagePass, ...complete]],
  ['invalid pause', [...complete, event('pause'), packagePass]],
  ['invalid continue', [...complete, event('cont'), packagePass]],
  ['test action without name', [...complete, { Action: 'run', Package }, packagePass]],
  ['pass before run', [event('pass'), ...complete, packagePass]],
  ['malformed JSON', [...complete, '{broken', packagePass]],
  ['null JSON', [...complete, null, packagePass]],
  ['invalid event shape', [...complete, { Action: 'run', Test: 42 }, packagePass]],
]) test(`strict parser rejects ${name}`, () => {
  const result = parse(events)
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, /Security acceptance FAILED/)
})
for (const [category, entries] of [
  ['full config', fullConfigEntries],
  ['legacy MCP', legacyMCPEntries],
  ['independent role', independentRoleEntries], ['platform provider', platformProviderEntries], ['MCP install', installEntries], ['first-run setup', setupEntries],
]) {
  for (const suffix of entries) test(`strict parser requires ${category} ${suffix}`, () => {
    const Test = `TestE2E_SecurityUrgent${suffix}`
    const result = parse([...complete.filter(e => e.Test !== Test), packagePass])
    assert.equal(result.status, 1, result.stderr)
    assert.ok(result.stderr.includes(`${Package}/${Test}: required matrix entry missing`), result.stderr)
  })
}
for (const suffix of teamProviderEntries) test(`strict parser requires team provider ${suffix}`, () => {
  const result = parse([...complete.filter(e => e.Test !== `TestE2E_SecurityUrgent${suffix}`), packagePass])
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, /required matrix entry missing/)
})
for (const suffix of collisionEntries) test(`strict parser requires collision ${suffix}`, () => {
  const result = parse([...complete.filter(e => e.Test !== `TestE2E_SecurityUrgent${suffix}`), packagePass])
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, /required matrix entry missing/)
})
for (const name of providerCases) test(`strict parser requires org provider ${name}`, () => {
  const result = parse([...complete.filter(e => !e.Test.endsWith(`OrgProviderMasks/${name}`)), packagePass])
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, /required matrix entry missing/)
})
for (const participant of memoryCases) {
  for (const outcome of ['denied', 'allowed']) test(`strict parser requires memory ${participant}/${outcome}`, () => {
    const suffix = `MemoryParticipants/${participant}/${outcome}`
    const result = parse([...complete.filter(e => !e.Test.endsWith(suffix)), packagePass])
    assert.equal(result.status, 1, result.stderr)
    assert.match(result.stderr, /required matrix entry missing/)
  })
}
test('strict parser accepts complete matrix and package success', () => {
  const result = parse([...complete, packagePass])
  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.report.scenarios['CHAT-103'], 'pass')
  assert.equal(result.report.scenarios['CHAT-104'], 'pass')
  assert.equal(result.report.scenarios['CHAT-105'], 'pass')
  assert.equal(result.report.scenarios['CHAT-106'], 'pass')
  assert.equal(result.report.scenarios['CHAT-107'], 'pass')
  assert.equal(result.report.scenarios['CHAT-109'], 'pass')
  assert.equal(result.report.scenarios['CHAT-110'], 'pass')
  assert.equal(result.report.scenarios['CHAT-111'], 'pass')
  assert.equal(result.report.scenarios['CHAT-112'], 'pass')
  assert.equal(result.report.scenarios['CHAT-113'], 'pass')
})
for (const [key, value] of [
  ['ASTONISH_SECURITY_TRACK', 'all'], ['ASTONISH_SECURITY_TRACK', 'unknown'],
  ['ASTONISH_SECURITY_TRACK', ''], ['ASTONISH_E2E_BACKEND', 'mysql'],
  ['ASTONISH_E2E_BACKEND', ''], ['ASTONISH_E2E_SANDBOX_BACKEND', 'openshell'],
  ['ASTONISH_SECURITY_REVISION', ''], ['ASTONISH_SECURITY_TEST_COMMAND', ''],
]) test(`strict parser rejects invalid metadata ${key}=${value}`, () => {
  const result = parse([...complete, packagePass], true, { [key]: value })
  assert.equal(result.status, 1, result.stderr)
})
for (const backend of ['postgres', 'sqlite']) test(`strict report identifies ${backend} execution`, () => {
  const extra = backend === 'postgres' ? postgresFirstRunEntries.flatMap(Test => ['run', 'pass'].map(Action => ({ Action, Package, Test }))) : []
  const command = backend === 'postgres'
    ? metadata.ASTONISH_SECURITY_TEST_COMMAND.replace("'^TestE2E_SecurityUrgent'", "'^(TestE2E_SecurityUrgent|TestE2E_SecurityPostgresFirstRunSetup$)'")
    : metadata.ASTONISH_SECURITY_TEST_COMMAND
  const result = parse([...complete, ...extra, packagePass], true, {
    ASTONISH_E2E_BACKEND: backend, ASTONISH_SECURITY_TEST_COMMAND: command,
  })
  assert.equal(result.status, 0, result.stderr)
  if (backend === 'postgres') assert.equal(result.report.scenarios['CHAT-115'], 'pass')
  assert.equal(result.report.test_command, command)
  const count = backend === 'postgres' ? 597 : 594
  assert.deepEqual(result.report.security, {
    track: 'urgent', storage: backend, runtime: 'k8s', revision: 'a'.repeat(40),
    counts: { executed: count, passed: count, required: count, packages: 1 },
  })
})
test('strict parser accepts valid parallel-test transitions', () => {
  const result = parse([...complete, event('run', 'MemoryAuthority/parallel'),
    event('pause', 'MemoryAuthority/parallel'), event('cont', 'MemoryAuthority/parallel'),
    event('pass', 'MemoryAuthority/parallel'), packagePass])
  assert.equal(result.status, 0, result.stderr)
})
for (const [track, storage, runtime, expected] of [
  ['urgent', 'both', 'k8s', 0], ['unknown', 'both', 'k8s', 1],
  ['all', 'both', 'k8s', 1], ['sandbox', 'both', 'k8s', 1],
  ['urgent', 'postgres', 'k8s', 1], ['urgent', 'sqlite', 'k8s', 1],
  ['urgent', 'both', 'docker', 1], ['urgent', 'both', 'openshell', 1],
]) test(`selection gate ${track}/${storage}/${runtime}`, () => {
  const result = spawnSync(process.execPath, ['tests/scenarios/parse-run.mjs', '--security-selection'], {
    encoding: 'utf8', env: { ...process.env, ASTONISH_SECURITY_TRACK: track,
      ASTONISH_E2E_BACKEND: storage, ASTONISH_E2E_SANDBOX_BACKEND: runtime },
  })
  assert.equal(result.status, expected, result.stderr)
  if (expected !== 0) {
    // Exercise Make's actual wiring; invalid selection must stop before Go,
    // kubectl, database access, or cleanup (no acknowledgement is supplied).
    const make = spawnSync('make', ['test-security-preflight', `SECURITY_TRACK=${track}`,
      `SECURITY_STORAGE=${storage}`, `SECURITY_RUNTIME=${runtime}`, 'ASTONISH_E2E_DEDICATED='], { encoding: 'utf8' })
    assert.equal(make.status, 2, make.stdout + make.stderr)
    assert.match(make.stderr, /Security acceptance FAILED/)
    assert.doesNotMatch(make.stdout + make.stderr, /BLOCKED before cleanup/)
  }
})
test('new security catalog entries and documented manifest count stay aligned', () => {
  const catalog = readFileSync('tests/scenarios/chat.yaml', 'utf8')
  for (const [id, name] of [['CHAT-108', 'PlatformProviderMasks'], ['CHAT-109', 'FirstRunSetup'], ['CHAT-110', 'MCPInstallIsolation'], ['CHAT-111', 'IndependentRoleAuthority'], ['CHAT-112', 'LegacyMCPLocalAuthorization'], ['CHAT-113', 'ScopedMemoryCleanup'], ['CHAT-114', 'FullConfigMasksLocal']]) {
    const entries = catalog.split(/(?=^  - id: )/m).filter(entry => entry.startsWith(`  - id: ${id}\n`))
    assert.equal(entries.length, 1, `${id} must appear exactly once`)
    assert.match(entries[0], /status: covered/)
    assert.ok(entries[0].includes(`- TestE2E_SecurityUrgent${name}`))
  }
  assert.deepEqual([platformProviderEntries.length, installEntries.length, setupEntries.length, independentRoleEntries.length], [17, 22, 18, 5])
  assert.equal(newHTTPMCPCases.length, 167)
  assert.deepEqual(['scoped_mutation', 'existing_refresh', 'org_admin_standard_install_uninstall', 'remaining_rejected']
    .map(group => newHTTPMCPCases.filter(name => name.startsWith(`${group}/`)).length), [28, 8, 3, 128])
  assert.equal(legacyMCPEntries.length, 377)
  assert.equal(maskedMCPCases.length, 153)
  assert.deepEqual(['modern', 'legacy', 'inline'].map(route => maskedMCPCases.filter(name => name.includes(`/${route}/`)).length), [60, 54, 39])
  assert.equal(fullConfigEntries.length, 52)
  assert.equal(fullConfigEntries.filter(name => !fullConfigEntries.some(other => other.startsWith(`${name}/`))).length, 40)
  assert.equal(complete.length / 2, 594)
  assert.equal(new Set(complete.map(e => e.Test)).size, 594)
  // Slash-named t.Run leaves do not emit intermediate group or scope parents.
  for (const name of [...newHTTPMCPCases, ...maskedMCPCases]) {
    const parts = name.split('/')
    for (let i = 1; i < parts.length; i++) {
      assert.ok(!legacyMCPEntries.includes(`LegacyMCPLocalAuthorization/${parts.slice(0, i).join('/')}`))
    }
  }
  assert.match(readFileSync('docs/architecture/testing.md', 'utf8'), /requires \*\*594 entries\*\*/)
})

test('local first-run process proof stays outside urgent matrix claims', () => {
  const docs = readFileSync('docs/architecture/testing.md', 'utf8')
  const catalog = readFileSync('tests/scenarios/chat.yaml', 'utf8')
  assert.ok(!complete.some(e => e.Test.includes('FirstRunSetupProcessContinuity')))
  assert.match(docs, /TestE2E_FirstRunSetupProcessContinuity/)
  assert.match(docs, /local HTTP proof/)
  assert.match(docs, /not selected by the `\^TestE2E_SecurityUrgent` prefix/)
  assert.match(docs, /not a real daemon binary/)
  assert.match(docs, /no PostgreSQL setup proof/)
  assert.match(catalog, /Twenty-one cases exercise both install and uninstall/)
  assert.match(catalog, /preserve that absence/)
})

for (const backend of ['postgres', 'sqlite']) test(`strict parser requires legacy MCP coverage in ${backend} leg`, () => {
  const result = parse([...complete.filter(e => !e.Test.startsWith('TestE2E_SecurityUrgentLegacyMCPLocalAuthorization')), packagePass], true, { ASTONISH_E2E_BACKEND: backend })
  assert.equal(result.status, 1, result.stderr)
  for (const suffix of legacyMCPEntries) {
    assert.ok(result.stderr.includes(`${Package}/TestE2E_SecurityUrgent${suffix}: required matrix entry missing`), result.stderr)
  }
})

test('legacy MCP metadata distinguishes local SQLite HTTP from live matrix acceptance', () => {
  const docs = readFileSync('docs/architecture/testing.md', 'utf8')
  const catalog = readFileSync('tests/scenarios/chat.yaml', 'utf8')
  assert.match(docs, /local SQLite HTTP\/JWT proof/)
  assert.match(docs, /temporary SQLite regardless of `ASTONISH_E2E_BACKEND`/)
  assert.match(docs, /report does not make it PostgreSQL coverage/)
  assert.match(docs, /\*\*377 entries\*\* \(parent \+ 376 subcases/)
  assert.match(docs, /28 scoped mutations \+ 8 existing-row refreshes \+ 3 install\/uninstall pairs \+ 128 rejections/)
  assert.match(docs, /inherited PreWarm and Studio chat initialization hooks/)
  assert.match(docs, /e2e-no-discovery/)
  assert.match(catalog, /These 376 subcases/)
  assert.match(catalog, /167 additional outcomes/)
  assert.match(catalog, /invalid local sandbox backend stops discovery before external access/)
  assert.match(catalog, /Inherited chat initialization and credential-store hooks are cleared/)
  assert.match(catalog, /Local coverage is not live PostgreSQL\/SQLite urgent-matrix acceptance/)
})

for (const backend of ['postgres', 'sqlite']) {
  for (const suffix of scopedMemoryEntries) test(`strict parser requires scoped memory ${suffix} in ${backend} leg`, () => {
    const Test = `TestE2E_SecurityUrgent${suffix}`
    const result = parse([...complete.filter(e => e.Test !== Test), packagePass], true, { ASTONISH_E2E_BACKEND: backend })
    assert.equal(result.status, 1, result.stderr)
    assert.ok(result.stderr.includes(`${Package}/${Test}: required matrix entry missing`), result.stderr)
  })
}

test('scoped memory metadata distinguishes ID alias injection from SQL collisions', () => {
  const docs = readFileSync('docs/architecture/testing.md', 'utf8')
  const catalog = readFileSync('tests/scenarios/chat.yaml', 'utf8')
  assert.equal(scopedMemoryEntries.length, 9)
  assert.ok(scopedMemoryEntries.slice(1).every(name => name.split('/').length === 4))
  for (const text of [docs, catalog]) {
    assert.match(text, /TestE2E_SecurityUrgentScopedMemoryCleanup/)
    assert.match(text, /ID aliasing/)
    assert.match(text, /not a SQL-level collision/)
    assert.match(text, /bounded 500/)
  }
  assert.match(docs, /TestE2E_Memory_LocalDeletionFailures/)
  assert.ok(!complete.some(e => e.Test.includes('LocalDeletionFailures')))
})

test('masked HTTP coverage retains real parents and bounded storage-failure caveats', () => {
  for (const suffix of fullConfigEntries) {
    const parts = suffix.split('/')
    for (let i = 1; i < parts.length; i++) assert.ok(fullConfigEntries.includes(parts.slice(0, i).join('/')))
  }
  for (const name of ['nonexistent_env', 'nonexistent_url', 'inherited_env', 'inherited_url']) {
    assert.ok(maskedMCPEntries.includes(`LegacyMCPLocalAuthorization/masked_update/team/modern/${name}`))
  }
  for (const path of ['docs/architecture/testing.md', 'tests/scenarios/chat.yaml']) {
    const text = readFileSync(path, 'utf8')
    assert.match(text, /unresolved notfound inconsistency/i)
    assert.match(text, /bounded 500 storage failures/)
    assert.match(text, /no 5xx is proof of authorization/)
    assert.match(text, /40 leaves and 12 real/)
    assert.match(text, /CHAT-114/)
    assert.match(text, /TestE2E_SecurityUrgentFullConfigMasksLocal/)
  }
})

// Opt-in comparison consumes real local-only go test -json output, never fabricates
// missing live coverage or treats a partial local run as matrix acceptance.
if (process.env.ASTONISH_MASK_HTTP_EVENTS) test('observed local masked HTTP hierarchy matches required entries', () => {
  const events = readFileSync(process.env.ASTONISH_MASK_HTTP_EVENTS, 'utf8').trim().split('\n').map(JSON.parse)
  assert.ok(!events.some(e => e.Action === 'skip' || e.Action === 'fail'))
  const expected = [...fullConfigEntries, ...legacyMCPEntries].map(suffix => `TestE2E_SecurityUrgent${suffix}`).sort()
  for (const Action of ['run', 'pass']) {
    assert.deepEqual(events.filter(e => e.Test && e.Action === Action).map(e => e.Test).sort(), expected)
  }
  const result = parse(events)
  assert.equal(result.status, 1, 'local-only coverage must not pass the full matrix')
  for (const Test of expected) assert.ok(!result.stderr.includes(`${Package}/${Test}: required matrix entry missing`), result.stderr)
  assert.match(result.stderr, /ConfigAuthority: required matrix entry missing/)
})

// Exercise the actual Make recipes in a credential-free temporary checkout.
// Go/Kubernetes/git are synthetic executables; only the parser is real. These
// tests prove orchestration and failure propagation, NOT infrastructure acceptance.
for (const failure of ['', 'preflight', 'go-postgres', 'go-sqlite', 'parser-postgres', 'parser-sqlite']) {
  test(`actual Make security orchestration ${failure || 'both-backends'}`, () => {
    const dir = mkdtempSync(join(tmpdir(), 'security-make-'))
    try {
      mkdirSync(join(dir, 'bin'))
      mkdirSync(join(dir, 'tests/scenarios'), { recursive: true })
      writeFileSync(join(dir, 'Makefile'), readFileSync('Makefile'))
      writeFileSync(join(dir, 'tests/scenarios/parse-run.mjs'), readFileSync('tests/scenarios/parse-run.mjs'))
      for (const backend of ['postgres', 'sqlite']) {
        const extra = backend === 'postgres' ? postgresFirstRunEntries.flatMap(Test =>
          ['run', 'pass'].map(Action => ({ Action, Package, Test }))) : []
        writeFileSync(join(dir, `${backend}.json`), [...complete, ...extra, packagePass].map(JSON.stringify).join('\n'))
      }
      const shim = `#!${process.execPath}
const fs = require('node:fs');
const cp = require('node:child_process');
const path = require('node:path');
const tool = path.basename(process.argv[1]);
const args = process.argv.slice(2);
const backend = process.env.ASTONISH_E2E_BACKEND;
const fail = process.env.FIXTURE_FAILURE;
fs.appendFileSync('calls.jsonl', JSON.stringify({tool,args,backend,acceptance:process.env.ASTONISH_SECURITY_ACCEPTANCE,command:process.env.ASTONISH_SECURITY_TEST_COMMAND})+'\\n');
if (tool === 'node') {
  if (args.includes('--strict-security') && fail === 'parser-'+backend) process.exit(23);
  const result = cp.spawnSync(${JSON.stringify(process.execPath)}, args, {stdio:'inherit'});
  process.exit(result.status ?? 99);
}
if (tool === 'go') {
  if (!args.includes('-json')) {
    if (fail === 'preflight') { console.error('BLOCKED synthetic preflight'); process.exit(1); }
    process.exit(0);
  }
  process.stdout.write(fs.readFileSync(backend+'.json'));
  process.exit(fail === 'go-'+backend ? 19 : 0);
}
if (tool === 'git') { if (args[0] === 'rev-parse') console.log('a'.repeat(40)); process.exit(0); }
if (tool === 'kubectl') { if (args.includes('pvc')) process.stdout.write('Bound'); process.exit(0); }
if (tool === 'mktemp') { const file = path.join(process.cwd(), backend+'.results'); fs.writeFileSync(file,''); console.log(file); process.exit(0); }
process.exit(98);
`
      for (const tool of ['node', 'go', 'git', 'kubectl', 'mktemp']) {
        writeFileSync(join(dir, 'bin', tool), shim, { mode: 0o700 })
      }
      const result = spawnSync('/usr/bin/make', ['test-security', 'SECURITY_TRACK=urgent',
        'SECURITY_STORAGE=both', 'SECURITY_RUNTIME=k8s', 'DEV_ARCH=synthetic'], {
        cwd: dir, encoding: 'utf8', timeout: 20000,
        env: { PATH: `${join(dir, 'bin')}:/usr/bin:/bin`, HOME: dir, TMPDIR: dir,
          FIXTURE_FAILURE: failure, ASTONISH_E2E_CONTROL_PLANE_NAMESPACE: 'synthetic-control',
          ASTONISH_E2E_SANDBOX_NAMESPACE: 'synthetic-sandbox' },
      })
      assert.equal(result.status, failure ? 2 : 0, result.stdout + result.stderr)
      const calls = readFileSync(join(dir, 'calls.jsonl'), 'utf8').trim().split('\n').map(JSON.parse)
      const preflights = calls.filter(c => c.tool === 'go' && !c.args.includes('-json'))
      assert.deepEqual(preflights.map(c => c.backend), failure === 'preflight' ? ['postgres'] : ['postgres', 'sqlite'])
      for (const c of preflights) assert.ok(c.args.includes('^TestSecurityPreflightEnvironment$'))
      if (failure === 'preflight') {
        assert.ok(!calls.some(c => c.tool === 'kubectl' || c.args.includes('-json')))
        return
      }
      assert.ok(calls.indexOf(preflights[1]) < calls.findIndex(c => c.tool === 'kubectl'))
      const runs = calls.filter(c => c.tool === 'go' && c.args.includes('-json'))
      const backends = failure.endsWith('-postgres') ? ['postgres'] : ['postgres', 'sqlite']
      assert.deepEqual(runs.map(c => c.backend), backends)
      for (const c of runs) {
        const pattern = c.backend === 'postgres'
          ? '^(TestE2E_SecurityUrgent|TestE2E_SecurityPostgresFirstRunSetup$)' : '^TestE2E_SecurityUrgent'
        assert.equal(c.args[c.args.indexOf('-run') + 1], pattern)
        assert.equal(c.acceptance, '1')
        assert.equal(c.args.at(-1), './tests/e2e/chat_auth')
        assert.ok(c.command.includes('go test -tags=e2e'))
      }
      const parsers = calls.filter(c => c.tool === 'node' && c.args.includes('--strict-security'))
      assert.deepEqual(parsers.map(c => c.backend), backends)
      for (const backend of backends) {
        const reportPath = join(dir, `${backend}.results.report.json`)
        if (failure === `parser-${backend}`) { assert.equal(existsSync(reportPath), false); continue }
        const report = JSON.parse(readFileSync(reportPath, 'utf8'))
        assert.equal(report.security.storage, backend)
        assert.equal(report.security.counts.required, backend === 'postgres' ? 597 : 594)
        assert.equal(report.test_command, runs.find(c => c.backend === backend).command)
      }
    } finally { rmSync(dir, { recursive: true, force: true }) }
  })
}

test('documented first-run safety selector covers actual isolation tests', () => {
  const docs = readFileSync('docs/architecture/testing.md', 'utf8')
  const line = docs.split('\n').find(line => line.includes("-run '^TestPostgresFirstRun("))
  assert.ok(line, 'first-run safety command missing')
  const pattern = new RegExp(line.match(/-run '([^']+)'/)[1])
  for (const suffix of ['GateBeforeEffects', 'IsolationBeforeEffects', 'SymlinkIsolationBeforeEffects', 'RejectionsLocal']) {
    assert.ok(pattern.test(`TestPostgresFirstRun${suffix}`), `documented command omits ${suffix}`)
  }
})

test('ordinary parser retains permissive skip behavior', () => {
  const result = parse([event('skip')], false)
  assert.equal(result.status, 0, result.stderr)
})
