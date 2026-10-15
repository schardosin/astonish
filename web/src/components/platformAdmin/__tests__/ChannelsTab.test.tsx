import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import ChannelsTab from '../ChannelsTab'
import * as adminApi from '../../../api/platformAdmin'

vi.mock('../../../api/platformAdmin', () => ({
  listChannels: vi.fn(), saveChannel: vi.fn(), deleteChannel: vi.fn(), testEmailConnection: vi.fn(),
}))

describe('email receiver authentication settings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(adminApi.saveChannel).mockResolvedValue({ message: 'Saved' })
  })

  it.each(['imap', 'msgraph'])('saves and restores receiver identity for %s', async (provider) => {
    const user = userEvent.setup()
    const channel = { type: 'email', description: 'Email', enabled: true, config: { provider, trusted_authserv_id: 'receiver.example', sender_auth_mode: 'strict' }, secrets: [], secrets_configured: true }
    vi.mocked(adminApi.listChannels).mockResolvedValue([channel])
    render(<ChannelsTab />)
    await user.click((await screen.findAllByText('Email', { exact: true }))[0])
    expect(screen.getByLabelText('Sender authentication')).toHaveValue('strict')
    const input = screen.getByLabelText('Trusted mail receiver authserv-id')
    expect(input).toHaveValue('receiver.example')
    await user.clear(input)
    expect(screen.getByRole('alert')).toHaveTextContent('Configure a trusted authserv-id')
    await user.type(input, 'new.receiver.example')
    vi.mocked(adminApi.listChannels).mockResolvedValue([{ ...channel, config: { ...channel.config, trusted_authserv_id: 'new.receiver.example' } }])
    await user.click(screen.getByRole('button', { name: /save/i }))
    await waitFor(() => expect(adminApi.saveChannel).toHaveBeenCalledWith('email', expect.objectContaining({ config: expect.objectContaining({ trusted_authserv_id: 'new.receiver.example', sender_auth_mode: 'strict' }) })))
    expect(await screen.findByLabelText('Trusted mail receiver authserv-id')).toHaveValue('new.receiver.example')
  })

  it('saves provider-trust without requiring an authserv-id', async () => {
    const user = userEvent.setup()
    const channel = { type: 'email', description: 'Email', enabled: true, config: { provider: 'msgraph', sender_auth_mode: 'strict', trusted_authserv_id: 'receiver.example' }, secrets: [], secrets_configured: true }
    vi.mocked(adminApi.listChannels).mockResolvedValue([channel])
    render(<ChannelsTab />)
    await user.click((await screen.findAllByText('Email', { exact: true }))[0])
    await user.selectOptions(screen.getByLabelText('Sender authentication'), 'provider-trust')
    expect(screen.getByRole('alert')).toHaveTextContent('Spoofed or unauthenticated mail')
    expect(screen.queryByLabelText('Trusted mail receiver authserv-id')).not.toBeInTheDocument()
    vi.mocked(adminApi.listChannels).mockResolvedValue([{ ...channel, config: { provider: 'msgraph', sender_auth_mode: 'provider-trust' } }])
    await user.click(screen.getByRole('button', { name: /save/i }))
    await waitFor(() => expect(adminApi.saveChannel).toHaveBeenCalledWith('email', expect.objectContaining({ config: expect.objectContaining({ sender_auth_mode: 'provider-trust' }) })))
    expect(screen.getByLabelText('Sender authentication')).toHaveValue('provider-trust')
  })
})
