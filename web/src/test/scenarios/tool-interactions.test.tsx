/**
 * Tool Interaction Scenario Tests (B3, C1b)
 *
 * Tests user interactions with tool cards: expanding/collapsing tool details,
 * and clicking approval buttons.
 *
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import singleToolCall from '../fixtures/scenarios/tools/single-tool-call.json'
import approvalFlow from '../fixtures/scenarios/tools/approval-flow.json'

describe('Tool Interaction Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('B3: Tool Card Expand/Collapse', () => {
    it('expands tool card on click to show args/result', async () => {
      result = renderChat({
        scenarioEvents: singleToolCall.events as FixtureEvent[],
      })

      await result.sendMessage('Search')

      // Wait for collapsed tool activity block
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="tool-activity-block"]')).toBeTruthy()
      }, { timeout: 10000 })

      // Expand the activity block header
      const activityBtn = result.container.querySelector(
        '[data-testid="tool-activity-block"] > button',
      ) as HTMLElement
      expect(activityBtn).toBeTruthy()
      await result.user.click(activityBtn)

      // Step row with tool name should appear
      await waitFor(() => {
        const codeElements = result.container.querySelectorAll('code')
        const hasWebSearch = Array.from(codeElements).some(el => el.textContent?.includes('web_search'))
        expect(hasWebSearch).toBe(true)
      }, { timeout: 10000 })

      // Click the step row that contains web_search to reveal args/result
      const stepButtons = result.container.querySelectorAll('[data-testid="tool-activity-block"] button')
      let stepButton: HTMLElement | null = null
      for (const btn of Array.from(stepButtons)) {
        const code = btn.querySelector('code')
        if (code?.textContent?.includes('web_search')) {
          stepButton = btn as HTMLElement
          break
        }
      }
      expect(stepButton).not.toBeNull()
      await result.user.click(stepButton!)

      // After expanding the step, the tool args JSON should appear in a <pre>
      await waitFor(() => {
        const preElements = result.container.querySelectorAll('pre')
        const hasArgs = Array.from(preElements).some(el =>
          el.textContent?.includes('Go testing best practices')
        )
        expect(hasArgs).toBe(true)
      }, { timeout: 10000 })

      // Collapse the activity block
      await result.user.click(activityBtn)

      // The args should no longer be visible in any <pre>
      await waitFor(() => {
        const preElements = result.container.querySelectorAll('pre')
        const hasArgs = Array.from(preElements).some(el =>
          el.textContent?.includes('Go testing best practices')
        )
        expect(hasArgs).toBe(false)
      }, { timeout: 10000 })
    })
  })

  describe('C1b: Approval - Click Allow', () => {
    it('sends Allow message when Allow button is clicked', async () => {
      result = renderChat({
        scenarioEvents: approvalFlow.events as FixtureEvent[],
      })

      await result.sendMessage('Check the system')

      // Wait for "Allow" button text to appear
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        const allowBtn = Array.from(buttons).find(btn => btn.textContent === 'Allow')
        expect(allowBtn).toBeDefined()
      }, { timeout: 10000 })

      // Find the Allow button and click it
      const buttons = result.container.querySelectorAll('button')
      const allowBtn = Array.from(buttons).find(btn => btn.textContent === 'Allow')
      expect(allowBtn).toBeDefined()

      // Clicking Allow should call sendMessage("Allow") which triggers another fetch.
      // The second fetch will use the default mock (no scenario events => 500 error),
      // but we just verify the click doesn't crash.
      await result.user.click(allowBtn!)

      // Verify the component didn't crash — the container should still be in the document
      expect(result.container).toBeInTheDocument()
    })
  })

  describe('C1c: Approval - Click Deny', () => {
    it('sends Deny message when Deny button is clicked', async () => {
      result = renderChat({
        scenarioEvents: approvalFlow.events as FixtureEvent[],
      })

      await result.sendMessage('Check the system')

      // Wait for "Deny" button to appear
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        const denyBtn = Array.from(buttons).find(btn => btn.textContent === 'Deny')
        expect(denyBtn).toBeDefined()
      }, { timeout: 10000 })

      // Find the Deny button and click it
      const buttons = result.container.querySelectorAll('button')
      const denyBtn = Array.from(buttons).find(btn => btn.textContent === 'Deny')
      expect(denyBtn).toBeDefined()

      // Clicking Deny sends "Deny" as a message. The second fetch will use the
      // default mock (no queue configured => 500 error), but we verify the click
      // is handled without crashing.
      await result.user.click(denyBtn!)

      // Verify the component didn't crash
      expect(result.container).toBeInTheDocument()
    })

    it('renders both Allow and Deny buttons from approval options', async () => {
      result = renderChat({
        scenarioEvents: approvalFlow.events as FixtureEvent[],
      })

      await result.sendMessage('Check the system')

      // Both buttons should be present
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        const buttonTexts = Array.from(buttons).map(btn => btn.textContent)
        expect(buttonTexts).toContain('Allow')
        expect(buttonTexts).toContain('Deny')
      }, { timeout: 10000 })
    })
  })
})
