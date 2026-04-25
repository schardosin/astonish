import { useState, useEffect, useRef, useId } from 'react'

/**
 * MermaidBlock renders a mermaid diagram from source text.
 * It lazy-loads the mermaid library on first use and renders the
 * diagram as inline SVG. Falls back to showing the raw source
 * on parse/render errors.
 */
export default function MermaidBlock({ chart }: { chart: string }) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const uniqueId = useId().replace(/:/g, '_')
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false

    async function render() {
      try {
        const mermaid = (await import('mermaid')).default

        // Initialize once — subsequent calls are no-ops inside mermaid.
        mermaid.initialize({
          startOnLoad: false,
          theme: 'neutral',
          // Keep diagrams legible but not enormous
          fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
          securityLevel: 'strict',
        })

        const { svg: rendered } = await mermaid.render(`mermaid-${uniqueId}`, chart.trim())
        if (!cancelled) {
          setSvg(rendered)
          setError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to render diagram')
          setSvg(null)
        }
      }
    }

    render()
    return () => { cancelled = true }
  }, [chart, uniqueId])

  if (error) {
    return (
      <div className="mermaid-error" style={{
        border: '1px solid rgba(239, 68, 68, 0.3)',
        borderRadius: '8px',
        padding: '12px',
        margin: '8px 0',
        background: 'rgba(239, 68, 68, 0.05)',
      }}>
        <div style={{ fontSize: '11px', color: '#f87171', marginBottom: '6px', fontWeight: 500 }}>
          Diagram render error: {error}
        </div>
        <pre style={{
          fontSize: '12px',
          whiteSpace: 'pre-wrap',
          color: 'var(--text-secondary)',
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
          margin: 0,
        }}>
          {chart}
        </pre>
      </div>
    )
  }

  if (!svg) {
    // Loading state — show a subtle placeholder
    return (
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '24px',
        margin: '8px 0',
        borderRadius: '8px',
        border: '1px solid var(--border-color)',
        background: 'var(--bg-tertiary)',
        color: 'var(--text-muted)',
        fontSize: '12px',
      }}>
        Rendering diagram...
      </div>
    )
  }

  return (
    <div
      ref={containerRef}
      className="mermaid-diagram"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
