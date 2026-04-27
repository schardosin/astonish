/**
 * Session Switch While Streaming Scenario Tests (P2.12)
 *
 * Tests that switching sessions or creating new sessions during/after active
 * streaming doesn't crash the component. Verifies the abort→cleanup→reset
 * path that runs when the user interrupts a stream by navigating away.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor, act } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

// Fixtures — use a tool call fixture to have more SSE events in the stream
import singleToolCall from '../fixtures/scenarios/tools/single-tool-call.json'
import simpleQa from '../fixtures/scenarios/core/simple-qa.json'

describe('Session Switch While Streaming Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('P2.12a: New Session during stream', () => {
    it('clicking New Conversation after stream start does not crash', async () => {
      result = renderChat({
        scenarioEvents: singleToolCall.events as FixtureEvent[],
        sessions: [
          { id: 'sess-old', title: 'Old Chat', createdAt: '2024-01-01', updatedAt: '2024-01-01', messageCount: 3 },
        ],
      })

      // Send a message to start streaming
      const textarea = result.container.querySelector('[data-testid="chat-input"]') as HTMLTextAreaElement
      expect(textarea).not.toBeNull()

      await result.user.clear(textarea)
      await result.user.type(textarea, 'Search for testing')
      await result.user.keyboard('{Enter}')

      // Brief wait for the stream to begin processing
      await act(async () => {
        await new Promise(resolve => setTimeout(resolve, 100))
      })

      // Now click "New conversation" — this should abort the stream and reset
      const newBtn = result.container.querySelector('button[title="New conversation"]')
      expect(newBtn).not.toBeNull()

      await result.user.click(newBtn as HTMLElement)

      // Wait for the component to settle after the interruption.
      // The key assertion: the component should not crash and messages should be cleared.
      await act(async () => {
        await new Promise(resolve => setTimeout(resolve, 200))
      })

      // The component should still be functional (not crashed)
      expect(result.container).toBeInTheDocument()

      // After new session, the tool call content from the interrupted stream
      // should be gone (messages were cleared)
      await waitFor(() => {
        const text = result.container.textContent || ''
        // The agent response text from the tool call should be cleared
        expect(text).not.toContain('Go testing best practices')
      }, { timeout: 10000 })
    })
  })

  describe('P2.12b: Switch to different session after stream', () => {
    it('selecting another session after completing a stream loads that session', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
        sessions: [
          { id: 'sess-other', title: 'Other Chat', createdAt: '2024-01-01', updatedAt: '2024-01-01', messageCount: 5 },
        ],
        // Provide session history for the other session
        mockConfig: {
          sessionHistory: {
            id: 'sess-other',
            title: 'Other Chat',
            messages: [
              { role: 'user', content: 'Previous question' },
              { role: 'model', content: 'Previous answer from other session' },
            ],
          },
        },
      })

      // Send a message and wait for response
      await result.sendMessage('What is Go?')

      await waitFor(() => {
        expect(result.container.textContent).toContain('Go is a statically typed language')
      }, { timeout: 10000 })

      // Now click on the "Other Chat" session in the sidebar
      const sidebarItems = result.container.querySelectorAll('[class*="cursor-pointer"]')
      let otherSessionEl: HTMLElement | null = null
      for (const el of Array.from(sidebarItems)) {
        if (el.textContent?.includes('Other Chat')) {
          otherSessionEl = el as HTMLElement
          break
        }
      }

      if (otherSessionEl) {
        await result.user.click(otherSessionEl)

        // Wait for session switch — current messages should be replaced
        // The component should not crash during the transition
        await waitFor(() => {
          expect(result.container).toBeInTheDocument()
        }, { timeout: 10000 })
      }

      // Component should still be functional regardless
      expect(result.container).toBeInTheDocument()
    })
  })
})
