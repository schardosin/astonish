import { describe, expect, it } from 'vitest'

import {
  addAgentToFleetConfig,
  createDefaultFleetAgent,
  removeAgentFromFleetConfig,
  slugifyAgentKey,
} from '../fleetUtils'
import type { FleetPlanData } from '../fleetUtils'

const baseConfig: FleetPlanData = {
  name: 'Test',
  agents: {
    po: { name: 'PO', identity: 'PO', behaviors: 'Lead', tools: true },
    dev: { name: 'Dev', identity: 'Dev', behaviors: 'Code', tools: true },
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
})
