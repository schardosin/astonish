import { describe, it, expect, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import SetupWizard from '../SetupWizard'

// Mock the API modules
vi.mock('../../api/agents', () => ({
  fetchStandardServers: vi.fn().mockResolvedValue([]),
  installStandardServer: vi.fn().mockResolvedValue({}),
}))

vi.mock('../../api/sandbox', () => ({
  fetchSandboxStatus: vi.fn().mockResolvedValue({ available: false }),
  fetchOptionalTools: vi.fn().mockResolvedValue([]),
  initSandbox: vi.fn().mockResolvedValue({}),
}))

// Mock fetch for /api/settings/config
globalThis.fetch = vi.fn().mockResolvedValue({
  ok: true,
  json: () => Promise.resolve({ providers: [] }),
}) as unknown as typeof fetch

describe('SetupWizard', () => {
  const onComplete = vi.fn()

  it('renders the welcome step', async () => {
    render(<SetupWizard onComplete={onComplete} />)
    expect(screen.getByText('Welcome to Astonish Studio')).toBeInTheDocument()
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })

  it('renders step indicators', async () => {
    const { container } = render(<SetupWizard onComplete={onComplete} />)
    // Step indicators are a row of dots. 9 steps total.
    const dots = container.querySelectorAll('[class*="rounded-full"]')
    expect(dots.length).toBeGreaterThanOrEqual(9)
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })

  it('shows the Continue button on the welcome step', async () => {
    render(<SetupWizard onComplete={onComplete} />)
    expect(screen.getByText('Continue')).toBeInTheDocument()
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })

  it('has Back button invisible or disabled on the first step', async () => {
    render(<SetupWizard onComplete={onComplete} />)
    const backButton = screen.queryByText('Back')
    // On step 0, the Back button exists but should be non-functional
    // (either invisible, disabled, or pointer-events-none)
    if (backButton) {
      const parent = backButton.closest('button')
      const className = parent?.className ?? ''
      const isHidden = /invisible|opacity-0|hidden|pointer-events-none/.test(className)
      const isDisabled = parent?.disabled
      expect(isHidden || isDisabled).toBe(true)
    }
    // If no Back button at all, that's also valid
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })

  it('renders the three overview bullet points on welcome step', async () => {
    render(<SetupWizard onComplete={onComplete} />)
    // The welcome step describes 3 things: connect provider, web search, browser & sandbox
    expect(screen.getByText(/Connect.*provider/i)).toBeInTheDocument()
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })

  it('renders as a full-screen overlay', async () => {
    const { container } = render(<SetupWizard onComplete={onComplete} />)
    const overlay = container.firstElementChild
    expect(overlay?.className).toContain('fixed')
    expect(overlay?.className).toContain('z-')
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0))
    })
  })
})
