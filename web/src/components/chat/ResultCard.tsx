import { useState } from 'react'
import { Copy, Check, Maximize2, Minimize2, ChevronDown, ChevronUp } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface ResultCardProps {
  content: string
  showRaw: boolean
  onToggleRaw: () => void
}

// Final output card for long agent responses.
// Perplexity-inspired: bordered card with header bar (copy/fullscreen buttons),
// full markdown rendering, and collapse/expand functionality.
export default function ResultCard({ content, showRaw, onToggleRaw }: ResultCardProps) {
  const [copied, setCopied] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const handleDownload = () => {
    const blob = new Blob([content], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'response.md'
    a.click()
    URL.revokeObjectURL(url)
  }

  if (fullscreen) {
    return (
      <div
        className="fixed inset-0 z-50 flex flex-col"
        style={{ background: 'var(--bg-primary)' }}
      >
        {/* Fullscreen header */}
        <div
          className="flex items-center justify-between px-6 py-3 shrink-0"
          style={{ borderBottom: '1px solid var(--border-color)' }}
        >
          <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
            Response
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={onToggleRaw}
              className="p-1.5 rounded hover:bg-white/10 transition-colors"
              title={showRaw ? 'Show formatted' : 'Show raw'}
              style={{ color: 'var(--text-muted)' }}
            >
              <span className="text-xs font-mono">{showRaw ? 'MD' : 'Raw'}</span>
            </button>
            <button
              onClick={handleCopy}
              className="p-1.5 rounded hover:bg-white/10 transition-colors"
              title="Copy"
              style={{ color: 'var(--text-muted)' }}
            >
              {copied ? <Check size={14} className="text-green-400" /> : <Copy size={14} />}
            </button>
            <button
              onClick={handleDownload}
              className="p-1.5 rounded hover:bg-white/10 transition-colors text-xs"
              title="Download as markdown"
              style={{ color: 'var(--text-muted)' }}
            >
              DL
            </button>
            <button
              onClick={() => setFullscreen(false)}
              className="p-1.5 rounded hover:bg-white/10 transition-colors"
              title="Exit fullscreen"
              style={{ color: 'var(--text-muted)' }}
            >
              <Minimize2 size={14} />
            </button>
          </div>
        </div>

        {/* Fullscreen content */}
        <div className="flex-1 overflow-y-auto p-6">
          <div className="max-w-3xl mx-auto">
            {showRaw ? (
              <pre className="text-sm whitespace-pre-wrap break-words font-mono" style={{ color: 'var(--text-primary)' }}>
                {content}
              </pre>
            ) : (
              <div style={{ color: 'var(--text-primary)' }} className="markdown-body markdown-body--document text-sm">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
              </div>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div
      className="result-card rounded-lg overflow-hidden"
      style={{
        border: '1px solid var(--border-color)',
        background: 'var(--bg-secondary)',
        boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
      }}
    >
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-2"
        style={{ borderBottom: '1px solid var(--border-color)' }}
      >
        <span className="text-xs font-semibold" style={{ color: 'var(--text-primary)' }}>
          Response
        </span>
        <div className="flex items-center gap-1">
          <button
            onClick={onToggleRaw}
            className="p-1 rounded hover:bg-white/10 transition-colors"
            title={showRaw ? 'Show formatted' : 'Show raw'}
            style={{ color: 'var(--text-muted)' }}
          >
            <span className="text-[10px] font-mono">{showRaw ? 'MD' : 'Raw'}</span>
          </button>
          <button
            onClick={handleCopy}
            className="p-1 rounded hover:bg-white/10 transition-colors"
            title="Copy"
            style={{ color: 'var(--text-muted)' }}
          >
            {copied ? <Check size={13} className="text-green-400" /> : <Copy size={13} />}
          </button>
          <button
            onClick={() => setFullscreen(true)}
            className="p-1 rounded hover:bg-white/10 transition-colors"
            title="Fullscreen"
            style={{ color: 'var(--text-muted)' }}
          >
            <Maximize2 size={13} />
          </button>
        </div>
      </div>

      {/* Content */}
      {!collapsed && (
        <div className="p-4 max-w-[90%]">
          {showRaw ? (
            <pre className="text-sm whitespace-pre-wrap break-words font-mono" style={{ color: 'var(--text-primary)' }}>
              {content}
            </pre>
          ) : (
            <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            </div>
          )}
        </div>
      )}

      {/* Collapse footer */}
      <div
        className="flex justify-center py-1.5 cursor-pointer hover:bg-white/5 transition-colors"
        style={{ borderTop: '1px solid var(--border-color)' }}
        onClick={() => setCollapsed(!collapsed)}
      >
        {collapsed ? (
          <ChevronDown size={14} style={{ color: 'var(--text-muted)' }} />
        ) : (
          <ChevronUp size={14} style={{ color: 'var(--text-muted)' }} />
        )}
      </div>
    </div>
  )
}
