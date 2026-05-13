import { ChevronRight, Plus, Trash2, MessageSquare, Loader, Clock, Search, Users } from 'lucide-react'
import type { ChatSession } from '../../api/studioChat'

// Extended ChatSession with optional fleet fields
interface SidebarSession extends ChatSession {
  fleetKey?: string
  fleetName?: string
}

interface SessionSidebarProps {
  sessions: SidebarSession[]
  activeSessionId: string | null
  sessionFilter: string
  onSessionFilterChange: (filter: string) => void
  isLoadingSessions: boolean
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  onSelectSession: (id: string) => void
  onNewSession: () => void
  onDeleteSession: (e: React.MouseEvent, id: string) => void
  onStartFleet: () => void
  theme: string
}

function formatTimeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const mins = Math.floor(diffMs / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`
  return date.toLocaleDateString()
}

export default function SessionSidebar({
  sessions,
  activeSessionId,
  sessionFilter,
  onSessionFilterChange,
  isLoadingSessions,
  sidebarCollapsed,
  onToggleSidebar,
  onSelectSession,
  onNewSession,
  onDeleteSession,
  onStartFleet,
  theme,
}: SessionSidebarProps) {
  if (sidebarCollapsed) {
    return (
      <div
        className="flex flex-col items-center py-3 gap-3"
        style={{
          borderRight: '1px solid var(--border-color)',
          background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
        }}
      >
        <button
          onClick={onToggleSidebar}
          className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
          title="Show sidebar"
          style={{ color: 'var(--text-secondary)' }}
        >
          <ChevronRight size={16} />
        </button>
        <button
          onClick={onNewSession}
          className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
          title="New conversation"
          style={{ color: 'var(--text-secondary)' }}
        >
          <Plus size={16} />
        </button>
      </div>
    )
  }

  return (
    <div
      className="flex flex-col"
      style={{
        width: '280px',
        minWidth: '280px',
        borderRight: '1px solid var(--border-color)',
        background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
      }}
    >
      {/* Sidebar Header */}
      <div className="flex items-center justify-between px-4 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>Conversations</span>
        <div className="flex items-center gap-1">
          <button
            onClick={onStartFleet}
            className="p-1.5 rounded-lg hover:bg-cyan-500/15 transition-colors"
            title="Start fleet session"
            style={{ color: 'var(--text-secondary)' }}
          >
            <Users size={16} className="text-cyan-400" />
          </button>
          <button
            onClick={onNewSession}
            className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
            title="New conversation"
            style={{ color: 'var(--text-secondary)' }}
          >
            <Plus size={16} />
          </button>
          <button
            onClick={onToggleSidebar}
            className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
            title="Hide sidebar"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronRight size={16} className="rotate-180" />
          </button>
        </div>
      </div>

      {/* Search */}
      <div className="px-3 py-2">
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            value={sessionFilter}
            onChange={(e) => onSessionFilterChange(e.target.value)}
            placeholder="Search conversations..."
            className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg focus:outline-none focus:ring-1 focus:ring-purple-500"
            style={{
              background: 'var(--bg-tertiary)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-color)',
            }}
          />
        </div>
      </div>

      {/* Session List */}
      <div className="flex-1 overflow-y-auto">
        {isLoadingSessions ? (
          <div className="flex items-center justify-center py-8">
            <Loader size={18} className="animate-spin text-purple-400" />
          </div>
        ) : sessions.length === 0 ? (
          <div className="px-4 py-8 text-center">
            <MessageSquare size={24} className="mx-auto mb-2" style={{ color: 'var(--text-muted)' }} />
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              {sessionFilter ? 'No matching conversations' : 'No conversations yet'}
            </p>
          </div>
        ) : (
          sessions.map(session => (
            <button
              key={session.id}
              onClick={() => onSelectSession(session.id)}
              className={`w-full text-left px-4 py-3 transition-colors group ${
                activeSessionId === session.id ? 'bg-purple-500/15' : 'hover:bg-purple-500/5'
              }`}
              style={{ borderBottom: '1px solid var(--border-color)' }}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    {session.fleetKey && (
                      <Users size={12} className="text-cyan-400 flex-shrink-0" />
                    )}
                    <p
                      className="text-sm font-medium truncate"
                      style={{ color: activeSessionId === session.id ? 'var(--accent)' : 'var(--text-primary)' }}
                    >
                      {session.title || 'Untitled'}
                    </p>
                  </div>
                  <div className="flex items-center gap-2 mt-1">
                    <Clock size={10} style={{ color: 'var(--text-muted)' }} />
                    <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                      {formatTimeAgo(session.updatedAt)}
                    </span>
                    <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                      {session.messageCount} msg{session.messageCount !== 1 ? 's' : ''}
                    </span>
                  </div>
                </div>
                <div
                  role="button"
                  tabIndex={0}
                  onClick={(e) => onDeleteSession(e as unknown as React.MouseEvent, session.id)}
                  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onDeleteSession(e as unknown as React.MouseEvent, session.id) } }}
                  className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all cursor-pointer"
                  title="Delete conversation"
                >
                  <Trash2 size={12} className="text-red-400" />
                </div>
              </div>
            </button>
          ))
        )}
      </div>
    </div>
  )
}
