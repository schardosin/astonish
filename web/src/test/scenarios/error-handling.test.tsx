/**
 * Error Handling Scenario Tests (I1-I5)
 *
 * Tests error events, structured error info, retry events,
 * and connection error handling.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import simpleError from '../fixtures/scenarios/errors/simple-error.json'
import structuredErrorInfo from '../fixtures/scenarios/errors/structured-error-info.json'
import retryEvent from '../fixtures/scenarios/errors/retry-event.json'

describe('Error Handling Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('I1: Simple Error', () => {
    it('renders error message in error styling', async () => {
      result = renderChat({
        scenarioEvents: simpleError.events as FixtureEvent[],
      })

      await result.sendMessage('Test')

      await waitFor(() => {
        expect(screen.getByText(/Rate limit exceeded/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('I2: Structured Error Info', () => {
    it('renders error with title, reason, and suggestion', async () => {
      result = renderChat({
        scenarioEvents: structuredErrorInfo.events as FixtureEvent[],
      })

      await result.sendMessage('Test')

      // Title
      await waitFor(() => {
        expect(screen.getByText(/Provider Error/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Reason
      await waitFor(() => {
        expect(screen.getByText(/API key is invalid or expired/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Suggestion
      await waitFor(() => {
        expect(screen.getByText(/Check your API key in Settings/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('includes original error in collapsible details', async () => {
      result = renderChat({
        scenarioEvents: structuredErrorInfo.events as FixtureEvent[],
      })

      await result.sendMessage('Test')

      // The original error should be somewhere in the document
      await waitFor(() => {
        expect(screen.getByText(/HTTP 401 Unauthorized/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('I4: Retry Event', () => {
    it('renders retry indicator and continues with text', async () => {
      result = renderChat({
        scenarioEvents: retryEvent.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze this')

      // Retry indicator should show attempt info (e.g., "Retry 1/3:")
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Retry 1/3')
      }, { timeout: 10000 })

      // Text after retry should appear (the continuation)
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('15% increase in performance')
      }, { timeout: 10000 })
    })
  })
})
