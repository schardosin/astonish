import { describe, expect, it } from 'vitest'

import {
  capabilityMapFromKeys,
  enabledCapabilityKeys,
  extraCapabilityKeys,
  FLEET_AGENT_MODES,
  FLEET_CAPABILITY_GROUPS,
  FLEET_CAPABILITY_REGISTRY,
  isValidCapabilityKey,
  normalizeCapabilityKey,
} from '../fleetConstants'

describe('fleetConstants', () => {
  it('uses a domain-neutral capability registry', () => {
    expect(FLEET_CAPABILITY_REGISTRY).toEqual(expect.arrayContaining([
      'research',
      'writing',
      'review',
      'publishing',
      'planning',
      'supervisor',
    ]))
    expect(FLEET_CAPABILITY_REGISTRY).not.toEqual(expect.arrayContaining([
      'code.write',
      'design.architecture',
    ]))
    expect(FLEET_CAPABILITY_GROUPS.length).toBeGreaterThan(3)
    expect(FLEET_AGENT_MODES).toEqual(['agentic', 'simple'])
  })

  it('converts capability maps to and from key lists', () => {
    const caps = { writing: true, review: true, unused: false }
    expect(enabledCapabilityKeys(caps)).toEqual(['writing', 'review'])
    expect(capabilityMapFromKeys(['validation'])).toEqual({ validation: true })
    expect(extraCapabilityKeys({ 'code.write': true }, FLEET_CAPABILITY_REGISTRY)).toEqual(['code.write'])
    expect(extraCapabilityKeys({ research: true }, FLEET_CAPABILITY_REGISTRY)).toEqual([])
  })

  it('normalizes and validates custom capability keys', () => {
    expect(normalizeCapabilityKey('Genetics Analysis')).toBe('genetics-analysis')
    expect(normalizeCapabilityKey('pharmacology.research')).toBe('pharmacology.research')
    expect(isValidCapabilityKey('genetics.analysis')).toBe(true)
    expect(isValidCapabilityKey('-bad')).toBe(false)
  })
})
