/**
 * Reconnection Scenario Tests (P1.9)
 *
 * Tests the reconnection flow: when a user selects a session that has an
 * active background runner, the frontend reconnects via GET /api/studio/sessions/:id/stream
 * instead of loading static history. The SSE events from the reconnect stream should
 * render normally — text, tool calls, results, etc.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

// Fixtures
import reconnectStream from '../fixtures/scenarios/core/reconnect-stream.json'

describe('Reconnection Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('P1.9: Reconnect to active session', () => {
    it('renders events from reconnect stream when session is running', async () => {
      // Set up: session "sess-reconnect" exists and is running.
      // When the component loads with initialSessionId, it checks the session status.
      // If running=true, it calls connectChatStream (GET /sessions/:id/stream)
      // which returns our reconnectEvents.
      result = renderChat({
        reconnectEvents: reconnectStream.events as FixtureEvent[],
        initialSessionId: 'sess-reconnect',
        sessions: [
          { id: 'sess-reconnect', title: 'Active Session' },
        ],
        sessionStatus: { sessionId: 'sess-reconnect', running: true, eventCount: 5 },
      })

      // Wait for reconnect stream events to render
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Go 1.24 was released')
      }, { timeout: 10000 })

      // Tool call should also be visible (web_search)
      await waitFor(() => {
        const codeElements = result.container.querySelectorAll('code')
        const hasWebSearch = Array.from(codeElements).some(el =>
          el.textContent?.includes('web_search')
        )
        expect(hasWebSearch).toBe(true)
      }, { timeout: 10000 })
    })

    it('transitions to idle state after reconnect stream completes', async () => {
      result = renderChat({
        reconnectEvents: reconnectStream.events as FixtureEvent[],
        initialSessionId: 'sess-reconnect',
        sessions: [
          { id: 'sess-reconnect', title: 'Active Session' },
        ],
        sessionStatus: { sessionId: 'sess-reconnect', running: true, eventCount: 5 },
      })

      // Wait for stream to complete
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Go 1.24 was released')
      }, { timeout: 10000 })

      // After stream completes, textarea should be back to idle placeholder
      await waitFor(() => {
        const ta = result.container.querySelector('[data-testid="chat-input"]') as HTMLTextAreaElement
        if (ta) {
          const placeholder = ta.getAttribute('placeholder') || ''
          expect(placeholder.toLowerCase()).not.toContain('responding')
        }
      }, { timeout: 10000 })
    })
  })
})
