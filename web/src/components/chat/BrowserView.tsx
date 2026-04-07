import { useState, useCallback } from 'react'
import { Monitor, ExternalLink, Check, Maximize2, Minimize2, X } from 'lucide-react'

export interface BrowserHandoffData {
  vncProxyUrl: string
  pageUrl: string
  pageTitle: string
  reason: string
}

interface BrowserViewProps {
  data: BrowserHandoffData
  theme: string
  onDone: () => void
}

/**
 * BrowserView renders an inline KasmVNC viewer in the chat stream during
 * browser sharing sessions. The user can interact with the browser visually
 * (e.g. solve a CAPTCHA) while continuing to chat with the agent.
 * Click "Done" to end the visual sharing session.
 */
export default function BrowserView({ data, theme, onDone }: BrowserViewProps) {
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [isDone, setIsDone] = useState(false)
  const [iframeLoaded, setIframeLoaded] = useState(false)

  const handleDone = useCallback(async () => {
    setIsDone(true)
    try {
      await fetch('/api/browser/handoff-done', { method: 'POST' })
    } catch {
      // Best-effort — the handoff may have already timed out
    }
    onDone()
  }, [onDone])

  const toggleFullscreen = useCallback(() => {
    setIsFullscreen(prev => !prev)
  }, [])

  if (isDone) {
    return (
      <div
        className="my-2 rounded-lg px-3 py-2 flex items-center gap-2 text-sm"
        style={{
          border: '1px solid var(--border-color)',
          background: theme === 'dark' ? 'rgba(34, 197, 94, 0.05)' : 'rgba(34, 197, 94, 0.08)',
          color: 'var(--text-muted)',
        }}
      >
        <Check size={14} className="text-green-400" />
        <span>Browser sharing ended</span>
      </div>
    )
  }

  const containerClasses = isFullscreen
    ? 'fixed inset-0 z-50 flex flex-col'
    : 'my-3 rounded-lg overflow-hidden flex flex-col'

  const containerStyle = isFullscreen
    ? {
        background: theme === 'dark' ? '#0a0a0f' : '#ffffff',
      }
    : {
        border: '1px solid rgba(6, 182, 212, 0.3)',
        background: theme === 'dark' ? 'rgba(6, 182, 212, 0.03)' : 'rgba(6, 182, 212, 0.05)',
      }

  return (
    <div className={containerClasses} style={containerStyle}>
      {/* Header */}
      <div
        className="flex items-center gap-2 px-3 py-2 shrink-0"
        style={{
          borderBottom: '1px solid rgba(6, 182, 212, 0.2)',
          background: theme === 'dark' ? 'rgba(6, 182, 212, 0.05)' : 'rgba(6, 182, 212, 0.08)',
        }}
      >
        <Monitor size={14} className="text-cyan-400" />
        <span className="text-xs font-medium flex-1 truncate" style={{ color: 'var(--text-primary)' }}>
          Browser: {data.reason}
        </span>

        {data.pageUrl && (
          <span className="text-xs truncate max-w-[200px]" style={{ color: 'var(--text-muted)' }}>
            {data.pageUrl}
          </span>
        )}

        <div className="flex items-center gap-1 ml-auto shrink-0">
          <a
            href={data.vncProxyUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="p-1 rounded hover:bg-cyan-500/10 transition-colors"
            title="Open in new tab"
          >
            <ExternalLink size={14} className="text-cyan-400" />
          </a>
          <button
            onClick={toggleFullscreen}
            className="p-1 rounded hover:bg-cyan-500/10 transition-colors"
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? (
              <Minimize2 size={14} className="text-cyan-400" />
            ) : (
              <Maximize2 size={14} className="text-cyan-400" />
            )}
          </button>
          {isFullscreen && (
            <button
              onClick={toggleFullscreen}
              className="p-1 rounded hover:bg-cyan-500/10 transition-colors"
              title="Close"
            >
              <X size={14} className="text-cyan-400" />
            </button>
          )}
        </div>
      </div>

      {/* KasmVNC iframe */}
      <div className="relative flex-1" style={{ minHeight: isFullscreen ? 0 : '500px' }}>
        {!iframeLoaded && (
          <div className="absolute inset-0 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
            <span className="text-sm">Loading browser view...</span>
          </div>
        )}
        <iframe
          src={data.vncProxyUrl}
          className="w-full h-full border-0"
          style={{
            minHeight: isFullscreen ? '100%' : '500px',
            opacity: iframeLoaded ? 1 : 0,
          }}
          onLoad={() => setIframeLoaded(true)}
          title="Browser View"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      </div>

      {/* Footer with Done button */}
      <div
        className="flex items-center justify-between px-3 py-2 shrink-0"
        style={{
          borderTop: '1px solid rgba(6, 182, 212, 0.2)',
          background: theme === 'dark' ? 'rgba(6, 182, 212, 0.05)' : 'rgba(6, 182, 212, 0.08)',
        }}
      >
        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
          You can continue chatting while viewing the browser. Click Done to end visual sharing.
        </span>
        <button
          onClick={handleDone}
          className="px-3 py-1 rounded text-xs font-medium bg-cyan-500 text-white hover:bg-cyan-600 transition-colors"
        >
          Done
        </button>
      </div>
    </div>
  )
}
