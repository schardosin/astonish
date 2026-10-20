import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'

const Package = 'github.com/SAP/astonish/tests/e2e/chat_auth'
const event = (Action, suffix = 'ConfigAuthority') => ({ Action, Package, Test: `TestE2E_SecurityUrgent${suffix}` })
const complete = ['ConfigAuthority', 'SessionOwnership', 'ArtifactOwnership', 'MCPAuthority', 'MemoryAuthority', 'ChannelOwnership']
  .flatMap(name => [event('run', name), event('pass', name)])
const packagePass = { Action: 'pass', Package }
function parse(events, strict = true) {
  const dir = mkdtempSync(join(tmpdir(), 'security-parser-'))
  try {
    const file = join(dir, 'events.json')
    writeFileSync(file, events.map(e => typeof e === 'string' ? e : JSON.stringify(e)).join('\n'))
    return spawnSync(process.execPath, ['tests/scenarios/parse-run.mjs', file, ...(strict ? ['--strict-security'] : [])], {
      encoding: 'utf8', env: { ...process.env, ASTONISH_SCENARIO_OUTPUT: join(dir, 'report.json') },
    })
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
  ['malformed JSON', [...complete, '{broken', packagePass]],
  ['null JSON', [...complete, null, packagePass]],
  ['invalid event shape', [...complete, { Action: 'run', Test: 42 }, packagePass]],
]) test(`strict parser rejects ${name}`, () => {
  const result = parse(events)
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, /Security acceptance FAILED/)
})
test('strict parser accepts complete matrix and package success', () => {
  const result = parse([...complete, packagePass])
  assert.equal(result.status, 0, result.stderr)
})
test('ordinary parser retains permissive skip behavior', () => {
  const result = parse([event('skip')], false)
  assert.equal(result.status, 0, result.stderr)
})
