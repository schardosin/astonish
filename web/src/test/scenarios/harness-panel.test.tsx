/**
 * Harness panel layout: auto-open, sidebar collapse, placeholder re-focus.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { waitFor, fireEvent } from '@testing-library/react'
import './scenarioSetup'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import appGenerated from '../fixtures/scenarios/apps/app-generated.json'
import distillPreview from '../fixtures/scenarios/distill/distill-preview.json'

vi.mock('../../components/chat/AppPreview', () => ({
  default: ({ code }: { code: string }) => (
    <div data-testid="app-preview-iframe">{code}</div>
  ),
}))

vi.mock('../../components/FlowPreview', () => ({
  default: () => <div data-testid="flow-preview">Flow Preview</div>,
}))

describe('Harness Panel Layout', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  it('collapses session sidebar when harness opens', async () => {
    result = renderChat({
      scenarioEvents: appGenerated.events as FixtureEvent[],
    })

    // Expanded sidebar shows "Conversations" header
    await waitFor(() => {
      expect(result.container.textContent).toContain('Conversations')
      expect(
        Array.from(result.container.querySelectorAll('button')).some(
          b => b.getAttribute('title') === 'Hide sidebar',
        ),
      ).toBe(true)
    })

    await result.sendMessage('Generate a dashboard')

    await waitFor(() => {
      expect(result.container.querySelector('[data-testid="harness-panel"]')).toBeTruthy()
      // Collapsed strip exposes expand control
      expect(
        Array.from(result.container.querySelectorAll('button')).some(
          b => b.getAttribute('title') === 'Show sidebar',
        ),
      ).toBe(true)
    }, { timeout: 10000 })
  })

  it('renders a resize handle on the harness panel', async () => {
    result = renderChat({
      scenarioEvents: appGenerated.events as FixtureEvent[],
    })

    await result.sendMessage('Generate a dashboard')

    await waitFor(() => {
      expect(result.container.querySelector('[data-testid="harness-panel"]')).toBeTruthy()
      expect(result.container.querySelector('[data-testid="harness-resize-handle"]')).toBeTruthy()
      expect(result.container.querySelector('[data-testid="harness-resize-grip"]')).toBeTruthy()
    }, { timeout: 10000 })
  })

  it('re-focuses distill when its placeholder is clicked after an app', async () => {
    // First play distill, then app — latest is app. Click distill placeholder to re-focus.
    const combined: FixtureEvent[] = [
      ...(distillPreview.events as FixtureEvent[]),
    ]

    result = renderChat({ scenarioEvents: combined })
    await result.sendMessage('Distill this conversation')

    await waitFor(() => {
      expect(result.container.querySelector('[data-testid="harness-panel"]')?.getAttribute('data-harness-kind')).toBe('distill')
    }, { timeout: 10000 })

    // Close harness then re-open via placeholder
    const closeBtn = result.container.querySelector('[data-testid="harness-panel-close"]')
    expect(closeBtn).toBeTruthy()
    fireEvent.click(closeBtn!)

    await waitFor(() => {
      expect(result.container.querySelector('[data-testid="harness-panel"]')).toBeNull()
    })

    const placeholder = result.container.querySelector('[data-testid="harness-placeholder"]')
    expect(placeholder).toBeTruthy()
    fireEvent.click(placeholder!)

    await waitFor(() => {
      const panel = result.container.querySelector('[data-testid="harness-panel"]')
      expect(panel).toBeTruthy()
      expect(panel?.getAttribute('data-harness-kind')).toBe('distill')
    })
  })
})
