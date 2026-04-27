/**
 * Distill Flow Scenario Tests (J1-J2)
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { renderChat } from '../helpers/renderChat'
import type { RenderChatResult } from '../helpers/renderChat'
import type { FixtureEvent } from '../helpers/sseSimulator'

import distillPreview from '../fixtures/scenarios/distill/distill-preview.json'
import distillSave from '../fixtures/scenarios/distill/distill-save.json'

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

// Mock FlowPreview since it pulls in ReactFlow and heavy deps
vi.mock('../../components/FlowPreview', () => ({
  default: ({ yamlContent }: { yamlContent: string }) => (
    <div data-testid="flow-preview">Flow Preview</div>
  ),
}))

describe('Distill Flow Scenarios', () => {
  let result: RenderChatResult

  afterEach(() => {
    if (result) result.cleanup()
  })

  describe('J1: Distill Preview', () => {
    it('renders distill preview card with flow name and description', async () => {
      result = renderChat({
        scenarioEvents: distillPreview.events as FixtureEvent[],
      })

      await result.sendMessage('Distill this conversation')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('research_flow')
        expect(text).toContain('Automated research pipeline')
      }, { timeout: 10000 })
    })

    it('shows tags on the preview card', async () => {
      result = renderChat({
        scenarioEvents: distillPreview.events as FixtureEvent[],
      })

      await result.sendMessage('Distill this conversation')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('research')
        expect(text).toContain('web')
      }, { timeout: 10000 })
    })

    it('shows explanation content', async () => {
      result = renderChat({
        scenarioEvents: distillPreview.events as FixtureEvent[],
      })

      await result.sendMessage('Distill this conversation')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('This flow automates research')
      }, { timeout: 10000 })
    })
  })

  describe('J2: Distill Save', () => {
    it('renders saved confirmation with file path', async () => {
      result = renderChat({
        scenarioEvents: distillSave.events as FixtureEvent[],
      })

      await result.sendMessage('Save the flow')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('research_flow.yaml')
      }, { timeout: 10000 })
    })

    it('shows run command after save', async () => {
      result = renderChat({
        scenarioEvents: distillSave.events as FixtureEvent[],
      })

      await result.sendMessage('Save the flow')

      await waitFor(() => {
        const text = result.container.textContent || ''
        expect(text).toContain('astonish run research_flow')
      }, { timeout: 10000 })
    })
  })
})
