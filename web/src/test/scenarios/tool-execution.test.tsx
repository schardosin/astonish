/**
 * Tool Execution Scenario Tests (B1-B7, C1-C3)
 *
 * Tests tool call/result rendering, parallel tool calls, streaming text
 * finalization, artifacts, auto-approval, and approval flow.
 *
 */

import { describe, it, expect, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import singleToolCall from '../fixtures/scenarios/tools/single-tool-call.json'
import parallelToolCalls from '../fixtures/scenarios/tools/parallel-tool-calls.json'
import toolWithArtifact from '../fixtures/scenarios/tools/tool-with-artifact.json'
import autoApprovedTool from '../fixtures/scenarios/tools/auto-approved-tool.json'
import approvalFlow from '../fixtures/scenarios/tools/approval-flow.json'

describe('Tool Execution Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('B1: Single Tool Call', () => {
    it('renders tool call and tool result messages', async () => {
      result = renderChat({
        scenarioEvents: singleToolCall.events as FixtureEvent[],
      })

      await result.sendMessage('Search for Go testing best practices')

      // Tool activity block should appear (interstitial "Let me search" is folded as process text)
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="tool-activity-block"]')).toBeTruthy()
        const text = result.container.textContent || ''
        expect(text).toMatch(/1 search|Searching|web_search|1 note/i)
      }, { timeout: 5000 })

      // Dimmed process narration is visible without expanding
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="activity-process-text"]')?.textContent)
          .toMatch(/Let me search for that/)
      }, { timeout: 5000 })

      // Final user-facing text should appear outside the fold as an Agent bubble
      await waitFor(() => {
        expect(screen.getByText(/best practices for Go testing/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Interstitial is not labeled as a separate Agent bubble — only the final answer is
      const agentLabels = Array.from(result.container.querySelectorAll('div'))
        .filter(el => el.children.length === 0 && el.textContent === 'Agent')
      expect(agentLabels.length).toBe(1)

      // Expand to confirm interstitial note was preserved in notes list
      const activityBtn = result.container.querySelector(
        '[data-testid="tool-activity-block"] > button',
      ) as HTMLElement
      await result.user.click(activityBtn)
      await waitFor(() => {
        expect(screen.getByTestId('activity-notes')).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('B2: Parallel Tool Calls', () => {
    it('renders multiple tool calls in order', async () => {
      result = renderChat({
        scenarioEvents: parallelToolCalls.events as FixtureEvent[],
      })

      await result.sendMessage('Search and read file')

      // Collapsed activity summary should cover both tools
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="tool-activity-block"]')).toBeTruthy()
        const text = result.container.textContent || ''
        expect(text).toMatch(/explored|1 search|Searching/i)
      }, { timeout: 5000 })

      // Expand the activity block to reveal per-tool names
      const activityBtn = result.container.querySelector(
        '[data-testid="tool-activity-block"] > button',
      ) as HTMLElement
      expect(activityBtn).toBeTruthy()
      await result.user.click(activityBtn)

      await waitFor(() => {
        const codeElements = result.container.querySelectorAll('code')
        const names = Array.from(codeElements).map(el => el.textContent)
        expect(names.some(n => n?.includes('web_search'))).toBe(true)
        expect(names.some(n => n?.includes('read_file'))).toBe(true)
      }, { timeout: 5000 })

      // Final text
      await waitFor(() => {
        expect(screen.getByText(/found the information from both sources/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('B4: Streaming Text Finalization Before Tool Call', () => {
    it('commits streaming text when tool_call interrupts it', async () => {
      result = renderChat({
        scenarioEvents: singleToolCall.events as FixtureEvent[],
      })

      await result.sendMessage('Search for something')

      // Final answer remains a visible Agent bubble after the fold
      await waitFor(() => {
        expect(screen.getByText(/best practices for Go testing/)).toBeInTheDocument()
      }, { timeout: 5000 })

      // Pre-tool narration is visible as dimmed process text (not lost / not a second Agent bubble)
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="activity-process-text"]')?.textContent)
          .toMatch(/Let me search for that/)
      }, { timeout: 5000 })
    })
  })

  describe('B6: Auto-Approved Tool', () => {
    it('renders auto-approval badge with tool name', async () => {
      result = renderChat({
        scenarioEvents: autoApprovedTool.events as FixtureEvent[],
      })

      await result.sendMessage('Run a command')

      // Activity fold absorbs auto_approved as a note; expand to see tool name
      await waitFor(() => {
        expect(result.container.querySelector('[data-testid="tool-activity-block"]')).toBeTruthy()
      }, { timeout: 5000 })

      const activityBtn = result.container.querySelector(
        '[data-testid="tool-activity-block"] > button',
      ) as HTMLElement
      await result.user.click(activityBtn)

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('shell_command')
      }, { timeout: 5000 })

      // Final text
      await waitFor(() => {
        expect(screen.getByText(/directory listing shows 5 entries/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('B7: Tool with Artifact', () => {
    it('renders artifact card when write_file produces an artifact event', async () => {
      result = renderChat({
        scenarioEvents: toolWithArtifact.events as FixtureEvent[],
      })

      await result.sendMessage('Create a report')

      // Artifact filename should appear somewhere in the document
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('report.md')
      }, { timeout: 5000 })

      // Final text
      await waitFor(() => {
        expect(screen.getByText(/created the report/)).toBeInTheDocument()
      }, { timeout: 5000 })
    })
  })

  describe('C1: Approval Flow', () => {
    it('renders approval prompt with option buttons', async () => {
      result = renderChat({
        scenarioEvents: approvalFlow.events as FixtureEvent[],
      })

      await result.sendMessage('Check the system')

      // Should show the approval prompt with tool name
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('shell_command')
      }, { timeout: 5000 })

      // Should show the option buttons
      await waitFor(() => {
        const buttons = screen.getAllByRole('button')
        const allowBtn = buttons.find(btn => btn.textContent === 'Allow')
        const denyBtn = buttons.find(btn => btn.textContent === 'Deny')
        expect(allowBtn).toBeDefined()
        expect(denyBtn).toBeDefined()
      }, { timeout: 5000 })
    })
  })
})
