import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useChannelLinkFlow } from '../useChannelLinkFlow'

const linkedSlackChannel = {
  id: 'slack-1',
  user_id: 'user-1',
  channel_type: 'slack',
  external_id: 'T123:U123',
  display_name: 'alice',
  enabled: true,
  verified: true,
  created_at: '2026-08-04T00:00:00Z',
}

describe('useChannelLinkFlow', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('does not complete for a different workspace and keeps polling across rerenders', async () => {
    vi.useFakeTimers()
    const onLinked = vi.fn()
    const fetchMock = vi.fn(async (url: string) => new Response(JSON.stringify(url.endsWith('link-code')
      ? { code: 'aB_9cD-eF0gH1iJ2kL3mNw', expires_in: 300 }
      : { channels: [linkedSlackChannel] })))
    vi.stubGlobal('fetch', fetchMock)
    const { result, rerender } = renderHook(() => useChannelLinkFlow({
      channelType: 'slack', expectedIdentity: { user_id: 'UOTHER' }, channels: [],
      onLinked, onSuccess: () => {}, onError: () => {},
    }))
    await act(async () => { await result.current[1].generateCode() })
    for (let i = 0; i < 4; i++) {
      await act(async () => { vi.advanceTimersByTime(1000) })
      rerender()
    }
    expect(fetchMock).toHaveBeenCalledWith('/api/user/channels', expect.anything())
    expect(onLinked).not.toHaveBeenCalled()
    expect(result.current[0].isActive).toBe(true)
  })

  it('detects completion when the linked channel is already present in the initial list', async () => {
    vi.useFakeTimers()
    const onLinked = vi.fn()
    const onSuccess = vi.fn()
    const onError = vi.fn()

    let call = 0
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      call += 1
      if (url === '/api/user/channels/link-code') {
        return new Response(JSON.stringify({ code: 'ABC123', expires_in: 300 }), { status: 200 })
      }
      return new Response(JSON.stringify({ channels: [linkedSlackChannel] }), { status: 200 })
    }))

    const { result } = renderHook(() => useChannelLinkFlow({
      expectedIdentity: { user_id: 'U123' },
      channelType: 'slack',
      channels: [linkedSlackChannel],
      onLinked,
      onError,
      onSuccess,
    }))

    await act(async () => {
      await result.current[1].generateCode()
    })

    await act(async () => {
      vi.advanceTimersByTime(3000)
    })

    expect(onLinked).toHaveBeenCalledTimes(1)
    expect(onSuccess).toHaveBeenCalledWith('Slack linked and verified successfully!')
    expect(onError).not.toHaveBeenCalled()
    expect(call).toBeGreaterThanOrEqual(2)
  })
})
