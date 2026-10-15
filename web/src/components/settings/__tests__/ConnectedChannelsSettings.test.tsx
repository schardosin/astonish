import { fireEvent, render, screen, waitFor, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import ConnectedChannelsSettings from '../ConnectedChannelsSettings'

vi.mock('../../../api/teamContext', () => ({ teamFetch: (...args: unknown[]) => fetch(...args as Parameters<typeof fetch>) }))

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('ConnectedChannelsSettings verification', () => {
  it.each(['telegram', 'slack'] as const)('requires and sends the preselected %s identity', async (channel) => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/channels/info') return new Response(JSON.stringify({ telegram: { configured: true, bot_username: 'test_bot' }, slack: { configured: true } }))
      if (url === '/api/user/channels/link-code') return new Response(JSON.stringify({ code: 'aB_9cD-eF0gH1iJ2kL3mNw', expires_in: 300 }))
      return new Response(JSON.stringify({ channels: [] }))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<ConnectedChannelsSettings />)
    const button = await screen.findByRole('button', { name: channel === 'telegram' ? /Link Telegram/ : /Link Slack/ })
    expect((button as HTMLButtonElement).disabled).toBe(true)
    if (channel === 'telegram') fireEvent.change(screen.getByLabelText('Your Telegram user ID'), { target: { value: '12345' } })
    else {
      fireEvent.change(screen.getByLabelText('Your Slack member ID'), { target: { value: 'U123' } })
      expect((button as HTMLButtonElement).disabled).toBe(true)
      fireEvent.change(screen.getByLabelText('Slack workspace ID'), { target: { value: 'T123' } })
    }
    expect((button as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(button)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/user/channels/link-code', expect.objectContaining({
      body: JSON.stringify({ channel_type: channel, expected_identity: channel === 'telegram' ? { user_id: '12345' } : { user_id: 'U123', workspace_id: 'T123' } }),
    })))
  })

  it('submits the exact full mixed-case pasted email token', async () => {
    const token = 'aB_9cD-eF0gH1iJ2kL3mNw'
    const fetchMock = vi.fn(async (url: string) => {
      if (url === '/api/channels/info') return new Response(JSON.stringify({ telegram: {}, email: { configured: true } }))
      if (url === '/api/user/channels/link-code') return new Response(JSON.stringify({ email: 'me@example.com', expires_in: 300 }))
      return new Response(JSON.stringify({ channels: [] }))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<ConnectedChannelsSettings />)
    fireEvent.click(await screen.findByRole('button', { name: /Link Email/i }))
    fireEvent.change(screen.getByPlaceholderText('you@example.com'), { target: { value: 'me@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: /Send.*Code/i }))
    await screen.findByText(/A verification code was sent to/)
    const input = screen.getByRole('textbox')
    const verify = screen.getByRole('button', { name: /^Verify$/i }) as HTMLButtonElement
    for (const invalid of ['', 'ABC123', token + 'x', '!'.repeat(22)]) {
      fireEvent.change(input, { target: { value: invalid } })
      expect(verify.disabled).toBe(true)
    }
    const user = userEvent.setup()
    await user.clear(input)
    await user.click(input)
    await user.paste(token)
    expect((input as HTMLInputElement).value).toBe(token)
    expect(input.getAttribute('maxlength')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /^Verify$/i }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/user/channels/verify-email-code', expect.objectContaining({ body: JSON.stringify({ code: token }) })))
  })
})
