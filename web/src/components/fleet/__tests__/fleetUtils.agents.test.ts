import { describe, expect, it } from 'vitest'

import {
  addAgentToFleetConfig,
  createDefaultFleetAgent,
  getAgentColor,
  removeAgentFromFleetConfig,
  renameAgentInFleetConfig,
  slugifyAgentKey,
} from '../fleetUtils'
import type { FleetPlanData } from '../fleetUtils'

const baseConfig: FleetPlanData = {
  name: 'Test',
  agents: {
    po: {
      name: 'PO',
      identity: 'PO',
      behaviors: 'Lead',
      tools: true,
      memory: { receives: ['dev'] },
    },
    dev: {
      name: 'Dev',
      identity: 'Dev',
      behaviors: 'Code',
      tools: true,
      memory: { receives: ['po'] },
    },
  },
  communication: {
    flow: [
      { role: 'po', entry_point: true, talks_to: ['dev', 'customer'] },
      { role: 'dev', talks_to: ['po', 'customer'] },
    ],
  },
}

describe('fleet agent helpers', () => {
  it('slugifies agent keys', () => {
    expect(slugifyAgentKey('Product Owner')).toBe('product-owner')
    expect(slugifyAgentKey(' QA_Engineer ')).toBe('qa-engineer')
  })

  it('creates a minimal valid default agent with task claims', () => {
    expect(createDefaultFleetAgent('Architect')).toEqual({
      name: 'Architect',
      identity: 'You are Architect.',
      behaviors: 'Follow the user instructions carefully and collaborate with other agents when needed.',
      tools: true,
      capabilities: { general: true },
      task_policy: { claims: ['general'] },
    })
  })

  it('adds an agent and appends a communication flow node', () => {
    const next = addAgentToFleetConfig(baseConfig, 'qa', createDefaultFleetAgent('QA'))
    expect(next.agents?.qa?.name).toBe('QA')
    expect(next.communication?.flow).toEqual(expect.arrayContaining([
      expect.objectContaining({ role: 'qa', talks_to: ['customer'], entry_point: false }),
    ]))
  })

  it('removes an agent and cleans talks_to references', () => {
    const next = removeAgentFromFleetConfig(baseConfig, 'dev')
    expect(next.agents?.dev).toBeUndefined()
    expect(next.agents?.po).toBeDefined()
    expect(next.communication?.flow).toEqual([
      { role: 'po', entry_point: true, talks_to: ['customer'] },
    ])
  })

  it('promotes another agent to entry_point when deleting the entry agent', () => {
    const next = removeAgentFromFleetConfig(baseConfig, 'po')
    expect(next.agents?.po).toBeUndefined()
    expect(next.communication?.flow?.[0]).toEqual({
      role: 'dev',
      talks_to: ['customer'],
      entry_point: true,
    })
  })

  it('renames an agent key and rewrites flow + memory.receives', () => {
    const next = renameAgentInFleetConfig(baseConfig, 'dev', 'engineer')
    expect(next.agents?.dev).toBeUndefined()
    expect(next.agents?.engineer?.name).toBe('Dev')
    expect(next.agents?.po?.memory?.receives).toEqual(['engineer'])
    expect(next.agents?.engineer?.memory?.receives).toEqual(['po'])
    expect(next.communication?.flow).toEqual([
      { role: 'po', entry_point: true, talks_to: ['engineer', 'customer'] },
      { role: 'engineer', talks_to: ['po', 'customer'] },
    ])
  })

  it('refuses to rename onto an existing key', () => {
    expect(() => renameAgentInFleetConfig(baseConfig, 'dev', 'po')).toThrow(/already exists/)
  })

  it('returns known role colors and distinct palette colors for unknown keys', () => {
    expect(getAgentColor('po').text).toBe('#60a5fa')
    expect(getAgentColor('e2e').text).toBe('#2dd4bf')
    expect(getAgentColor('researcher').text).toBe(getAgentColor('researcher').text)
    const a = getAgentColor('researcher')
    const b = getAgentColor('reviewer')
    expect(a.label).toBe('researcher')
    expect(b.label).toBe('reviewer')
    expect(a.text).not.toBe(b.text)
  })
})
