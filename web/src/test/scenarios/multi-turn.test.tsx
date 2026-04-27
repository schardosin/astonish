/**
 * Multi-Turn Conversation Scenario Tests (P1.7)
 *
 * Tests that a user can send multiple messages in the same session and
 * receive different responses each time. Uses the queue-based mockFetch
 * to serve different SSE event sets for each POST /api/studio/chat call.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

// Fixtures
import turn1 from '../fixtures/scenarios/core/multi-turn-1.json'
import turn2 from '../fixtures/scenarios/core/multi-turn-2.json'

describe('Multi-Turn Conversation Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('P1.7: Queue-based multi-turn', () => {
    it('sends two messages and gets different responses for each', async () => {
      // Use queue mode: first POST gets turn1 events, second POST gets turn2 events
      result = renderChat({
        scenarioEvents: [
          turn1.events as FixtureEvent[],
          turn2.events as FixtureEvent[],
        ],
      })

      // --- Turn 1 ---
      await result.sendMessage('Who created Go?')

      // First turn response should appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Robert Griesemer')
      }, { timeout: 10000 })

      // --- Turn 2 ---
      await result.sendMessage('When was it released?')

      // Second turn response should appear (different from first)
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('March 2012')
      }, { timeout: 10000 })

      // Both responses should coexist in the DOM (chat history)
      const fullText = result.container.textContent || ''
      expect(fullText).toContain('Robert Griesemer')
      expect(fullText).toContain('March 2012')
    })

    it('user messages appear in conversation order', async () => {
      result = renderChat({
        scenarioEvents: [
          turn1.events as FixtureEvent[],
          turn2.events as FixtureEvent[],
        ],
      })

      await result.sendMessage('Who created Go?')

      await waitFor(() => {
        expect(result.container.textContent).toContain('Robert Griesemer')
      }, { timeout: 10000 })

      await result.sendMessage('When was it released?')

      await waitFor(() => {
        expect(result.container.textContent).toContain('March 2012')
      }, { timeout: 10000 })

      // Both user messages should be visible
      const fullText = result.container.textContent || ''
      expect(fullText).toContain('Who created Go?')
      expect(fullText).toContain('When was it released?')
    })
  })
})
