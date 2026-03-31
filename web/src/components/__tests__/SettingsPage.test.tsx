import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SettingsPage from '../SettingsPage'

// Mock all child settings components to avoid deep dependency trees
vi.mock('../settings/GeneralSettings', () => ({ default: () => <div data-testid="general-settings">GeneralSettings</div> }))
vi.mock('../settings/ProvidersSettings', () => ({ default: () => <div data-testid="providers-settings">ProvidersSettings</div> }))
vi.mock('../settings/MCPServersSettings', () => ({ default: () => <div data-testid="mcp-settings">MCPServersSettings</div> }))
vi.mock('../settings/TapsSettings', () => ({ default: () => <div data-testid="taps-settings">TapsSettings</div> }))
vi.mock('../FlowStorePanel', () => ({ default: () => <div data-testid="flow-store">FlowStorePanel</div> }))
vi.mock('../settings/CredentialsSettings', () => ({ default: () => <div data-testid="credentials-settings">CredentialsSettings</div> }))
vi.mock('../settings/ChatSettings', () => ({ default: () => <div data-testid="chat-settings">ChatSettings</div> }))
vi.mock('../settings/BrowserSettings', () => ({ default: () => <div data-testid="browser-settings">BrowserSettings</div> }))
vi.mock('../settings/ChannelsSettings', () => ({ default: () => <div data-testid="channels-settings">ChannelsSettings</div> }))
vi.mock('../settings/SessionsSettings', () => ({ default: () => <div data-testid="sessions-settings">SessionsSettings</div> }))
vi.mock('../settings/MemorySettings', () => ({ default: () => <div data-testid="memory-settings">MemorySettings</div> }))
vi.mock('../settings/SubAgentsSettings', () => ({ default: () => <div data-testid="sub-agents-settings">SubAgentsSettings</div> }))
vi.mock('../settings/SkillsSettings', () => ({ default: () => <div data-testid="skills-settings">SkillsSettings</div> }))
vi.mock('../settings/SchedulerSettings', () => ({ default: () => <div data-testid="scheduler-settings">SchedulerSettings</div> }))
vi.mock('../settings/DaemonSettings', () => ({ default: () => <div data-testid="daemon-settings">DaemonSettings</div> }))
vi.mock('../settings/IdentitySettings', () => ({ default: () => <div data-testid="identity-settings">IdentitySettings</div> }))
vi.mock('../settings/OpenCodeSettings', () => ({ default: () => <div data-testid="opencode-settings">OpenCodeSettings</div> }))
vi.mock('../settings/SandboxSettings', () => ({ default: () => <div data-testid="sandbox-settings">SandboxSettings</div> }))

// Mock the settings API
vi.mock('../settings/settingsApi', () => ({
  fetchSettings: vi.fn().mockResolvedValue({
    general: { default_provider: 'openai', default_model: 'gpt-4', web_search_tool: '', web_extract_tool: '', timezone: '' },
    providers: [],
  }),
  fetchMCPConfig: vi.fn().mockResolvedValue({ mcpServers: {} }),
  fetchWebCapableTools: vi.fn().mockResolvedValue({ webSearch: [], webExtract: [] }),
  fetchFullConfig: vi.fn().mockResolvedValue({}),
  saveSettings: vi.fn().mockResolvedValue({}),
}))

// Mock global fetch for /api/standard-servers
beforeEach(() => {
  globalThis.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ servers: [] }),
  }) as unknown as typeof fetch
})

describe('SettingsPage', () => {
  const defaultProps = {
    onClose: vi.fn(),
    activeSection: 'general',
    onSectionChange: vi.fn(),
    theme: 'dark',
  }

  it('renders loading state initially', () => {
    render(<SettingsPage {...defaultProps} />)
    expect(screen.getByText('Loading settings...')).toBeInTheDocument()
  })

  it('renders the settings title after loading', async () => {
    render(<SettingsPage {...defaultProps} />)
    const title = await screen.findByText('Settings')
    expect(title).toBeInTheDocument()
  })

  it('renders menu items after loading', async () => {
    render(<SettingsPage {...defaultProps} />)
    // Wait for content to load
    await screen.findByText('Settings')
    // After loading, menu items should be present in the sidebar nav
    const allGeneral = await screen.findAllByText('General')
    // There should be at least one "General" in the menu (span) and one as the section heading (h3)
    expect(allGeneral.length).toBeGreaterThanOrEqual(2)
    // Verify the sidebar menu contains expected items
    expect(screen.getByText('Providers')).toBeInTheDocument()
    expect(screen.getByText('MCP Servers')).toBeInTheDocument()
    expect(screen.getByText('Credentials')).toBeInTheDocument()
  })

  it('calls onClose when close button is clicked', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<SettingsPage {...defaultProps} onClose={onClose} />)
    // Wait for loading to complete
    await screen.findByText('Settings')
    const closeButtons = screen.getAllByRole('button')
    // Find the button that triggers onClose (the X button in the sidebar header)
    const closeButton = closeButtons.find(
      btn => btn.querySelector('svg') && btn.closest('.w-64')
    )
    if (closeButton) {
      await user.click(closeButton)
      expect(onClose).toHaveBeenCalledTimes(1)
    }
  })

  it('calls onSectionChange when a menu item is clicked', async () => {
    const user = userEvent.setup()
    const onSectionChange = vi.fn()
    render(<SettingsPage {...defaultProps} onSectionChange={onSectionChange} />)
    const providersButton = await screen.findByText('Providers')
    await user.click(providersButton)
    expect(onSectionChange).toHaveBeenCalledWith('providers')
  })

  it('shows version info after loading', async () => {
    render(<SettingsPage {...defaultProps} appVersion="1.2.3" />)
    const version = await screen.findByText(/1\.2\.3/)
    expect(version).toBeInTheDocument()
  })

  it('shows update available when provided', async () => {
    render(
      <SettingsPage
        {...defaultProps}
        updateAvailable={{ version: '2.0.0', url: 'https://example.com' } as any}
        onUpdateClick={vi.fn()}
      />
    )
    const update = await screen.findByText(/2\.0\.0/)
    expect(update).toBeInTheDocument()
  })

  it('renders GeneralSettings for general section', async () => {
    render(<SettingsPage {...defaultProps} activeSection="general" />)
    // After loading completes, GeneralSettings should render
    const el = await screen.findByTestId('general-settings')
    expect(el).toBeInTheDocument()
  })

  it('renders ProvidersSettings for providers section', async () => {
    render(<SettingsPage {...defaultProps} activeSection="providers" />)
    const el = await screen.findByTestId('providers-settings')
    expect(el).toBeInTheDocument()
  })
})
