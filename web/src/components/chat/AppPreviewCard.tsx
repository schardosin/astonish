import { useState, useRef, useEffect, useCallback } from 'react'
import { Maximize2, Minimize2, Code, X, ChevronLeft, ChevronRight, AppWindow } from 'lucide-react'
import AppPreview from './AppPreview'
import type { AppPreviewMessage } from './chatTypes'

interface AppPreviewCardProps {
  data: AppPreviewMessage
  /** All versions of this app in the conversation (for version navigation) */
  versions?: AppPreviewMessage[]
  /** Current version index within the versions array */
  versionIndex?: number
  onNavigateVersion?: (index: number) => void
}

export default function AppPreviewCard({
  data,
  versions,
  versionIndex = 0,
  onNavigateVersion,
}: AppPreviewCardProps) {
  const [showCode, setShowCode] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const fullscreenRef = useRef<HTMLDivElement>(null)

  const hasMultipleVersions = versions && versions.length > 1
  const canGoPrev = hasMultipleVersions && versionIndex > 0
  const canGoNext = hasMultipleVersions && versionIndex < (versions?.length ?? 0) - 1

  // Close fullscreen on Escape
  useEffect(() => {
    if (!isFullscreen) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setIsFullscreen(false)
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isFullscreen])

  // Prevent body scroll in fullscreen
  useEffect(() => {
    if (isFullscreen) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [isFullscreen])

  const handleCopyCode = useCallback(() => {
    navigator.clipboard.writeText(data.code)
  }, [data.code])

  const renderCard = (fullscreen: boolean) => (
    <div
      className={fullscreen ? 'flex flex-col h-full' : 'my-3 rounded-xl overflow-hidden w-full max-w-3xl'}
      style={fullscreen ? {} : {
        border: '1px solid var(--border-color)',
        background: 'var(--bg-secondary)',
        boxShadow: 'var(--shadow-soft)',
      }}
    >
      {/* Header */}
      <div
        className="px-4 py-3 flex items-center justify-between"
        style={{ borderBottom: '1px solid var(--border-color)' }}
      >
        <div className="flex items-center gap-2 min-w-0">
          <AppWindow size={16} style={{ color: 'var(--accent)' }} className="flex-shrink-0" />
          <span className="text-sm font-semibold truncate" style={{ color: 'var(--accent)' }}>
            {data.title || 'App Preview'}
          </span>
          {data.description && (
            <span className="text-xs truncate hidden sm:inline" style={{ color: 'var(--text-muted)' }}>
              {data.description}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1 flex-shrink-0">
          {/* Version navigation */}
          {hasMultipleVersions && (
            <div className="flex items-center gap-0.5 mr-2">
              <button
                onClick={() => canGoPrev && onNavigateVersion?.(versionIndex - 1)}
                disabled={!canGoPrev}
                className="p-1 rounded transition-colors cursor-pointer disabled:opacity-30 disabled:cursor-default"
                style={{ color: 'var(--text-muted)' }}
                title="Previous version"
              >
                <ChevronLeft size={14} />
              </button>
              <span className="text-[10px] tabular-nums" style={{ color: 'var(--text-muted)' }}>
                v{data.version}
              </span>
              <button
                onClick={() => canGoNext && onNavigateVersion?.(versionIndex + 1)}
                disabled={!canGoNext}
                className="p-1 rounded transition-colors cursor-pointer disabled:opacity-30 disabled:cursor-default"
                style={{ color: 'var(--text-muted)' }}
                title="Next version"
              >
                <ChevronRight size={14} />
              </button>
            </div>
          )}

          {/* Code toggle */}
          <button
            onClick={() => setShowCode(!showCode)}
            className="p-1.5 rounded transition-colors cursor-pointer"
            style={{
              color: showCode ? 'var(--accent)' : 'var(--text-muted)',
              background: showCode ? 'var(--accent-soft)' : 'transparent',
            }}
            title={showCode ? 'Hide code' : 'View code'}
          >
            <Code size={14} />
          </button>

          {/* Fullscreen toggle */}
          <button
            onClick={() => setIsFullscreen(!isFullscreen)}
            className="p-1.5 rounded transition-colors cursor-pointer"
            style={{ color: 'var(--text-muted)' }}
            title={isFullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
          </button>

          {/* Close (fullscreen only) */}
          {fullscreen && (
            <button
              onClick={() => setIsFullscreen(false)}
              className="p-1.5 rounded transition-colors cursor-pointer"
              style={{ color: 'var(--text-muted)' }}
              title="Close fullscreen"
            >
              <X size={14} />
            </button>
          )}
        </div>
      </div>

      {/* Code panel (toggle) */}
      {showCode && (
        <div style={{ borderBottom: '1px solid var(--border-color)' }}>
          <div className="flex items-center justify-between px-4 py-1.5">
            <span className="text-[10px] font-medium uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>
              Source Code
            </span>
            <button
              onClick={handleCopyCode}
              className="text-[10px] px-2 py-0.5 rounded transition-colors cursor-pointer"
              style={{ color: 'var(--accent)', background: 'var(--accent-soft)' }}
            >
              Copy
            </button>
          </div>
          <pre
            className="px-4 pb-3 text-[11px] overflow-x-auto font-mono leading-relaxed"
            style={{
              maxHeight: fullscreen ? '40vh' : '200px',
              overflowY: 'auto',
              color: 'var(--text-secondary)',
            }}
          >
            <code>{data.code}</code>
          </pre>
        </div>
      )}

      {/* Preview */}
      <div className={fullscreen ? 'flex-1 overflow-auto p-2' : 'px-3 py-2'}>
        <AppPreview
          code={data.code}
          maxHeight={fullscreen ? 9999 : 500}
        />
      </div>
    </div>
  )

  return (
    <>
      {/* Inline card */}
      {renderCard(false)}

      {/* Fullscreen overlay */}
      {isFullscreen && (
        <div
          ref={fullscreenRef}
          className="fixed inset-0 z-50 flex flex-col"
          style={{
            background: 'var(--bg-primary)',
          }}
        >
          {renderCard(true)}
        </div>
      )}
    </>
  )
}
