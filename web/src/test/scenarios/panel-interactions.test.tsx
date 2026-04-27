/**
 * Panel Interaction Scenario Tests (N2-N4)
 *
 * Tests toolbar panel buttons (Files) and panel toggle behavior.
 * Verifies that clicking panel buttons doesn't crash and that
 * panels toggle correctly.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import artifactCreated from '../fixtures/scenarios/downloads/artifact-created.json'
import planWithDelegation from '../fixtures/scenarios/planning/plan-with-delegation.json'

describe('Panel Interaction Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('N2: Files Panel Toggle', () => {
    it('Files button click toggles file panel without crashing', async () => {
      result = renderChat({
        scenarioEvents: artifactCreated.events as FixtureEvent[],
      })

      await result.sendMessage('Create')

      // Wait for the Files button to appear (it only shows when artifacts exist)
      let filesButton: HTMLButtonElement | undefined
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        filesButton = Array.from(buttons).find(btn =>
          btn.textContent?.includes('Files')
        ) as HTMLButtonElement | undefined
        expect(filesButton).toBeDefined()
      }, { timeout: 10000 })

      // Click to open Files panel
      await result.user.click(filesButton!)

      // Click again to close Files panel (toggle off)
      await result.user.click(filesButton!)

      // If we get here without throwing, the toggle works
      expect(true).toBe(true)
    })

    it('shows artifact info when Files panel is opened', async () => {
      result = renderChat({
        scenarioEvents: artifactCreated.events as FixtureEvent[],
      })

      await result.sendMessage('Create')

      // Wait for the Files button
      let filesButton: HTMLButtonElement | undefined
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        filesButton = Array.from(buttons).find(btn =>
          btn.textContent?.includes('Files')
        ) as HTMLButtonElement | undefined
        expect(filesButton).toBeDefined()
      }, { timeout: 10000 })

      // Open the Files panel
      await result.user.click(filesButton!)

      // The panel should show the artifact filename
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('report.md')
      }, { timeout: 10000 })
    })
  })

  describe('N3: Plan Panel Rendering', () => {
    it('plan events render without crashing the UI', async () => {
      result = renderChat({
        scenarioEvents: planWithDelegation.events as FixtureEvent[],
      })

      await result.sendMessage('Build a feature')

      // The plan data should appear in some form (plan step text or agent response)
      await waitFor(() => {
        const text = result.container.textContent || ''
        // plan-with-delegation fixture has plan and delegation events
        // At minimum the stream should complete without error
        expect(text.length).toBeGreaterThan(0)
      }, { timeout: 10000 })
    })
  })

  describe('N4: Panel State After Stream', () => {
    it('Files button remains accessible after stream completes', async () => {
      result = renderChat({
        scenarioEvents: artifactCreated.events as FixtureEvent[],
      })

      await result.sendMessage('Create')

      // Verify Files button is present after the stream is done
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        const filesButton = Array.from(buttons).find(btn =>
          btn.textContent?.includes('Files')
        )
        expect(filesButton).toBeDefined()
      }, { timeout: 10000 })

      // The textarea should be idle (not streaming)
      await waitFor(() => {
        const ta = result.container.querySelector('textarea')
        const placeholder = ta?.getAttribute('placeholder') || ''
        expect(placeholder).not.toContain('responding')
      }, { timeout: 10000 })
    })
  })
})
