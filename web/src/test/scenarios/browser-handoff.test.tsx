/**
 * Browser Handoff Scenario Tests (P1)
 *
 * Tests the browser_request_human tool result being synthesized into a
 * browser_handoff message, which renders a BrowserView with page title,
 * reason text, and VNC iframe.
 *
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

      // Placeholder + harness panel host BrowserView
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('CAPTCHA detected')
        expect(result.container.querySelector('[data-testid="harness-panel"]')?.getAttribute('data-harness-kind')).toBe('browser_handoff')
        expect(result.container.querySelector('[data-testid="harness-placeholder"]')).toBeTruthy()
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('example.com/login')
      }, { timeout: 10000 })
    })
  })
})
