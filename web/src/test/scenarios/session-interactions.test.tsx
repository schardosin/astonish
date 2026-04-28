/**
 * Session Interaction Scenario Tests (S3, S5)
 *
 * Tests session management interactions: deleting sessions from the sidebar
 * and creating new sessions via the new conversation button.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { waitFor } from '@testing-library/react'
// Shared mocks (react-markdown, remark-gfm, HomePage, FleetStartDialog, FleetTemplatePicker, MermaidBlock)
import './scenarioSetup'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

// Fixtures
import simpleQa from '../fixtures/scenarios/core/simple-qa.json'

describe('Session Interaction Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('S3: Session Delete', () => {
    it('removes session from sidebar when delete is clicked', async () => {
      result = renderChat({
        sessions: [
          { id: 'sess-del', title: 'Delete Me', createdAt: '2024-01-01', updatedAt: '2024-01-01', messageCount: 1 },
        ],
      })

      // Wait for "Delete Me" to appear in the sidebar
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Delete Me')
      }, { timeout: 10000 })

      // Find the delete button (it's a div with role="button" and title="Delete conversation")
      const deleteBtn = result.container.querySelector('[title="Delete conversation"]')
      expect(deleteBtn).not.toBeNull()

      // Click the delete button
      await result.user.click(deleteBtn as HTMLElement)

      // Wait for "Delete Me" to be removed from the sidebar
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).not.toContain('Delete Me')
      }, { timeout: 10000 })
    })
  })

  describe('S5: New Session Button', () => {
    it('clears state when new session button is clicked', async () => {
      result = renderChat({
        scenarioEvents: simpleQa.events as FixtureEvent[],
        sessions: [
          { id: 'sess-existing', title: 'Existing Chat', createdAt: '2024-01-01', updatedAt: '2024-01-01', messageCount: 1 },
        ],
      })

      // Send a message and wait for agent response
      await result.sendMessage('What is Go?')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('Go is a statically typed language')
      }, { timeout: 10000 })

      // Find the "New conversation" button (title="New conversation")
      const newBtn = result.container.querySelector('button[title="New conversation"]')
      expect(newBtn).not.toBeNull()

      // Click the new conversation button
      await result.user.click(newBtn as HTMLElement)

      // Wait for the agent response to be gone (messages cleared)
      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).not.toContain('Go is a statically typed language')
      }, { timeout: 10000 })

      // HomePage should reappear
      await waitFor(() => {
        const homePage = result.container.querySelector('[data-testid="home-page"]')
        expect(homePage).not.toBeNull()
      }, { timeout: 10000 })
    })
  })
})
