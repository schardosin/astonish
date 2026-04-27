/**
 * Browser Handoff Scenario Tests (P1)
 *
 * Tests the browser_request_human tool result being synthesized into a
 * browser_handoff message, which renders a BrowserView with page title,
 * reason text, and VNC iframe.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import browserHandoff from '../fixtures/scenarios/browser/browser-handoff.json'

// Mocks
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

describe('Browser Handoff Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('P1: Browser Handoff', () => {
    it('renders browser view with page title and reason', async () => {
      result = renderChat({
        scenarioEvents: browserHandoff.events as FixtureEvent[],
      })

      await result.sendMessage('Check that page')

      // The BrowserView header renders "Browser: {data.reason}"
      // where reason comes from result.message = "CAPTCHA detected, please solve it"
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('CAPTCHA detected')
      }, { timeout: 10000 })

      // The BrowserView renders the page URL in the header
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('example.com/login')
      }, { timeout: 10000 })
    })
  })
})
