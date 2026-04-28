/**
 * Browser Handoff Scenario Tests (P1)
 *
 * Tests the browser_request_human tool result being synthesized into a
 * browser_handoff message, which renders a BrowserView with page title,
 * reason text, and VNC iframe.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

// Fixtures
import browserHandoff from '../fixtures/scenarios/browser/browser-handoff.json'

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
