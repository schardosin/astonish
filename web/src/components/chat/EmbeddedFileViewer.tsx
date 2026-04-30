import { useState, useEffect, useRef, useCallback } from 'react'
import { Maximize2, ChevronDown, FileText, Download, Loader, FilePlus, Edit3 } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { markdownComponents } from './markdownComponents'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import { fetchArtifactContent, getArtifactDownloadUrl, getArtifactPDFUrl } from '../../api/studioChat'
import type { SessionArtifact } from './chatTypes'

// File type badge color mapping (same as FilePanel)
function fileTypeBadgeStyle(fileType: string) {
  const colors: Record<string, { bg: string; text: string }> = {
    Markdown: { bg: 'rgba(59, 130, 246, 0.15)', text: '#60a5fa' },
    Python: { bg: 'rgba(234, 179, 8, 0.15)', text: '#facc15' },
    JSON: { bg: 'rgba(168, 85, 247, 0.15)', text: '#c084fc' },
    Go: { bg: 'rgba(6, 182, 212, 0.15)', text: '#22d3ee' },
    HTML: { bg: 'rgba(249, 115, 22, 0.15)', text: '#fb923c' },
    CSS: { bg: 'rgba(59, 130, 246, 0.15)', text: '#60a5fa' },
    Shell: { bg: 'rgba(34, 197, 94, 0.15)', text: '#4ade80' },
  }
  const c = colors[fileType] || { bg: 'rgba(148, 163, 184, 0.12)', text: 'var(--text-muted)' }
  return { background: c.bg, color: c.text }
}

