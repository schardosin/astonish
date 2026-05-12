import { MessageSquare, Zap, Brain, Users, Terminal, Sparkles } from 'lucide-react'
import type { ReactNode } from 'react'

interface HomePageProps {
  onSuggestionClick?: (text: string) => void
}

export default function HomePage({ onSuggestionClick }: HomePageProps) {
  const suggestions = [
    'What can you help me with?',
    '/status',
    '/fleet-plan',
    'Search my knowledge for recent decisions',
    'Help me write a script to automate deployments',
  ]

  return (
    <div
      className="flex-1 flex flex-col items-center justify-center p-8 overflow-y-auto"
      style={{ background: 'var(--bg-primary)' }}
    >
      {/* Welcome */}
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 mb-5">
          <img
            src="/astonish-logo.svg"
            alt="Astonish Logo"
            className="w-full h-full"
          />
        </div>
        <h1 className="text-3xl font-bold mb-2" style={{ color: 'var(--text-primary)' }}>
          Astonish Studio
        </h1>
        <p className="text-base max-w-lg mx-auto" style={{ color: 'var(--text-muted)' }}>
          Your AI operations assistant. Ask questions, run tasks, manage knowledge, or launch autonomous agent teams.
        </p>
      </div>

      {/* Capabilities */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 max-w-3xl w-full mb-10">
        <CapabilityCard
          icon={<MessageSquare size={18} />}
          title="Conversation"
          description="Ask anything — coding, analysis, writing, planning"
          color="#a855f7"
        />
        <CapabilityCard
          icon={<Zap size={18} />}
          title="Tools & Actions"
          description="Execute commands, browse the web, read & write files"
          color="#f59e0b"
        />
        <CapabilityCard
          icon={<Brain size={18} />}
          title="Knowledge"
          description="Searches your memories for context automatically"
          color="#3b82f6"
        />
        <CapabilityCard
          icon={<Users size={18} />}
          title="Fleet Plans"
          description="Launch multi-agent teams for complex projects"
          color="#10b981"
        />
        <CapabilityCard
          icon={<Terminal size={18} />}
          title="Slash Commands"
          description="/status, /fleet-plan, /drill, /help, and more"
          color="#06b6d4"
        />
        <CapabilityCard
          icon={<Sparkles size={18} />}
          title="Apps & Flows"
          description="Generate interactive UIs and reusable agent flows"
          color="#ec4899"
        />
      </div>

      {/* Suggestion chips */}
      <div className="max-w-2xl w-full">
        <p className="text-xs font-medium uppercase tracking-wider mb-3 text-center" style={{ color: 'var(--text-muted)' }}>
          Try asking
        </p>
        <div className="flex flex-wrap justify-center gap-2">
          {suggestions.map((text) => (
            <button
              key={text}
              onClick={() => onSuggestionClick?.(text)}
              className="px-3.5 py-2 rounded-xl text-sm transition-all hover:scale-[1.03] active:scale-[0.98]"
              style={{
                background: 'var(--bg-secondary)',
                color: 'var(--text-secondary)',
                border: '1px solid var(--border-color)',
              }}
            >
              {text}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

function CapabilityCard({ icon, title, description, color }: {
  icon: ReactNode
  title: string
  description: string
  color: string
}) {
  return (
    <div
      className="flex items-start gap-3 p-3.5 rounded-xl"
      style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border-color)',
      }}
    >
      <div
        className="flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center"
        style={{ background: `${color}20`, color }}
      >
        {icon}
      </div>
      <div className="min-w-0">
        <h3 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
          {title}
        </h3>
        <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
          {description}
        </p>
      </div>
    </div>
  )
}
