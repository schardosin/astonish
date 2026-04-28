/**
 * Panel Management Scenario Tests (N1-N4)
 *
 * Tests toolbar panel buttons (Todo, Files, Apps) and their visibility
 * based on session state (e.g., Files button appears when artifacts exist).
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'

// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'

import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures — reuse artifact-created to get sessionArtifacts populated
import artifactCreated from '../fixtures/scenarios/downloads/artifact-created.json'

describe('Panel Management Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('N1: Files button visibility', () => {
    it('shows Files button when artifacts are present', async () => {
      result = renderChat({
        scenarioEvents: artifactCreated.events as FixtureEvent[],
      })

      await result.sendMessage('Create')

      // After the artifact event is processed, sessionArtifacts.length > 0
      // which causes the Files button to render in the toolbar
      await waitFor(() => {
        const buttons = result.container.querySelectorAll('button')
        const filesButton = Array.from(buttons).find(btn =>
          btn.textContent?.includes('Files')
        )
        expect(filesButton).toBeDefined()
      }, { timeout: 10000 })
    })
  })
})
