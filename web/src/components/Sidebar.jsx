import { useState, useMemo } from 'react'
import { Plus, Trash2, Store, Search, ChevronDown, ChevronRight, FolderOpen } from 'lucide-react'
import { snakeToTitleCase } from '../utils/formatters'

export default function Sidebar({
  agents,
  selectedAgent,
  onAgentSelect,
  onCreateNew,
  onDeleteAgent,
  isLoading
}) {
  const [searchQuery, setSearchQuery] = useState('')
  const [sourceFilter, setSourceFilter] = useState('all') // 'all', 'local', 'official', or tap name
  const [collapsedSections, setCollapsedSections] = useState({})

  // Get unique sources for filter dropdown
  const sources = useMemo(() => {
    const srcSet = new Set()
    agents.forEach(a => {
      if (a.source === 'store') {
        srcSet.add(a.tapName || 'official')
      } else {
        srcSet.add('local')
      }
    })
    return Array.from(srcSet)
  }, [agents])

  // Filter and group agents
  const groupedAgents = useMemo(() => {
    // First filter by search
    let filtered = agents.filter(a => 
      a.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      (a.description && a.description.toLowerCase().includes(searchQuery.toLowerCase()))
    )

    // Then filter by source
    if (sourceFilter !== 'all') {
      filtered = filtered.filter(a => {
        if (sourceFilter === 'local') return a.source !== 'store'
        if (sourceFilter === 'official') return a.source === 'store' && a.tapName === 'official'
        return a.source === 'store' && a.tapName === sourceFilter
      })
    }

    // Group by source
    const groups = {
      local: [],
      official: [],
      taps: {} // tapName -> agents
    }

    filtered.forEach(a => {
      if (a.source !== 'store') {
        groups.local.push(a)
      } else if (a.tapName === 'official') {
        groups.official.push(a)
      } else {
        if (!groups.taps[a.tapName]) groups.taps[a.tapName] = []
        groups.taps[a.tapName].push(a)
      }
    })

    return groups
  }, [agents, searchQuery, sourceFilter])

  const toggleSection = (section) => {
    setCollapsedSections(prev => ({
      ...prev,
      [section]: !prev[section]
    }))
  }

  const renderAgentList = (agentList) => (
    <div className="space-y-0.5">
      {agentList.map((agent) => (
        <div
          key={agent.id}
          className={`group flex items-center gap-1 px-3 py-2 rounded-lg transition-colors ${
            selectedAgent?.id === agent.id
              ? 'bg-purple-500/20 border-l-4 border-[#6B46C1]'
              : 'hover:bg-purple-500/10'
          }`}
        >
          <button
            onClick={() => onAgentSelect(agent)}
            className="flex-1 text-left min-w-0"
            style={{ color: selectedAgent?.id === agent.id ? '#9F7AEA' : 'var(--text-secondary)' }}
            title={agent.description || ''}
          >
            <div className="font-medium text-sm truncate">
              {/* For store flows with tap prefix, show just the flow name since it's already under the tap section */}
              {snakeToTitleCase(agent.name.includes('/') ? agent.name.split('/').pop() : agent.name)}
            </div>
            {agent.description && (
              <div 
                className="text-xs mt-0.5 truncate" 
                style={{ color: 'var(--text-muted)', maxWidth: '160px' }}
              >
                {agent.description}
              </div>
            )}
          </button>
          {agent.source === 'store' ? (
            <button
              onClick={(e) => {
                e.stopPropagation()
                if (onDeleteAgent) onDeleteAgent(agent)
              }}
              className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all shrink-0"
              title="Uninstall flow"
            >
              <Trash2 size={14} className="text-red-400" />
            </button>
          ) : (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onDeleteAgent(agent)
              }}
              className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all shrink-0"
              title="Delete agent"
            >
              <Trash2 size={14} className="text-red-400" />
            </button>
          )}
        </div>
      ))}
    </div>
  )

  const renderSection = (title, agents, icon, sectionKey, color = 'var(--text-muted)') => {
    if (agents.length === 0) return null
    const isCollapsed = collapsedSections[sectionKey]
    
    return (
      <div className="mb-2">
        <button
          onClick={() => toggleSection(sectionKey)}
          className="w-full flex items-center gap-2 px-3 py-1.5 text-xs font-semibold uppercase tracking-wide hover:bg-white/5 rounded transition-colors"
          style={{ color }}
        >
          {isCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
          {icon}
          <span>{title}</span>
          <span className="ml-auto text-[10px] opacity-60">{agents.length}</span>
        </button>
        {!isCollapsed && renderAgentList(agents)}
      </div>
    )
  }

  return (
    <div className="w-64 flex flex-col" style={{ background: 'var(--bg-secondary)', borderRight: '1px solid var(--border-color)' }}>
      {/* Create New Agent Button */}
      <div className="p-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <button
          onClick={onCreateNew}
          className="w-full flex items-center justify-center gap-2 py-2.5 px-4 text-white font-semibold rounded-lg transition-colors shadow-sm text-sm"
          style={{ background: 'var(--accent)' }}
        >
          <Plus size={18} />
          New Flow
        </button>
      </div>

      {/* Search and Filter */}
      <div className="p-2 space-y-2" style={{ borderBottom: '1px solid var(--border-color)' }}>
        {/* Search */}
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            placeholder="Search flows"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full pl-8 pr-3 py-2 rounded-lg bg-transparent text-sm outline-none"
            style={{ border: '1px solid var(--border-color)', color: 'var(--text-primary)' }}
          />
        </div>

        {/* Filter */}
        <select
          value={sourceFilter}
          onChange={(e) => setSourceFilter(e.target.value)}
          className="w-full px-2.5 py-2 rounded-lg text-sm bg-transparent outline-none cursor-pointer"
          style={{ border: '1px solid var(--border-color)', color: 'var(--text-secondary)' }}
        >
          <option value="all">All Sources</option>
          <option value="local">Local</option>
          <option value="official">Official Store</option>
          {sources.filter(s => s !== 'local' && s !== 'official').map(tap => (
            <option key={tap} value={tap}>{tap}</option>
          ))}
        </select>
      </div>

      {/* Agent List */}
      <div className="flex-1 overflow-y-auto p-2 space-y-3">
        {isLoading ? (
          <div className="text-center py-8" style={{ color: 'var(--text-muted)' }}>
            <span className="text-sm">Loading flows...</span>
          </div>
        ) : (
          <>
            {/* Local Section */}
            {renderSection(
              'Local',
              groupedAgents.local,
              <FolderOpen size={12} />,
              'local',
              'var(--text-secondary)'
            )}

            {/* Official Store Section */}
            {renderSection(
              'Official Store',
              groupedAgents.official,
              <Store size={12} />,
              'official',
              '#3B82F6'
            )}

            {/* Custom Taps */}
            {Object.entries(groupedAgents.taps).map(([tapName, tapAgents]) => (
              <div key={`tap-${tapName}`}>
                {renderSection(
                  tapName,
                  tapAgents,
                  <Store size={12} />,
                  `tap-${tapName}`,
                  'var(--text-secondary)'
                )}
              </div>
            ))}

            {/* Empty state */}
            {groupedAgents.local.length === 0 &&
             groupedAgents.official.length === 0 &&
             Object.keys(groupedAgents.taps).length === 0 && (
              <div className="text-center py-8" style={{ color: 'var(--text-muted)' }}>
                <span className="text-sm">
                  {searchQuery ? 'No flows match your search' : 'No flows available'}
                </span>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
