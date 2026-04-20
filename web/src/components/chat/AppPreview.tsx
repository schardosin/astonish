import { useRef, useEffect, useCallback, useState } from 'react'

const SANDBOX_URL = '/api/app-preview/sandbox'

interface AppPreviewProps {
  code: string
  maxHeight?: number
}

function detectTheme(): 'dark' | 'light' {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

export default function AppPreview({ code, maxHeight = 500 }: AppPreviewProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState(200)
  const [error, setError] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const pendingCode = useRef<string | null>(null)

  const sendCode = useCallback((codeToSend: string) => {
    if (iframeRef.current?.contentWindow) {
      setError(null)
      iframeRef.current.contentWindow.postMessage({ type: 'render', code: codeToSend }, '*')
    }
  }, [])

  const sendTheme = useCallback(() => {
    if (iframeRef.current?.contentWindow) {
      iframeRef.current.contentWindow.postMessage({ type: 'theme', mode: detectTheme() }, '*')
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
      }
    }

    window.addEventListener('message', handleMessage)
    return () => window.removeEventListener('message', handleMessage)
  }, [maxHeight, sendCode, sendTheme])

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
