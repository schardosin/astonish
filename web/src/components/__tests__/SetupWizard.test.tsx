import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
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

// Keep the real platform wrappers: these tests exercise the rendered UI through fetch.
const fetchMock = vi.fn<typeof fetch>()
beforeEach(() => {
  fetchMock.mockReset().mockResolvedValue(new Response(JSON.stringify({ providers: [] })))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe.each([
  { name: 'SQLite single user', backend: /SQLite \(Recommended\)/, auth: 'none', url: '/api/platform/init/sqlite' },
  { name: 'SQLite multi-user', backend: /SQLite \(Recommended\)/, auth: 'builtin', url: '/api/platform/init/sqlite' },
  { name: 'PostgreSQL', backend: /PostgreSQL/, auth: null, url: '/api/platform/init' },
])('SetupWizard bootstrap: $name', ({ backend, auth, url }) => {
  it('gates setup, sends header-only authority, permits retry, and clears on success', async () => {
    const storageWrite = vi.spyOn(Storage.prototype, 'setItem')
    await act(async () => { render(<SetupWizard onComplete={vi.fn()} />) })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(screen.queryByLabelText('Bootstrap Token')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: backend }))
    const token = screen.getByLabelText('Bootstrap Token')
    expect(token).toHaveAttribute('type', 'password')
    expect(token).toHaveAttribute('autocomplete', 'off')
    expect(token).toHaveValue('')
    expect(screen.getByText(/Enter the ASTONISH_BOOTSTRAP_TOKEN/)).toBeInTheDocument()
    if (auth === 'builtin') {
      fireEvent.click(screen.getByRole('button', { name: /Multi-user Create/ }))
      fireEvent.change(screen.getByPlaceholderText('admin@example.com'), { target: { value: 'admin@example.com' } })
      fireEvent.change(screen.getByPlaceholderText('Minimum 8 characters'), { target: { value: 'admin-password' } })
    } else if (auth === null) {
      // Existing PG fields predate label associations; select its password distinctly from the token.
      const password = document.querySelector('input[type="password"]:not(#bootstrap-token)')!
      fireEvent.change(password, { target: { value: 'database-password' } })
    }
    const submit = screen.getByRole('button', { name: 'Initialize Platform' })
    expect(submit).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
    fetchMock.mockClear()
    fireEvent.click(submit)
    expect(fetchMock).not.toHaveBeenCalled()
    fireEvent.change(token, { target: { value: '   ' } })
    expect(submit).toBeDisabled()
    fireEvent.change(token, { target: { value: 'wrong-bootstrap-token' } })
    expect(submit).toBeEnabled()

    let respond!: (response: Response) => void
    fetchMock.mockImplementationOnce(() => new Promise<Response>(resolve => { respond = resolve }))
    fireEvent.click(submit)
    expect(screen.getByRole('button', { name: 'Initializing Platform...' })).toBeDisabled()
    expect(token).toBeDisabled()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await act(async () => {
      respond(new Response(JSON.stringify({ error: 'Explicit bootstrap authorization required' }), { status: 403 }))
    })
    expect(screen.getByText('Explicit bootstrap authorization required')).toBeInTheDocument()
    expect(token).toHaveValue('wrong-bootstrap-token')
    expect(token).toBeEnabled()
    expect(submit).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()

    fireEvent.change(token, { target: { value: 'correct-bootstrap-token' } })
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ success: true, restart_required: true })))
    await act(async () => { fireEvent.click(submit) })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    for (const [index, [requestURL, request]] of fetchMock.mock.calls.entries()) {
      expect(requestURL).toBe(url)
      expect(new Headers(request?.headers).get('X-Astonish-Bootstrap-Token')).toBe(index === 0 ? 'wrong-bootstrap-token' : 'correct-bootstrap-token')
      expect(request?.body).not.toContain('bootstrap')
      const body = JSON.parse(request?.body as string)
      expect(body.org_slug).toBe('my-org')
      if (auth !== null) expect(body.auth_mode).toBe(auth)
    }
    expect(storageWrite).not.toHaveBeenCalled()
    expect(token).toHaveValue('')
    expect(token).toBeDisabled()
    expect(screen.getByText('Platform Initialized')).toBeInTheDocument()
    expect(screen.getByText('Restart Required')).toBeInTheDocument()
    expect(screen.queryByText('Explicit bootstrap authorization required')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(screen.getByText('Select Provider')).toBeInTheDocument()
  })

  it('does not restore the token after the wizard unmounts', async () => {
    let unmount!: () => void
    await act(async () => { ({ unmount } = render(<SetupWizard onComplete={vi.fn()} />)) })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    fireEvent.click(screen.getByRole('button', { name: backend }))
    fireEvent.change(screen.getByLabelText('Bootstrap Token'), { target: { value: 'memory-only-token' } })
    unmount()
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ providers: [] })))
    await act(async () => { render(<SetupWizard onComplete={vi.fn()} />) })
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    fireEvent.click(screen.getByRole('button', { name: backend }))
    expect(screen.getByLabelText('Bootstrap Token')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Initialize Platform' })).toBeDisabled()
  })
})

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
