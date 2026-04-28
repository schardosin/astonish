/**
 * Core Chat Interaction Tests (A3, A8, I5)
 *
 * Tests for core chat interactions that aren't purely SSE-stream-focused:
 * session creation handling, partial-stream preservation, and connection errors.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import streamAbort from '../fixtures/scenarios/core/stream-abort.json'
import simpleQa from '../fixtures/scenarios/core/simple-qa.json'

describe('Core Chat Interaction Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('A8: Stream Abort — Partial Text Preserved', () => {
    it('preserves partial text when stream ends prematurely', async () => {
      result = renderChat({
        scenarioEvents: streamAbort.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze')

      // First chunk should be preserved
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Starting to analyze')
      }, { timeout: 10000 })

      // Second chunk should be accumulated
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('multiple sources')
      }, { timeout: 10000 })
    })

    it('accumulates both text chunks into a single agent message', async () => {
      result = renderChat({
        scenarioEvents: streamAbort.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Starting to analyze')
        expect(text).toContain('multiple sources')
      }, { timeout: 10000 })

      // Should be one agent message, not two separate ones
      const markdownElements = screen.getAllByTestId('markdown')
      const agentMessages = markdownElements.filter(el =>
        el.textContent?.includes('Starting to analyze')
      )
      expect(agentMessages).toHaveLength(1)
    })
  })

  describe('I5: Connection Error', () => {
    it('shows error message when SSE connection fails', async () => {
      // Suppress expected console.error from connectChat's onError path
      const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

      result = renderChat({
        mockConfig: {
          customHandlers: {
            '/api/studio/chat': () => new Response('Internal Server Error', {
              status: 500,
              statusText: 'Internal Server Error',
            }),
          },
        },
      })

      await result.sendMessage('Test')

      // The connectChat onError callback appends { type: 'error', content: err.message }
      // The error text should come from the response body or status
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Internal Server Error')
      }, { timeout: 10000 })

      errorSpy.mockRestore()
    })
  })

  describe('A3: Session Creation', () => {
    it('processes session event and continues normally', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      await result.sendMessage('What is Go?')

      // Agent response should appear — session event (isNew: true) was processed without error
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Go is a statically typed language')
      }, { timeout: 10000 })
    })

    it('adds user message to chat immediately', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      await result.sendMessage('What is Go?')

      await waitFor(() => {
        expect(screen.getByText('What is Go?')).toBeInTheDocument()
      }, { timeout: 10000 })
    })
  })
})
