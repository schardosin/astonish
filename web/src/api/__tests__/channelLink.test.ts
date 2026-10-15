import { beforeEach, describe, expect, it, vi } from 'vitest'
import { generateChannelLinkCode, linkPrincipal, listLinkedChannels, validEmailLinkCode } from '../channelLink'
import { teamFetch } from '../teamContext'

vi.mock('../teamContext', () => ({ teamFetch: vi.fn() }))
const request = vi.mocked(teamFetch)
beforeEach(() => request.mockReset())

describe('channel linking API contract', () => {
  it('validates canonical Telegram IDs within the backend integer range', () => {
    for (const user_id of ['', '0', '-1', '01', 'alice', '9223372036854775808']) {
      expect(linkPrincipal('telegram', { user_id })).toBeNull()
    }
    expect(linkPrincipal('telegram', { user_id: '9223372036854775807' })).toBe('9223372036854775807')
    expect(linkPrincipal('telegram', { user_id: '123', workspace_id: 'T123' })).toBeNull()
  })

  it('requires workspace-qualified Slack identities without case normalization', () => {
    expect(linkPrincipal('slack', { user_id: 'U123' })).toBeNull()
    expect(linkPrincipal('slack', { user_id: 'u123', workspace_id: 'T123' })).toBeNull()
    expect(linkPrincipal('slack', { user_id: 'U123', workspace_id: 't123' })).toBeNull()
    expect(linkPrincipal('slack', { user_id: 'W123', workspace_id: 'T123' })).toBe('T123:W123')
    expect(linkPrincipal('slack', { user_id: 'U123', workspace_id: 'T999' })).toBe('T999:U123')
  })

  it('accepts only full case-sensitive URL-safe email tokens', () => {
    const token = 'aB_9cD-eF0gH1iJ2kL3mNw'
    expect(validEmailLinkCode(token)).toBe(true)
    expect(validEmailLinkCode(` ${token}\n`)).toBe(true)
    for (const invalid of ['ABC123', token + 'x', token.slice(1), '!'.repeat(22)]) {
      expect(validEmailLinkCode(invalid)).toBe(false)
    }
  })

  it('sends the exact preselected identity through authenticated team transport', async () => {
    const identity = { user_id: 'U123', workspace_id: 'T123' }
    const result = { code: 'aB_9cD-eF0gH1iJ2kL3mNw', expires_in: 300 }
    request.mockResolvedValue(new Response(JSON.stringify(result)))
    expect(await generateChannelLinkCode('slack', identity)).toEqual(result)
    expect(request).toHaveBeenCalledWith('/api/user/channels/link-code', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'include',
      body: JSON.stringify({ channel_type: 'slack', expected_identity: identity }),
    })
  })

  it('rejects invalid preselection before requesting any token', async () => {
    await expect(generateChannelLinkCode('slack', { user_id: 'U123' })).rejects.toThrow('valid expected slack identity')
    expect(request).not.toHaveBeenCalled()
  })

  it('propagates backend errors and parses the linked channel response', async () => {
    request.mockResolvedValueOnce(new Response(JSON.stringify({ error: 'Channel unavailable' }), { status: 400 }))
    await expect(generateChannelLinkCode('telegram', { user_id: '123' })).rejects.toThrow('Channel unavailable')
    request.mockResolvedValueOnce(new Response(JSON.stringify({ channels: [{ external_id: 'T123:U123' }] })))
    expect(await listLinkedChannels()).toEqual([{ external_id: 'T123:U123' }])
    request.mockResolvedValueOnce(new Response('', { status: 403 }))
    await expect(listLinkedChannels()).rejects.toThrow('Failed to load linked channels')
  })
})
