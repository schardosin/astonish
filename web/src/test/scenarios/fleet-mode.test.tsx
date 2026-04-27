/**
 * Fleet Mode Scenario Tests (O5, O9)
 *
 * Tests fleet execution progress rendering and fleet redirect event handling.
 * Fleet progress events flow through the regular sendMessage SSE handler
 * and create fleet_execution message types rendered by FleetExecutionPanel.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import fleetExecutionProgress from '../fixtures/scenarios/fleet/fleet-execution-progress.json'
import fleetRedirect from '../fixtures/scenarios/fleet/fleet-redirect.json'

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

describe('Fleet Mode Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('O5: Fleet Execution Progress', () => {
    it('renders fleet execution panel with phase names', async () => {
      result = renderChat({
        scenarioEvents: fleetExecutionProgress.events as FixtureEvent[],
      })

      await result.sendMessage('Start fleet')

      // The fleet_progress events create a fleet_execution message type
      // which is rendered by FleetExecutionPanel. Phase names should appear.
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Planning')
        expect(text).toContain('Implementation')
      }, { timeout: 10000 })
    })
  })

  describe('O9: Fleet Redirect', () => {
    it('processes fleet redirect without crashing', async () => {
      result = renderChat({
        scenarioEvents: fleetRedirect.events as FixtureEvent[],
      })

      // The fleet_redirect event sets isStreaming=false immediately,
      // so sendMessage will detect the placeholder going back to idle quickly.
      // FleetStartDialog is mocked to null, so it won't render anything visible.
      await result.sendMessage('/fleet Build a scraper')

      // Verify the stream completed without errors.
      // After fleet_redirect, isStreaming is set to false and the done event
      // is still processed. Just verify no crash occurred.
      await waitFor(() => {
        const text = result.container.textContent || ''
        // No error messages should appear
        expect(text).not.toContain('stream error')
        expect(text).not.toContain('Unknown error')
      }, { timeout: 10000 })
    })
  })
})
