/**
 * App Preview Scenario Tests (G1-G3)
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import appGenerated from '../fixtures/scenarios/apps/app-generated.json'
import appVersionNav from '../fixtures/scenarios/apps/app-version-navigation.json'
import appSavedConfirmation from '../fixtures/scenarios/apps/app-saved-confirmation.json'

// Standard mocks
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

// Mock AppPreview — uses iframes which aren't available in jsdom
vi.mock('../../components/chat/AppPreview', () => ({
  default: ({ code }: { code: string }) => (
    <div data-testid="app-preview-iframe">{code}</div>
  ),
}))

describe('App Preview Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) {
      result.cleanup()
    }
  })

  describe('G1: App Generated', () => {
    it('renders app preview card with title', async () => {
      result = renderChat({
        scenarioEvents: appGenerated.events as FixtureEvent[],
      })

      await result.sendMessage('Generate a dashboard')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain('Revenue Dashboard')
      }, { timeout: 10000 })
    })

    it('renders agent text before app preview', async () => {
      result = renderChat({
        scenarioEvents: appGenerated.events as FixtureEvent[],
      })

      await result.sendMessage('Generate a dashboard')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain("Here's the app")
      }, { timeout: 10000 })
    })
  })

  describe('G2: App Version Navigation', () => {
    it('renders latest version of the app', async () => {
      result = renderChat({
        scenarioEvents: appVersionNav.events as FixtureEvent[],
      })

      await result.sendMessage('Make a dashboard')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain('Dashboard')
      }, { timeout: 10000 })
    })

    it('shows version count', async () => {
      result = renderChat({
        scenarioEvents: appVersionNav.events as FixtureEvent[],
      })

      await result.sendMessage('Make a dashboard')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain('2')
      }, { timeout: 10000 })
    })
  })

  describe('G3: App Saved', () => {
    it('renders app saved confirmation with name', async () => {
      result = renderChat({
        scenarioEvents: appSavedConfirmation.events as FixtureEvent[],
      })

      await result.sendMessage('Save the app')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain('My Dashboard')
      }, { timeout: 10000 })
    })

    it('shows saved indicator', async () => {
      result = renderChat({
        scenarioEvents: appSavedConfirmation.events as FixtureEvent[],
      })

      await result.sendMessage('Save the app')

      await waitFor(() => {
        const text = result.container.textContent
        expect(text).toContain('App Saved')
      }, { timeout: 10000 })
    })
  })
})
