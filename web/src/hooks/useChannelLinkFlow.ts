import { useState, useEffect, useRef, useCallback } from 'react'
import { generateChannelLinkCode, listLinkedChannels, linkPrincipal, type LinkIdentity } from '../api/channelLink'

interface UserChannel {
  id: string
  user_id: string
  channel_type: string
  external_id: string
  display_name: string
  enabled: boolean
  verified: boolean
  verified_at?: string
  created_at: string
}

interface LinkFlowState {
  /** The link code (for telegram/slack) or null */
  code: string | null
  /** Whether an async operation is in progress */
  loading: boolean
  /** Whether the code was copied to clipboard */
  copied: boolean
  /** Countdown seconds remaining */
  timeLeft: number
  /** Whether the flow is active (code generated, waiting for link) */
  isActive: boolean
}

interface LinkFlowActions {
  /** Generate a link code for this channel */
  generateCode: () => Promise<void>
  /** Copy the link command to clipboard */
  copyCode: () => void
  /** Cancel the current link flow */
  cancel: () => void
}

interface UseChannelLinkFlowOptions {
  /** Channel type: 'telegram' | 'slack' */
  channelType: 'telegram' | 'slack'
  expectedIdentity: LinkIdentity
  /** Current list of user channels (for detecting new links) */
  channels: UserChannel[]
  /** Called when linking succeeds (e.g., to refresh channel list) */
  onLinked: () => void
  /** Called when an error occurs */
  onError: (message: string) => void
  /** Called when linking succeeds with a success message */
  onSuccess: (message: string) => void
}

/**
 * useChannelLinkFlow encapsulates the link code generation, countdown timer,
 * and poll-for-completion flow used by Telegram and Slack channel linking.
 *
 * Eliminates duplicated state (code, loading, copied, expiresAt, timeLeft)
 * and duplicated countdown/poll logic across channel types.
 */
export function useChannelLinkFlow({
  channelType,
  expectedIdentity,
  channels,
  onLinked,
  onError,
  onSuccess,
}: UseChannelLinkFlowOptions): [LinkFlowState, LinkFlowActions] {
  const callbacks = useRef({ onLinked, onSuccess })
  useEffect(() => { callbacks.current = { onLinked, onSuccess } }, [onLinked, onSuccess])
  const [expectedPrincipal, setExpectedPrincipal] = useState<string | null>(null)
  const [code, setCode] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [copied, setCopied] = useState(false)
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const [timeLeft, setTimeLeft] = useState<number>(0)

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Countdown timer for code expiry
  useEffect(() => {
    if (expiresAt === null) {
      setTimeLeft(0)
      return
    }
    const update = () => {
      const remaining = Math.max(0, Math.floor((expiresAt - Date.now()) / 1000))
      setTimeLeft(remaining)
      if (remaining === 0) {
        setCode(null)
        setExpiresAt(null)
        if (pollRef.current) clearInterval(pollRef.current)
      }
    }
    update()
    timerRef.current = setInterval(update, 1000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [expiresAt])

  // Poll for link completion when code is active
  useEffect(() => {
    if (!code) return

    pollRef.current = setInterval(async () => {
      try {
        const linkedChannels = await listLinkedChannels()
        const linkedChannel = linkedChannels.find(
          (c: UserChannel) => c.channel_type === channelType && c.external_id === expectedPrincipal && c.verified && c.enabled
        )
        if (linkedChannel) {
          setCode(null)
          setExpiresAt(null)
          if (pollRef.current) clearInterval(pollRef.current)
          callbacks.current.onLinked()
          callbacks.current.onSuccess(`${channelType === 'telegram' ? 'Telegram' : 'Slack'} linked and verified successfully!`)
        }
      } catch { /* ignore poll errors */ }
    }, 3000)

    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [code, channelType, expectedPrincipal])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const generateCode = useCallback(async () => {
    setLoading(true)
    try {
      const data = await generateChannelLinkCode(channelType, expectedIdentity)
      setExpectedPrincipal(linkPrincipal(channelType, expectedIdentity))
      const codeValue = data.code
      if (!codeValue && channelType === 'telegram' && !data.bot_username) {
        throw new Error('Telegram bot is not connected. Contact your administrator.')
      }
      if (!codeValue && channelType === 'slack') {
        throw new Error('Slack bot is not connected. Contact your administrator.')
      }
      setCode(codeValue || data.code)
      setExpiresAt(Date.now() + (data.expires_in || 300) * 1000)
      setCopied(false)
    } catch (err: unknown) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [channelType, expectedIdentity, onError])

  const copyCode = useCallback(() => {
    if (!code) return
    navigator.clipboard.writeText(`/link ${code}`)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [code])

  const cancel = useCallback(() => {
    setCode(null)
    setExpiresAt(null)
    if (pollRef.current) clearInterval(pollRef.current)
  }, [])

  const state: LinkFlowState = {
    code,
    loading,
    copied,
    timeLeft,
    isActive: code !== null,
  }

  const actions: LinkFlowActions = {
    generateCode,
    copyCode,
    cancel,
  }

  return [state, actions]
}
