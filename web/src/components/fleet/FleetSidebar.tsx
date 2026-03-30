import { useState } from 'react'
import {
  Search, ChevronDown, ChevronRight, Loader,
  Trash2, Radio, FileText, Eye,
} from 'lucide-react'
import type { FleetPlanSummary, FleetDefinition } from '../../api/fleetChat'
import type { FleetChatSession, SidebarItem, SelectedItem } from './fleetUtils'
import { formatTimeAgo } from './fleetUtils'

// ─── Fleet Sidebar ───

interface FleetSidebarProps {
  plans: FleetPlanSummary[]
  sessions: FleetChatSession[]
  templates: FleetDefinition[]
  selectedItem: SelectedItem | null
  onSelect: (item: SelectedItem) => void
  onDeleteSession: (id: string) => void
  onSearch: (query: string) => void
  searchQuery: string
  isLoading: boolean
  theme: string
}

export default function FleetSidebar({
  plans, sessions, templates, selectedItem, onSelect, onDeleteSession, onSearch, searchQuery,
  isLoading, theme,
}: FleetSidebarProps) {
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({})

  const toggleSection = (key: string) => {
    setCollapsedSections(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const renderSection = (title: string, icon: React.ReactNode, items: SidebarItem[], type: string, badgeColor: string | null) => {
    const isCollapsed = collapsedSections[type]
    return (
      <div key={type}>
        <button
          onClick={() => toggleSection(type)}
          className="w-full flex items-center gap-2 px-4 py-2 text-xs font-semibold uppercase tracking-wider hover:bg-white/5 transition-colors"
          style={{ color: 'var(--text-muted)' }}
        >
          {isCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
          {icon}
          <span>{title}</span>
          <span className="ml-auto text-[10px] font-normal px-1.5 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)' }}>
            {items.length}
          </span>
        </button>
        {!isCollapsed && (
          <div className="pb-1">
            {items.length === 0 ? (
              <div className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                No {title.toLowerCase()} found
              </div>
            ) : items.map(item => {
              const isSelected = selectedItem?.type === type && selectedItem?.key === item.key
              return (
                <button
                  key={`${type}-${item.key}`}
                  onClick={() => onSelect({ type, key: item.key })}
                  className={`group w-full text-left px-4 py-2.5 transition-colors ${
                    isSelected ? 'bg-cyan-500/15 border-l-2 border-cyan-400' : 'hover:bg-white/5 border-l-2 border-transparent'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    {type === 'plan' && item.activated && (
                      <div className="w-2 h-2 rounded-full bg-green-400 flex-shrink-0" title="Active" />
                    )}
                    <span
                      className="text-sm font-medium truncate flex-1"
                      style={{ color: isSelected ? '#22d3ee' : 'var(--text-primary)' }}
                    >
                      {item.name}
                    </span>
                    {badgeColor && item.badge && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: badgeColor, color: '#fff' }}>
                        {item.badge}
                      </span>
                    )}
                    {item.onDelete && (
                      <button
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); item.onDelete!() }}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all shrink-0"
                        title="Delete session"
                      >
                        <Trash2 size={12} className="text-red-400" />
                      </button>
                    )}
                  </div>
                  {item.subtitle && (
                    <div className="text-xs mt-0.5 truncate" style={{ color: 'var(--text-muted)' }}>
                      {item.subtitle}
                    </div>
                  )}
                </button>
              )
            })}
          </div>
        )}
      </div>
    )
  }

  const planItems: SidebarItem[] = plans.map(p => ({
    key: p.key,
    name: p.name,
    subtitle: `${p.created_from || 'custom'} | ${p.agent_names?.join(', ') || ''}`,
    activated: false,
    badge: p.channel_type !== 'chat' ? p.channel_type : null,
  }))

  const sessionItems: SidebarItem[] = sessions.map(s => ({
    key: s.id,
    name: s.title || `Session ${s.id.slice(0, 8)}`,
    subtitle: `${s.id.slice(0, 8)} | ${s.issueNumber ? `#${s.issueNumber}` : ''} ${formatTimeAgo(s.updatedAt)}`.trim(),
    badge: null,
    onDelete: () => onDeleteSession(s.id),
  }))

  const templateItems: SidebarItem[] = templates.map(t => ({
    key: t.key,
    name: t.name,
    subtitle: `${t.agent_count} agents: ${t.agent_names?.join(', ') || ''}`,
    badge: null,
  }))

  return (
    <div
      className="flex flex-col h-full"
      style={{
        width: '288px',
        minWidth: '288px',
        borderRight: '1px solid var(--border-color)',
        background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
      }}
    >
      {/* Search */}
      <div className="px-3 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            value={searchQuery}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => onSearch(e.target.value)}
            placeholder="Search fleet..."
            className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg focus:outline-none focus:ring-1 focus:ring-cyan-500"
            style={{
              background: 'var(--bg-tertiary)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-color)',
            }}
          />
        </div>
      </div>

      {/* Sections */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader size={18} className="animate-spin text-cyan-400" />
          </div>
        ) : (
          <>
            {renderSection('Fleet Plans', <FileText size={12} />, planItems, 'plan', 'rgba(6, 182, 212, 0.6)')}
            {renderSection('Active Sessions', <Radio size={12} />, sessionItems, 'session', null)}
            {renderSection('Templates', <Eye size={12} />, templateItems, 'template', null)}
          </>
        )}
      </div>
    </div>
  )
}
