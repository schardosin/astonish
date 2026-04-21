import { useState, useRef, useEffect, useCallback } from 'react'
import { Maximize2, Minimize2, Code, X, ChevronLeft, ChevronRight, AppWindow, Save } from 'lucide-react'
import AppPreview from './AppPreview'
import type { AppPreviewMessage } from './chatTypes'

interface AppPreviewCardProps {
  data: AppPreviewMessage
  /** All versions of this app in the conversation (for version navigation) */
  versions?: AppPreviewMessage[]
  /** Current version index within the versions array */
  versionIndex?: number
  onNavigateVersion?: (index: number) => void
  /** Called when user confirms save — passes the user-chosen name */
  onSave?: (name: string) => void
  /** Whether this is the active (latest) app being refined */
  isActive?: boolean
}

export default function AppPreviewCard({
  data,
  versions,
  versionIndex = 0,
  onNavigateVersion,
  onSave,
  isActive = false,
}: AppPreviewCardProps) {
  const [showCode, setShowCode] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [showSaveDialog, setShowSaveDialog] = useState(false)
  const [saveName, setSaveName] = useState('')
  const saveInputRef = useRef<HTMLInputElement>(null)
  const fullscreenRef = useRef<HTMLDivElement>(null)

  const hasMultipleVersions = versions && versions.length > 1
  const canGoPrev = hasMultipleVersions && versionIndex > 0
  const canGoNext = hasMultipleVersions && versionIndex < (versions?.length ?? 0) - 1

  // The displayed version — either the navigated version or the current data
  const displayedData = versions && versions[versionIndex] ? versions[versionIndex] : data

  // Focus save input when dialog opens
  useEffect(() => {
    if (showSaveDialog && saveInputRef.current) {
      saveInputRef.current.focus()
      saveInputRef.current.select()
    }
  }, [showSaveDialog])

  const handleSaveClick = useCallback(() => {
    setSaveName(displayedData.title || 'My App')
    setShowSaveDialog(true)
  }, [displayedData.title])

  const handleSaveConfirm = useCallback(() => {
    const name = saveName.trim()
    if (name && onSave) {
      onSave(name)
      setShowSaveDialog(false)
    }
  }, [saveName, onSave])

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
    navigator.clipboard.writeText(displayedData.code)
  }, [displayedData.code])

  const renderCard = (fullscreen: boolean) => (
    <div
      className={fullscreen ? 'flex flex-col h-full' : 'my-3 rounded-xl overflow-hidden w-full'}
      style={fullscreen ? {} : {
        border: `1px solid ${isActive ? 'var(--accent)' : 'var(--border-color)'}`,
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
            {displayedData.title || 'App Preview'}
          </span>
          {displayedData.description && (
            <span className="text-xs truncate hidden sm:inline" style={{ color: 'var(--text-muted)' }}>
              {displayedData.description}
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
                v{displayedData.version}
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

          {/* Save button (only shown when actively refining) */}
          {isActive && onSave && !showSaveDialog && (
            <button
              onClick={handleSaveClick}
              className="flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium transition-colors cursor-pointer mr-1"
              style={{
                color: 'var(--text-on-accent)',
                background: 'var(--accent)',
              }}
              title="Save this app"
            >
              <Save size={12} />
              Save
            </button>
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

      {/* Save dialog — inline bar below header */}
      {showSaveDialog && (
        <div
          className="flex items-center gap-2 px-4 py-2.5"
          style={{ borderBottom: '1px solid var(--border-color)', background: 'var(--bg-tertiary)' }}
        >
          <label className="text-xs whitespace-nowrap" style={{ color: 'var(--text-secondary)' }}>
            App name:
          </label>
          <input
            ref={saveInputRef}
            type="text"
            value={saveName}
            onChange={(e) => setSaveName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleSaveConfirm()
              if (e.key === 'Escape') setShowSaveDialog(false)
            }}
            className="flex-1 px-2.5 py-1 rounded-lg text-sm border focus:outline-none focus:ring-1"
            style={{
              background: 'var(--bg-primary)',
              borderColor: 'var(--border-color)',
              color: 'var(--text-primary)',
              // @ts-expect-error CSS custom property for focus ring
              '--tw-ring-color': 'var(--accent)',
            }}
            placeholder="Enter app name..."
          />
          <button
            onClick={handleSaveConfirm}
            disabled={!saveName.trim()}
            className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-medium transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-default"
            style={{
              color: 'var(--text-on-accent)',
              background: 'var(--accent)',
            }}
          >
            <Save size={12} />
            Save
          </button>
          <button
            onClick={() => setShowSaveDialog(false)}
            className="p-1 rounded transition-colors cursor-pointer"
            style={{ color: 'var(--text-muted)' }}
            title="Cancel"
          >
            <X size={14} />
          </button>
        </div>
      )}

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
            <code>{displayedData.code}</code>
          </pre>
        </div>
      )}

      {/* Preview */}
      <div className={fullscreen ? 'flex-1 overflow-auto p-2' : 'px-3 py-2'}>
        <AppPreview
          code={displayedData.code}
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
