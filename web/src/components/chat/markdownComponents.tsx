import type { Components } from 'react-markdown'
import MermaidBlock from './MermaidBlock'

/**
 * Shared custom component overrides for ReactMarkdown.
 *
 * Currently handles:
 * - `code` blocks with language "mermaid" → renders as interactive SVG diagrams
 *
 * Usage:
 *   <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
 *     {content}
 *   </ReactMarkdown>
 *
 * These can be merged with additional per-site overrides when needed:
 *   components={{ ...markdownComponents, p: CustomParagraph }}
 */
export const markdownComponents: Partial<Components> = {
  code({ className, children, ...props }) {
    // Detect mermaid code blocks — ReactMarkdown sets className to "language-mermaid"
    // for fenced code blocks tagged as ```mermaid
    const isMermaid = className === 'language-mermaid'

    if (isMermaid) {
      const chart = String(children).replace(/\n$/, '')
      return <MermaidBlock chart={chart} />
    }

    // Default: render as regular <code>
    return (
      <code className={className} {...props}>
        {children}
      </code>
    )
  },
}
