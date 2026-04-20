import { AppWindow, ChevronRight, ChevronDown, Loader } from 'lucide-react'

interface AppCodeIndicatorProps {
  /** Whether the code is still being streamed */
  streaming: boolean
  /** The raw code content */
  code: string
  /** Whether the code panel is expanded (controlled by parent) */
  expanded: boolean
  /** Toggle expand/collapse (controlled by parent) */
  onToggle: () => void
}

/**
 * Compact indicator that replaces the raw astonish-app code block in chat messages.
 * During streaming: shows a "Generating app..." spinner.
 * After streaming: shows a collapsed block with optional expand to view code.
 * Rendered outside ReactMarkdown to avoid remount jank during streaming.
 */
export default function AppCodeIndicator({ streaming, code, expanded, onToggle }: AppCodeIndicatorProps) {
  // Count non-empty lines for the summary
  const lineCount = code.split('\n').filter(l => l.trim()).length

  return (
    <div
      className="my-2 rounded-lg overflow-hidden text-sm"
      style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border-color)',
      }}
    >
      {/* Header bar */}
      <button
        onClick={onToggle}
        className="w-full flex items-center gap-2 px-3 py-2 text-left"
        style={{
          cursor: 'pointer',
          color: 'var(--text-secondary)',
        }}
      >
        <AppWindow size={14} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
        {streaming ? (
          <>
            <span className="flex-1" style={{ color: 'var(--text-secondary)' }}>
              Generating app...
            </span>
            <Loader size={14} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
            {expanded ? (
              <ChevronDown size={14} style={{ color: 'var(--text-muted)' }} />
            ) : (
              <ChevronRight size={14} style={{ color: 'var(--text-muted)' }} />
            )}
          </>
        ) : (
          <>
            <span className="flex-1" style={{ color: 'var(--text-secondary)' }}>
              Generated app
            </span>
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
              {lineCount} lines
            </span>
            {expanded ? (
              <ChevronDown size={14} style={{ color: 'var(--text-muted)' }} />
            ) : (
              <ChevronRight size={14} style={{ color: 'var(--text-muted)' }} />
            )}
          </>
        )}
      </button>

      {/* Expandable code view */}
      {expanded && (
        <div style={{ borderTop: '1px solid var(--border-color)' }}>
          <pre
            className="px-3 py-2 text-xs overflow-x-auto font-mono leading-relaxed"
            style={{ color: 'var(--text-secondary)', maxHeight: 400, overflowY: 'auto' }}
          >
            <code>{code}</code>
          </pre>
        </div>
      )}
    </div>
  )
}
