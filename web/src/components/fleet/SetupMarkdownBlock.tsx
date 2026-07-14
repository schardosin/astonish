interface SetupMarkdownBlockProps {
  content: string
}

export default function SetupMarkdownBlock({ content }: SetupMarkdownBlockProps) {
  if (!content.trim()) return null
  return (
    <div
      className="prose prose-sm max-w-none text-sm whitespace-pre-wrap"
      style={{ color: 'var(--text-secondary)' }}
    >
      {content}
    </div>
  )
}
