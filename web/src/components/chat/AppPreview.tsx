import { useRef, useEffect, useCallback, useState } from 'react'
import { fetchAppData, fetchAppAction } from '../../api/apps'

const SANDBOX_URL = '/api/app-preview/sandbox'

interface AppPreviewProps {
  code: string
  maxHeight?: number
  /** Optional app name — used for saved app data source lookups */
  appName?: string
}

function detectTheme(): 'dark' | 'light' {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

export default function AppPreview({ code, maxHeight = 500, appName = '' }: AppPreviewProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState(200)
  const [error, setError] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const pendingCode = useRef<string | null>(null)
  // Track polling intervals for data_subscribe messages
  const pollingIntervals = useRef<Map<string, ReturnType<typeof setInterval>>>(new Map())

  const sendToIframe = useCallback((data: Record<string, unknown>) => {
    if (iframeRef.current?.contentWindow) {
      iframeRef.current.contentWindow.postMessage(data, '*')
    }
  }, [])

  const sendCode = useCallback((codeToSend: string) => {
    setError(null)
    sendToIframe({ type: 'render', code: codeToSend })
  }, [sendToIframe])

  const sendTheme = useCallback(() => {
    sendToIframe({ type: 'theme', mode: detectTheme() })
  }, [sendToIframe])

  // Clean up polling intervals on unmount
  useEffect(() => {
    const intervals = pollingIntervals.current
    return () => {
      intervals.forEach((id) => clearInterval(id))
      intervals.clear()
    }
  }, [])

  useEffect(() => {
    function handleMessage(e: MessageEvent) {
      if (e.source !== iframeRef.current?.contentWindow) return
      const msg = e.data
      if (!msg || !msg.type) return

      switch (msg.type) {
        case 'sandbox_ready':
          setReady(true)
          sendTheme()
          if (pendingCode.current) {
            sendCode(pendingCode.current)
            pendingCode.current = null
          }
          break
        case 'render_success':
          setError(null)
          if (msg.height && typeof msg.height === 'number') {
            setHeight(Math.min(msg.height + 2, maxHeight))
          }
          break
        case 'render_error':
          setError(msg.error || 'Unknown render error')
          break

        // ── Data proxy relay ──────────────────────────────────────
        case 'data_request':
          handleDataRequest(msg)
          break
        case 'action_request':
          handleActionRequest(msg)
          break
        case 'data_subscribe':
          handleDataSubscribe(msg)
          break
        case 'data_unsubscribe':
          handleDataUnsubscribe(msg)
          break
      }
    }

    // Relay data_request from iframe → backend → iframe
    async function handleDataRequest(msg: Record<string, unknown>) {
      const { sourceId, args, requestId } = msg as {
        sourceId: string
        args: Record<string, unknown>
        requestId: string
      }
      try {
        const resp = await fetchAppData(sourceId, args || {}, requestId, appName)
        sendToIframe({
          type: 'data_response',
          requestId: resp.requestId || requestId,
          data: resp.data,
          error: resp.error,
        })
      } catch (err: unknown) {
        sendToIframe({
          type: 'data_response',
          requestId,
          error: err instanceof Error ? err.message : 'Data request failed',
        })
      }
    }

    // Relay action_request from iframe → backend → iframe
    async function handleActionRequest(msg: Record<string, unknown>) {
      const { actionId, payload, requestId } = msg as {
        actionId: string
        payload: Record<string, unknown>
        requestId: string
      }
      try {
        const resp = await fetchAppAction(actionId, payload || {}, requestId)
        sendToIframe({
          type: 'action_response',
          requestId: resp.requestId || requestId,
          data: resp.data,
          error: resp.error,
        })
      } catch (err: unknown) {
        sendToIframe({
          type: 'action_response',
          requestId,
          error: err instanceof Error ? err.message : 'Action request failed',
        })
      }
    }

    // Set up polling for data_subscribe messages from iframe
    function handleDataSubscribe(msg: Record<string, unknown>) {
      const { sourceId, args, interval } = msg as {
        sourceId: string
        args: Record<string, unknown>
        interval: number
      }
      // Clear existing interval for this sourceId
      const existing = pollingIntervals.current.get(sourceId)
      if (existing) clearInterval(existing)

      // Minimum 5s interval
      const ms = Math.max(interval || 30000, 5000)

      const id = setInterval(async () => {
        try {
          const resp = await fetchAppData(sourceId, args || {}, '', appName)
          sendToIframe({
            type: 'data_update',
            sourceId,
            data: resp.data,
            error: resp.error,
          })
        } catch (err: unknown) {
          sendToIframe({
            type: 'data_update',
            sourceId,
            error: err instanceof Error ? err.message : 'Polling failed',
          })
        }
      }, ms)

      pollingIntervals.current.set(sourceId, id)
    }

    // Stop polling for a sourceId
    function handleDataUnsubscribe(msg: Record<string, unknown>) {
      const { sourceId } = msg as { sourceId: string }
      const existing = pollingIntervals.current.get(sourceId)
      if (existing) {
        clearInterval(existing)
        pollingIntervals.current.delete(sourceId)
      }
    }

    window.addEventListener('message', handleMessage)
    return () => window.removeEventListener('message', handleMessage)
  }, [maxHeight, sendCode, sendTheme, sendToIframe, appName])

  // Watch for theme changes on the document element and forward to sandbox
  useEffect(() => {
    const observer = new MutationObserver(() => {
      sendTheme()
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [sendTheme])

  useEffect(() => {
    if (!code) return
    if (ready) {
      sendCode(code)
    } else {
      pendingCode.current = code
    }
  }, [code, ready, sendCode])

  return (
    <div className="relative w-full">
      <iframe
        ref={iframeRef}
        src={SANDBOX_URL}
        sandbox="allow-scripts allow-same-origin"
        style={{
          width: '100%',
          height: `${height}px`,
          border: 'none',
          borderRadius: '8px',
          overflow: 'hidden',
          background: 'var(--bg-primary)',
          transition: 'height 0.2s ease',
        }}
        title="App Preview"
      />
      {error && (
        <div
          className="mt-2 px-3 py-2 rounded-lg text-xs font-mono"
          style={{
            background: 'rgba(239, 68, 68, 0.1)',
            border: '1px solid rgba(239, 68, 68, 0.3)',
            color: '#ef4444',
          }}
        >
          {error}
        </div>
      )}
    </div>
  )
}
