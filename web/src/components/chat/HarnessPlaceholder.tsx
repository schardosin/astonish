import { AppWindow, FileText, Workflow, Clapperboard, Film, Monitor, ChevronRight } from 'lucide-react'
import type { HarnessFocus } from './chatHarness'

interface HarnessPlaceholderProps {
  focus: HarnessFocus
  title: string
  subtitle?: string
  isFocused?: boolean
  onOpen: (focus: HarnessFocus) => void
}

function iconFor(kind: HarnessFocus['kind']) {
  switch (kind) {
    case 'app':
      return AppWindow
    case 'report':
      return FileText
    case 'video':
      return Film
    case 'distill':
      return Workflow
    case 'tutorial_blueprint':
    case 'tutorial_slideshow':
      return Clapperboard
    case 'browser_handoff':
      return Monitor
  }
}

function kindPrefix(kind: HarnessFocus['kind']): string {
  switch (kind) {
    case 'app':
      return 'App'
    case 'report':
      return 'Report'
    case 'video':
      return 'Video'
    case 'distill':
      return 'Flow draft'
    case 'tutorial_blueprint':
      return 'Tutorial blueprint'
    case 'tutorial_slideshow':
      return 'Tutorial scenes'
    case 'browser_handoff':
      return 'Browser'
  }
}

export default function HarnessPlaceholder({
  focus,
  title,
  subtitle,
  isFocused = false,
  onOpen,
}: HarnessPlaceholderProps) {
  const Icon = iconFor(focus.kind)

  return (
    <button
      type="button"
      data-testid="harness-placeholder"
      data-harness-kind={focus.kind}
      onClick={() => onOpen(focus)}
      className="my-2 w-full max-w-xl flex items-center gap-3 px-3 py-2.5 rounded-xl text-left transition-colors cursor-pointer"
      style={{
        border: `1px solid ${isFocused ? 'var(--accent)' : 'var(--border-color)'}`,
        background: isFocused
          ? 'var(--accent-soft, rgba(59, 130, 246, 0.1))'
          : 'var(--bg-secondary)',
        boxShadow: 'var(--shadow-soft)',
      }}
    >
      <div
        className="flex items-center justify-center w-8 h-8 rounded-lg flex-shrink-0"
        style={{
          background: 'var(--accent-soft, rgba(59, 130, 246, 0.12))',
          color: 'var(--accent)',
        }}
      >
        <Icon size={16} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="text-sm font-medium truncate" style={{ color: 'var(--text-primary)' }}>
          {kindPrefix(focus.kind)}: {title}
        </div>
        {subtitle && (
          <div className="text-xs truncate mt-0.5" style={{ color: 'var(--text-muted)' }}>
            {subtitle}
          </div>
        )}
      </div>
      <span
        className="flex items-center gap-0.5 text-xs font-medium flex-shrink-0"
        style={{ color: 'var(--accent)' }}
      >
        Open
        <ChevronRight size={14} />
      </span>
    </button>
  )
}
