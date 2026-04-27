/**
 * Task Delegation Scenario Tests (D1-D7)
 *
 * Tests the delegate_tasks flow: delegation start, task lifecycle
 * (start → tool calls → text → complete/failed), retry, and the
 * TaskPlanPanel rendering.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import simpleDelegation from '../fixtures/scenarios/delegation/simple-delegation.json'
import multiTaskParallel from '../fixtures/scenarios/delegation/multi-task-parallel.json'
import taskFailure from '../fixtures/scenarios/delegation/task-failure.json'

// Mock react-markdown
vi.mock('react-markdown', () => ({
  default: ({ children }: { children: string }) => <span data-testid="markdown">{children}</span>,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))
vi.mock('../../components/HomePage', () => ({
  default: () => <div data-testid="home-page">HomePage</div>,
}))
vi.mock('../../components/chat/FleetStartDialog', () => ({ default: () => null }))
vi.mock('../../components/chat/FleetTemplatePicker', () => ({ default: () => null }))
vi.mock('../../components/chat/MermaidBlock', () => ({
  default: ({ chart }: { chart: string }) => <pre data-testid="mermaid">{chart}</pre>,
}))

describe('Task Delegation Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('D1: Simple Delegation', () => {
    it('renders task panel with task name', async () => {
      result = renderChat({
        scenarioEvents: simpleDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Research competitors')

      // Task name should appear in the delegation panel
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Research competitors')
      }, { timeout: 5000 })
    })

    it('shows final agent text after delegation completes', async () => {
      result = renderChat({
        scenarioEvents: simpleDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Research competitors')

      // Final summary text
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('top competitors in the market')
      }, { timeout: 10000 })
    })

    it('shows task duration on completion', async () => {
      result = renderChat({
        scenarioEvents: simpleDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Research competitors')

      // Duration should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('3.2s')
      }, { timeout: 5000 })
    })
  })

  describe('D2: Multi-Task Parallel Delegation', () => {
    it('renders all three task names', async () => {
      result = renderChat({
        scenarioEvents: multiTaskParallel.events as FixtureEvent[],
      })

      await result.sendMessage('Research from multiple angles')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Market research')
        expect(text).toContain('Competitor analysis')
        expect(text).toContain('Write summary')
      }, { timeout: 5000 })
    })

    it('shows all tasks completed with durations', async () => {
      result = renderChat({
        scenarioEvents: multiTaskParallel.events as FixtureEvent[],
      })

      await result.sendMessage('Research')

      // All durations should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('5.1s')
        expect(text).toContain('4.3s')
        expect(text).toContain('2.8s')
      }, { timeout: 5000 })
    })
  })

  describe('D4: Task Failure', () => {
    it('renders failed task with error message', async () => {
      result = renderChat({
        scenarioEvents: taskFailure.events as FixtureEvent[],
      })

      await result.sendMessage('Deploy changes')

      // Task name
      await waitFor(() => {
        expect(screen.getByText(/Deploy to staging/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Error text in the delegation panel or agent message
      await waitFor(() => {
        expect(screen.getByText(/Connection refused/i)).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('shows agent explanation after delegation failure', async () => {
      result = renderChat({
        scenarioEvents: taskFailure.events as FixtureEvent[],
      })

      await result.sendMessage('Deploy')

      await waitFor(() => {
        expect(screen.getByText(/deployment failed/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })
})
