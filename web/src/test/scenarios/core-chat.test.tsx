/**
 * Core Chat Flow Scenario Tests (A1-A8)
 *
 * Tests the fundamental chat lifecycle: sending messages, receiving SSE
 * event streams, rendering agent responses, and handling session management.
 *
 * These tests use the REAL connectChat() SSE parsing code, the REAL StudioChat
 * state management, and the REAL component tree. Only fetch() is mocked (at
 * the network level) to return simulated SSE streams from JSON fixtures.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import simpleQa from '../fixtures/scenarios/core/simple-qa.json'
import multiChunk from '../fixtures/scenarios/core/multi-chunk-streaming.json'
import emptyResponse from '../fixtures/scenarios/core/empty-response.json'
import sessionTitle from '../fixtures/scenarios/core/session-title-update.json'

describe('Core Chat Flow Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) {
      result.cleanup()
    }
  })

  describe('A1: Simple Q&A', () => {
    it('renders agent response after sending a message', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      // Should show home page initially (no messages)
      expect(screen.getByTestId('home-page')).toBeInTheDocument()

      // Send a message
      await result.sendMessage('What is Go?')

      // Agent response should appear
      await waitFor(() => {
        expect(screen.getByText(/Go is a statically typed language/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('shows user message in chat after sending', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      await result.sendMessage('What is Go?')

      // User message should appear
      await waitFor(() => {
        expect(screen.getByText('What is Go?')).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('hides home page after first message', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      expect(screen.getByTestId('home-page')).toBeInTheDocument()

      await result.sendMessage('What is Go?')

      await waitFor(() => {
        expect(screen.queryByTestId('home-page')).not.toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('A2: Multi-Chunk Streaming', () => {
    it('accumulates text chunks into a single agent message', async () => {
      result = renderChat({
        scenarioEvents: multiChunk.events as FixtureEvent[],
      })

      await result.sendMessage('Hello')

      // The three text chunks should accumulate into one message
      await waitFor(() => {
        expect(screen.getByText(/Hello world, how are you today/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('creates only one agent message for multiple text events', async () => {
      result = renderChat({
        scenarioEvents: multiChunk.events as FixtureEvent[],
      })

      await result.sendMessage('Hello')

      await waitFor(() => {
        expect(screen.getByText(/Hello world, how are you today/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Count markdown elements - should be exactly one for the agent response
      // (not three separate ones for each chunk)
      const markdownElements = screen.getAllByTestId('markdown')
      const agentMessages = markdownElements.filter(el =>
        el.textContent?.includes('Hello world')
      )
      expect(agentMessages).toHaveLength(1)
    })
  })

  describe('A7: Empty Response Safety', () => {
    it('shows error message when model returns empty response', async () => {
      result = renderChat({
        scenarioEvents: emptyResponse.events as FixtureEvent[],
      })

      await result.sendMessage('Test')

      await waitFor(() => {
        expect(screen.getByText(/model returned an empty response/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('A5: Session Title Update', () => {
    it('renders agent response with session title event mid-stream', async () => {
      result = renderChat({
        scenarioEvents: sessionTitle.events as FixtureEvent[],
        sessions: [
          { id: 'sess-titled', title: 'New Conversation', createdAt: '2024-01-01', updatedAt: '2024-01-01', messageCount: 0 },
        ],
      })

      await result.sendMessage('Tell me about Go testing')

      // Agent response should appear
      await waitFor(() => {
        expect(screen.getByText(/Go testing best practices/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })
})