// Embedded file viewer for a single artifact — renders the file content inline
// with a header bar (filename, type badge, download dropdown, fullscreen button).
// Used inside agent message bubbles to show report files and other created artifacts.
export default function EmbeddedFileViewer({ artifact, sessionId, onOpenInPanel }: {
  artifact: SessionArtifact
  sessionId?: string | null
  onOpenInPanel?: (path: string) => void
}) {
  const [content, setContent] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [downloadOpen, setDownloadOpen] = useState(false)
  const [exporting, setExporting] = useState<string | null>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  const isMarkdown = artifact.fileType === 'Markdown'
  const isEdit = artifact.toolName === 'edit_file'

  // Load file content on mount
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    fetchArtifactContent(artifact.path, sessionId || undefined)
      .then(text => { if (!cancelled) setContent(text) })
      .catch(err => { if (!cancelled) setError(err.message) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [artifact.path, sessionId])

  // Close dropdown on outside click
  useEffect(() => {
    if (!downloadOpen) return
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDownloadOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [downloadOpen])

  const handleDownloadMarkdown = useCallback(() => {
    const url = getArtifactDownloadUrl(artifact.path, sessionId || undefined)
    const a = document.createElement('a')
    a.href = url
    a.download = artifact.fileName
    a.click()
    setDownloadOpen(false)
  }, [artifact.path, artifact.fileName, sessionId])

  const handleExportPDF = useCallback(async () => {
    setDownloadOpen(false)
    setExporting('pdf')
    try {
      const url = getArtifactPDFUrl(artifact.path, sessionId || undefined)
      const res = await fetch(url)
      if (!res.ok) throw new Error(`PDF generation failed: ${res.statusText}`)
      const blob = await res.blob()
      const { saveAs } = await import('file-saver')
      saveAs(blob, artifact.fileName.replace(/\.[^.]+$/, '.pdf'))
    } catch (err) {
      console.error('PDF export failed:', err)
    } finally {
      setExporting(null)
    }
  }, [artifact.path, artifact.fileName, sessionId])

  const handleExportDOCX = useCallback(async () => {
    if (!content) return
    setExporting('docx')
    setDownloadOpen(false)
    try {
      const { remarkDocx } = await import('@m2d/remark-docx')
      const { saveAs } = await import('file-saver')

      const processor = unified()
        .use(remarkParse)
        .use(remarkGfm)
        .use(remarkDocx)

      const result = await processor.process(content)
      const blob = await result.result as Blob
      saveAs(blob, artifact.fileName.replace(/\.[^.]+$/, '.docx'))
    } catch (err) {
      console.error('DOCX export failed:', err)
    } finally {
      setExporting(null)
    }
  }, [content, artifact.fileName])

  return (
    <div
      className="rounded-lg overflow-hidden"
      style={{
        border: '1px solid var(--border-color)',
        background: 'var(--bg-primary)',
      }}
    >
      {/* File header bar */}
      <div
        className="flex items-center gap-2.5 px-3 py-2"
        style={{
          borderBottom: '1px solid var(--border-color)',
          background: 'var(--bg-secondary)',
        }}
      >
        {/* File icon */}
        <div
          className="flex items-center justify-center w-7 h-7 rounded shrink-0"
          style={{ background: 'rgba(34, 197, 94, 0.12)' }}
        >
          {isEdit
            ? <Edit3 size={14} className="text-green-400" />
            : <FilePlus size={14} className="text-green-400" />
          }
        </div>

        {/* Filename + type badge */}
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <FileText size={13} className="text-green-400 shrink-0" />
          <span className="text-xs font-medium truncate" style={{ color: 'var(--text-primary)' }}>
            {artifact.fileName}
          </span>
          <span
            className="text-[10px] px-1.5 py-0.5 rounded-full shrink-0 font-medium"
            style={fileTypeBadgeStyle(artifact.fileType)}
          >
            {artifact.fileType}
          </span>
        </div>

        {/* Download dropdown */}
        <div className="relative shrink-0" ref={dropdownRef}>
          <button
            onClick={() => setDownloadOpen(!downloadOpen)}
            className="flex items-center gap-1 px-2 py-1 rounded text-[10px] font-medium hover:opacity-80 transition-opacity"
            style={{
              background: 'var(--bg-primary)',
              color: 'var(--text-secondary)',
              border: '1px solid var(--border-color)',
            }}
            disabled={!!exporting}
          >
            {exporting ? <Loader size={10} className="animate-spin" /> : <Download size={10} />}
            <span>Download</span>
            <ChevronDown size={8} />
          </button>
          {downloadOpen && (
            <div
              className="absolute right-0 top-full mt-1 py-1 rounded-md shadow-lg z-50 text-xs"
              style={{
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border-color)',
                minWidth: '180px',
              }}
            >
              <button
                onClick={handleDownloadMarkdown}
                className="w-full text-left px-3 py-1.5 hover:opacity-80 transition-opacity"
                style={{ color: 'var(--text-primary)' }}
              >
                Download as Markdown
              </button>
              {isMarkdown && (
                <>
                  <button
                    onClick={handleExportDOCX}
                    className="w-full text-left px-3 py-1.5 hover:opacity-80 transition-opacity"
                    style={{ color: 'var(--text-primary)' }}
                  >
                    Download as DOCX
                  </button>
                  <button
                    onClick={handleExportPDF}
                    className="w-full text-left px-3 py-1.5 hover:opacity-80 transition-opacity"
                    style={{ color: 'var(--text-primary)' }}
                  >
                    Download as PDF
                  </button>
                </>
              )}
            </div>
          )}
        </div>

        {/* Fullscreen button — opens in FilePanel overlay */}
        {onOpenInPanel && (
          <button
            onClick={() => onOpenInPanel(artifact.path)}
            className="p-1 rounded hover:bg-white/10 transition-colors shrink-0"
            title="Open fullscreen"
            style={{ color: 'var(--text-muted)' }}
          >
            <Maximize2 size={13} />
          </button>
        )}
      </div>

      {/* File content */}
      <div
        className="overflow-y-auto p-4"
        style={{ maxHeight: '500px' }}
      >
        {loading && (
          <div className="flex items-center justify-center py-8">
            <Loader size={18} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        )}
        {error && (
          <div className="text-xs p-3 rounded" style={{ color: '#f87171', background: 'rgba(248, 113, 113, 0.08)' }}>
            Failed to load file: {error}
          </div>
        )}
        {!loading && !error && content && (
          <div className="max-w-4xl">
            {isMarkdown ? (
              <div className="markdown-body markdown-body--document text-sm">
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{content}</ReactMarkdown>
              </div>
            ) : (
              <pre
                className="text-xs whitespace-pre-wrap break-words p-3 rounded"
                style={{
                  color: 'var(--text-primary)',
                  fontFamily: 'var(--font-mono, monospace)',
                  background: 'var(--bg-secondary)',
                  border: '1px solid var(--border-color)',
                }}
              >
                {content}
              </pre>
            )}
          </div>
        )}
        {!loading && !error && !content && (
          <div className="text-xs text-center py-6" style={{ color: 'var(--text-muted)' }}>
            File is empty
          </div>
        )}
      </div>
    </div>
  )
}
