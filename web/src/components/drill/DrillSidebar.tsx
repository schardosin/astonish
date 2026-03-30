import { useState, type CSSProperties } from 'react'
import { Search, ChevronDown, ChevronRight, Loader, ListChecks } from 'lucide-react'
import type { DrillSuiteSummary } from '../../api/drillApi'
import { statusColor, StatusDot } from './drillUtils'
import type { SelectedItem } from './drillUtils'

// ─── Drill Sidebar ───

interface DrillSidebarProps {
  suites: DrillSuiteSummary[]
  selectedItem: SelectedItem | null
  onSelect: (item: SelectedItem) => void
  onSearch: (query: string) => void
  searchQuery: string
  isLoading: boolean
}

export default function DrillSidebar({ suites, selectedItem, onSelect, onSearch, searchQuery, isLoading }: DrillSidebarProps) {
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({})

  const toggleSection = (key: string) => {
    setCollapsedSections(prev => ({ ...prev, [key]: !prev[key] }))
  }

  const renderSuiteItem = (suite: DrillSuiteSummary) => {
    const isSelected = selectedItem?.type === 'suite' && selectedItem?.key === suite.name
    const sc = statusColor(suite.last_status)
    return (
      <button
        key={suite.name}
        onClick={() => onSelect({ type: 'suite', key: suite.name })}
        className={`w-full text-left px-4 py-2.5 transition-colors group ${
          isSelected ? 'border-l-2' : 'border-l-2 border-transparent hover:bg-white/5'
        }`}
        style={isSelected ? {
          background: 'rgba(245, 158, 11, 0.08)',
          borderLeftColor: '#f59e0b',
        } : {}}
      >
        <div className="flex items-center gap-2">
          <StatusDot status={suite.last_status} />
          <span className="text-xs font-medium truncate flex-1" style={{ color: isSelected ? '#fbbf24' : 'var(--text-primary)' }}>
            {suite.name}
          </span>
          <span className="text-[10px] px-1.5 py-0.5 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-muted)' }}>
            {suite.drill_count}
          </span>
        </div>
        {suite.last_summary && (
          <div className="mt-0.5 text-[10px] pl-4" style={{ color: sc.text }}>
            {suite.last_summary}
          </div>
        )}
      </button>
    )
  }

  const renderSection = (title: string, icon: React.ReactNode, items: DrillSuiteSummary[], type: string, renderItem: (suite: DrillSuiteSummary) => React.ReactNode) => {
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
            ) : items.map(renderItem)}
          </div>
        )}
      </div>
    )
  }

  return (
    <div
      className="flex flex-col border-r overflow-hidden"
      style={{ width: 288, minWidth: 288, borderColor: 'var(--border-color)', background: 'var(--bg-primary)' }}
    >
      {/* Search */}
      <div className="p-3 border-b" style={{ borderColor: 'var(--border-color)' }}>
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
          <input
            type="text"
            placeholder="Search drills..."
            value={searchQuery}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => onSearch(e.target.value)}
            className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg border focus:outline-none focus:ring-1"
            style={{
              background: 'var(--bg-secondary)',
              borderColor: 'var(--border-color)',
              color: 'var(--text-primary)',
              '--tw-ring-color': '#f59e0b',
            } as CSSProperties}
          />
        </div>
      </div>

      {/* Sections */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader size={20} className="animate-spin" style={{ color: 'var(--text-muted)' }} />
          </div>
        ) : (
          renderSection('Drill Suites', <ListChecks size={12} />, suites, 'suites', renderSuiteItem)
        )}
      </div>
    </div>
  )
}
