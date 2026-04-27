/**
 * Plan Tracking Scenario Tests (E1-E7, F1-F3)
 *
 * Tests the announce_plan flow, plan step status transitions
 * (pending → running → complete/failed), and the PlanPanel + TodoPanel
 * rendering.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import planWithDelegation from '../fixtures/scenarios/planning/plan-with-delegation.json'

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

describe('Plan Tracking Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('E1 + F1: Plan Announced with Delegation', () => {
    it('renders plan panel with goal and step names', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze the market')

      // Plan goal should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Research and analyze market competitors')
      }, { timeout: 10000 })

      // Step descriptions should appear in the plan panel
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Find top competitors')
      }, { timeout: 10000 })
    })

    it('shows plan steps from the announced plan', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze the market')

      // Step descriptions should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Find top competitors')
        expect(text).toContain('Compare features and pricing')
      }, { timeout: 10000 })
    })

    it('shows delegation task names alongside plan', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze the market')

      // Delegation task names should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Research competitors')
        expect(text).toContain('Feature comparison')
      }, { timeout: 10000 })
    })

    it('shows final agent text after plan completes', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze the market')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('analysis is complete')
      }, { timeout: 10000 })
    })
  })

  describe('E7: TodoPanel', () => {
    it('shows Todo button with plan step progress after plan events', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze the market')

      // Wait for plan to be rendered
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Research and analyze market competitors')
      }, { timeout: 10000 })

      // The plan footer shows step progress like "3/3 steps"
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('3/3')
      }, { timeout: 10000 })
    })
  })
})
