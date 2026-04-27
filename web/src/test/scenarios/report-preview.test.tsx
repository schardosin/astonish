/**
 * Report Preview Scenario Tests
 *
 * Tests the astonish-report code fence rendering: report_preview SSE event
 * triggers ResultCard rendering, and sources SSE event renders SourceCitations.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'

// Shared mocks
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import reportPreview from '../fixtures/scenarios/core/report-preview.json'

describe('Report Preview Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  it('renders report_preview as a document card with report content', async () => {
    result = renderChat({
      scenarioEvents: reportPreview.events as FixtureEvent[],
    })

    await result.sendMessage('Analyze the market')

    // Report title should appear
    await waitFor(() => {
      const text = result.container.textContent || ''
      expect(text).toContain('Market Analysis Report')
    }, { timeout: 10000 })

    // Report content sections should be visible
    await waitFor(() => {
      const text = result.container.textContent || ''
      expect(text).toContain('Executive Summary')
      expect(text).toContain('Key Findings')
      expect(text).toContain('Cloud adoption accelerating')
    }, { timeout: 10000 })

    // The "Report" label should appear (from the report_preview render path)
    await waitFor(() => {
      const text = result.container.textContent || ''
      expect(text).toContain('Report')
    }, { timeout: 10000 })
  })

  it('renders sources as citation pills', async () => {
    result = renderChat({
      scenarioEvents: reportPreview.events as FixtureEvent[],
    })

    await result.sendMessage('Analyze the market')

    // Sources should appear (SourceCitations renders "N sources")
    await waitFor(() => {
      const text = result.container.textContent || ''
      expect(text).toContain('source')
    }, { timeout: 10000 })
  })

  it('does not render ResultCard for plain long agent messages (heuristic removed)', async () => {
    // Use a fixture with a long agent message but no report_preview event
    const longTextEvents = [
      { type: 'session', data: { sessionId: 'sess-no-report', isNew: true } },
      { type: 'tool_call', data: { name: 'read_file', args: { path: '/test.txt' } } },
      { type: 'tool_result', data: { name: 'read_file', result: 'file contents' } },
      { type: 'text', data: { text: 'A'.repeat(600) } },
      { type: 'usage', data: { input_tokens: 50, output_tokens: 50, total_tokens: 100 } },
      { type: 'done', data: { done: true } },
    ] as FixtureEvent[]

    result = renderChat({ scenarioEvents: longTextEvents })
    await result.sendMessage('Read file')

    // Wait for the long text to render
    await waitFor(() => {
      const text = result.container.textContent || ''
      expect(text).toContain('AAAA')
    }, { timeout: 10000 })

    // Verify there's no ResultCard-specific UI (the "Report" label should NOT appear
    // since there's no report_preview event)
    const reportLabels = result.container.querySelectorAll('.text-xs.font-medium')
    const hasReportLabel = Array.from(reportLabels).some(el => el.textContent === 'Report')
    expect(hasReportLabel).toBe(false)
  })
})
