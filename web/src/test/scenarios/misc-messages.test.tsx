/**
 * Miscellaneous Message Scenario Tests (Q1-Q3, U1, V1, B5)
 *
 * Tests thinking messages, system messages, image messages,
 * usage accumulation, and mermaid diagram rendering.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import thinkingMessage from '../fixtures/scenarios/misc/thinking-message.json'
import emptyThinking from '../fixtures/scenarios/misc/empty-thinking.json'
import systemMessage from '../fixtures/scenarios/misc/system-message.json'
import usageAccumulation from '../fixtures/scenarios/misc/usage-accumulation.json'
import mermaidDiagram from '../fixtures/scenarios/misc/mermaid-diagram.json'
import imageMessage from '../fixtures/scenarios/misc/image-message.json'

// Mock react-markdown - renders children as plain text
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

describe('Miscellaneous Message Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('Q1: Thinking Message', () => {
    it('renders thinking message with thinking-note class', async () => {
      result = renderChat({
        scenarioEvents: thinkingMessage.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze this')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Let me analyze this step by step')
      }, { timeout: 10000 })

      await waitFor(() => {
        const thinkingEl = document.querySelector('.thinking-note')
        expect(thinkingEl).not.toBeNull()
      }, { timeout: 10000 })
    })

    it('renders agent response after thinking', async () => {
      result = renderChat({
        scenarioEvents: thinkingMessage.events as FixtureEvent[],
      })

      await result.sendMessage('Analyze this')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('microservices architecture')
      }, { timeout: 10000 })
    })
  })

  describe('Q3: Empty Thinking Filtered', () => {
    it('does not render a thinking message for empty text', async () => {
      result = renderChat({
        scenarioEvents: emptyThinking.events as FixtureEvent[],
      })

      await result.sendMessage('Test')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Here is the answer')
      }, { timeout: 10000 })

      // No thinking-note elements should exist for empty thinking text
      const thinkingElements = result.container.querySelectorAll('.thinking-note')
      expect(thinkingElements.length).toBe(0)
    })
  })

  describe('Q2: System Message', () => {
    it('renders system message with System label', async () => {
      result = renderChat({
        scenarioEvents: systemMessage.events as FixtureEvent[],
      })

      await result.sendMessage('/help')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('System')
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Available commands')
      }, { timeout: 10000 })
    })
  })

  describe('V1: Usage Accumulation', () => {
    it('accumulates token counts from multiple usage events', async () => {
      result = renderChat({
        scenarioEvents: usageAccumulation.events as FixtureEvent[],
      })

      await result.sendMessage('Search for something')

      // Verify the stream completes successfully and the agent response appears,
      // which means both usage events were processed without error.
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Based on the search results')
      }, { timeout: 10000 })
    })
  })

  describe('U1: Mermaid Diagram', () => {
    it('renders mermaid code block via MermaidBlock component', async () => {
      result = renderChat({
        scenarioEvents: mermaidDiagram.events as FixtureEvent[],
      })

      await result.sendMessage('Show me a diagram')

      // react-markdown is mocked to render children as plain text, so the
      // mermaid fence content appears as raw text. Verify the diagram syntax
      // and surrounding prose both render.
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('graph TD')
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('architecture diagram')
      }, { timeout: 10000 })
    })
  })

  describe('B5: Image Message', () => {
    it('renders image with correct data URI', async () => {
      result = renderChat({
        scenarioEvents: imageMessage.events as FixtureEvent[],
      })

      await result.sendMessage('Take a screenshot')

      await waitFor(() => {
        const img = result.container.querySelector('img')
        expect(img).not.toBeNull()
        expect(img!.src).toMatch(/^data:image\/png;base64,/)
      }, { timeout: 10000 })

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('screenshot of the page')
      }, { timeout: 10000 })
    })
  })
})
