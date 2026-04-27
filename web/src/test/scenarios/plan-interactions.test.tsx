/**
 * Plan Interaction Scenario Tests (E6, E2-E3, D5)
 *
 * Tests plan step status transitions, partial completion display,
 * step descriptions, and task retry flow.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import planStepTransitions from '../fixtures/scenarios/planning/plan-step-transitions.json'
import taskRetry from '../fixtures/scenarios/delegation/task-retry.json'

// Mock react-markdown and remark-gfm to avoid ESM issues in jsdom
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

describe('Plan Interaction Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('E6: Plan Partial Completion', () => {
    it('shows Partial status when a step fails', async () => {
      result = renderChat({
        scenarioEvents: planStepTransitions.events as FixtureEvent[],
      })

      await result.sendMessage('Build it')

      // Wait for the plan goal to appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Build feature X')
      }, { timeout: 10000 })

      // Should show "Partial" status badge (because one step failed)
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Partial')
      }, { timeout: 10000 })

      // Should show "2/3" (2 complete out of 3 steps)
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('2/3')
      }, { timeout: 10000 })
    })
  })

  describe('E2-E3: Plan Step Status', () => {
    it('shows step descriptions in the plan panel', async () => {
      result = renderChat({
        scenarioEvents: planStepTransitions.events as FixtureEvent[],
      })

      await result.sendMessage('Build it')

      // Wait for step descriptions to appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Design the architecture')
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Write the code')
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Run tests')
      }, { timeout: 10000 })
    })
  })

  describe('D5: Task Retry', () => {
    it('shows task completing after retry', async () => {
      result = renderChat({
        scenarioEvents: taskRetry.events as FixtureEvent[],
      })

      await result.sendMessage('Call API')

      // Wait for task name to appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('API Call')
      }, { timeout: 10000 })

      // Should show the final duration after retry
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('2.0s')
      }, { timeout: 10000 })

      // Should show the final text about successful retry
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('API call succeeded after retry')
      }, { timeout: 10000 })
    })
  })
})
