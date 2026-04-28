/**
 * Shared vi.mock() declarations for scenario tests.
 *
 * These mocks are needed by every scenario test to avoid ESM/jsdom issues
 * with react-markdown and to stub heavy sub-components that aren't relevant
 * to SSE event handling tests.
 *
 * Import this file at the top of each scenario test:
 *   import '../scenarioSetup'
 *
 * Vitest hoists vi.mock() calls, so they run before any other code.
 *
 * If a scenario test needs additional mocks (e.g. AppPreview, FlowPreview),
 * add them in the test file itself — they will also be hoisted.
 */
import { vi } from 'vitest'

// ESM-only modules that don't work in jsdom
vi.mock('react-markdown', () => ({
  default: ({ children }: { children: string }) => <span data-testid="markdown">{children}</span>,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))

// Heavy sub-components that aren't needed for SSE scenario testing
vi.mock('../../components/HomePage', () => ({
  default: () => <div data-testid="home-page">HomePage</div>,
}))
vi.mock('../../components/chat/FleetStartDialog', () => ({ default: () => null }))
vi.mock('../../components/chat/FleetTemplatePicker', () => ({ default: () => null }))
vi.mock('../../components/chat/MermaidBlock', () => ({
  default: ({ chart }: { chart: string }) => <pre data-testid="mermaid">{chart}</pre>,
}))
