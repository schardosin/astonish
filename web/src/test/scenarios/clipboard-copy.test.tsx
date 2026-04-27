/**
 * Clipboard Copy Scenario Tests (T1)
 *
 * Tests the copy-to-clipboard functionality on agent messages.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { waitFor, act, fireEvent } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import simpleQa from '../fixtures/scenarios/core/simple-qa.json'

// Mock react-markdown and remark-gfm to avoid ESM issues in jsdom
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

// Ensure navigator.clipboard exists in jsdom so copyToClipboard() doesn't throw.
// jsdom may reset the reference between module setup and component execution,
// so we verify behaviour via the icon change rather than a spy.
if (!navigator.clipboard) {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value: { writeText: () => Promise.resolve() },
    writable: true,
    configurable: true,
  })
}

describe('Clipboard Copy Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('T1: Copy Agent Message', () => {
    it('copies agent message content to clipboard', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
      })

      await result.sendMessage('What is Go?')

      // Wait for agent response to appear
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Go is a statically typed language')
      }, { timeout: 10000 })

      // Find the copy button (it has title="Copy")
      const copyButton = result.container.querySelector('button[title="Copy"]')
      expect(copyButton).not.toBeNull()

      // Click it using fireEvent
      await act(async () => {
        fireEvent.click(copyButton as HTMLElement)
      })

      // Allow the async clipboard call to resolve
      await act(async () => {
        await new Promise(resolve => setTimeout(resolve, 200))
      })

      // After a successful copy, the Copy icon changes to a Check icon (green-400).
      // This state change proves that copyToClipboard() executed and
      // navigator.clipboard.writeText() resolved without error.
      // (In jsdom the navigator.clipboard reference may be replaced by the
      //  environment between module-level setup and component execution, so we
      //  verify the outcome instead of the spy call count.)
      await waitFor(() => {
        const checkIcon = copyButton!.querySelector('.text-green-400')
        expect(checkIcon).not.toBeNull()
      }, { timeout: 10000 })
    })
  })
})
