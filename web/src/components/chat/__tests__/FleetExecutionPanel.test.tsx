import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import FleetExecutionPanel from '../FleetExecutionPanel'
import type { FleetExecutionMessage } from '../chatTypes'

describe('FleetExecutionPanel', () => {
  it('keeps the single-column timeline without lane metadata', () => {
    const data: FleetExecutionMessage = {
      type: 'fleet_execution',
      status: 'running',
      currentPhase: 'architect',
      currentAgent: 'architect',
      maxParallelAgents: 1,
      events: [
        { type: 'phase_start', phase: 'architect', agent: 'architect', timestamp: Date.now() },
        { type: 'text', phase: 'architect', agent: 'architect', text: 'Designing' },
      ],
    }

    render(<FleetExecutionPanel data={data} />)

    expect(screen.getByText('architect')).toBeInTheDocument()
    expect(screen.queryByText('Lane 1')).not.toBeInTheDocument()
  })

  it('renders multi-column lanes and task strip for lane events', () => {
    const data: FleetExecutionMessage = {
      type: 'fleet_execution',
      status: 'running',
      currentPhase: 'qa',
      currentAgent: 'qa',
      maxParallelAgents: 2,
      events: [
        { type: 'phase_start', phase: 'qa', agent: 'qa', lane_index: 0, timestamp: Date.now() },
        { type: 'phase_start', phase: 'e2e', agent: 'e2e', lane_index: 1, timestamp: Date.now() },
        { type: 'fleet_task_posted', task_id: 't1', title: 'Run smoke tests', required_capabilities: ['code.test'] },
      ],
    }

    render(<FleetExecutionPanel data={data} />)

    expect(screen.getByText('Coordinator')).toBeInTheDocument()
    expect(screen.getByText('Lane 1')).toBeInTheDocument()
    expect(screen.getByText('Lane 2')).toBeInTheDocument()
    expect(screen.getByText('Tasks')).toBeInTheDocument()
    expect(screen.getByText('Run smoke tests')).toBeInTheDocument()
  })
})
