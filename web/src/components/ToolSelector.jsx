import { useState, useMemo, useRef, useEffect } from 'react'
import { Search, ChevronDown, ChevronRight, Check, Server, Wrench } from 'lucide-react'

/**
 * ToolSelector - Grouped, searchable tool selection component
 * 
 * Features:
 * - Tools grouped by source (MCP server)
 * - Search/filter by name or description
 * - Collapsible server groups
 * - Shows tool descriptions
 * - Keyboard navigation
 */
export default function ToolSelector({ 
  availableTools = [], 
  selectedTools = [], 
  onAddTool, 
  onRemoveTool,
  placeholder = "Search tools..."
}) {
  const [searchQuery, setSearchQuery] = useState('')
  const [expandedGroups, setExpandedGroups] = useState({})
  const [isOpen, setIsOpen] = useState(false)
  const containerRef = useRef(null)
  
  // Group tools by source
  const groupedTools = useMemo(() => {
    const groups = {}
    
    availableTools.forEach(tool => {
      const source = tool.source || 'unknown'
      if (!groups[source]) {
        groups[source] = []
      }
      groups[source].push(tool)
    })
    
    // Sort groups alphabetically, but put 'internal' first
    const sortedGroups = Object.entries(groups).sort(([a], [b]) => {
      if (a === 'internal') return -1
      if (b === 'internal') return 1
      return a.localeCompare(b)
    })
    
    return sortedGroups
  }, [availableTools])
  
  // Filter tools based on search
  const filteredGroups = useMemo(() => {
    if (!searchQuery.trim()) return groupedTools
    
    const query = searchQuery.toLowerCase()
    return groupedTools
      .map(([source, tools]) => {
        const filteredTools = tools.filter(tool => 
          tool.name.toLowerCase().includes(query) ||
          (tool.description && tool.description.toLowerCase().includes(query))
        )
        return [source, filteredTools]
      })
      .filter(([, tools]) => tools.length > 0)
  }, [groupedTools, searchQuery])
  
  // Auto-expand groups when searching
  useEffect(() => {
    if (searchQuery.trim()) {
      // Expand all groups when searching
      const allExpanded = {}
      filteredGroups.forEach(([source]) => {
        allExpanded[source] = true
      })
      setExpandedGroups(allExpanded)
    }
  }, [searchQuery, filteredGroups])
  
  // Toggle group expansion
  const toggleGroup = (source) => {
    setExpandedGroups(prev => ({
      ...prev,
      [source]: !prev[source]
    }))
  }
  
  // Handle tool click
  const handleToolClick = (tool) => {
    if (selectedTools.includes(tool.name)) {
      onRemoveTool(tool.name)
    } else {
      onAddTool(tool.name)
    }
  }
  
  // Click outside to close
  useEffect(() => {
    const handleClickOutside = (e) => {
      if (containerRef.current && !containerRef.current.contains(e.target)) {
        setIsOpen(false)
      }
    }
    
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [isOpen])
  
  const totalTools = availableTools.length
  const selectedCount = selectedTools.length
  
  return (
    <div ref={containerRef} className="relative">
      {/* Trigger Button */}
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        className="w-full px-3 py-2 rounded border text-sm text-left flex items-center justify-between transition-colors hover:border-purple-500/50"
        style={{ 
          background: 'var(--bg-primary)', 
          borderColor: isOpen ? 'rgba(124, 58, 237, 0.5)' : 'var(--border-color)', 
          color: 'var(--text-primary)' 
        }}
      >
        <span className="flex items-center gap-2">
          <Wrench size={14} style={{ color: 'var(--text-muted)' }} />
          {selectedCount > 0 
            ? `${selectedCount} tool${selectedCount > 1 ? 's' : ''} selected`
            : 'Select tools...'
          }
        </span>
        <ChevronDown 
          size={16} 
          className={`transition-transform ${isOpen ? 'rotate-180' : ''}`}
          style={{ color: 'var(--text-muted)' }}
        />
      </button>
      
      {/* Dropdown Panel */}
      {isOpen && (
        <div 
          className="absolute z-50 mt-1 w-full rounded-lg border shadow-xl overflow-hidden"
          style={{ 
            background: 'var(--bg-secondary)', 
            borderColor: 'var(--border-color)',
            maxHeight: '400px'
          }}
        >
          {/* Search Input */}
          <div className="p-2 border-b" style={{ borderColor: 'var(--border-color)' }}>
            <div className="relative">
              <Search 
                size={14} 
                className="absolute left-2.5 top-1/2 -translate-y-1/2"
                style={{ color: 'var(--text-muted)' }}
              />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={placeholder}
                autoFocus
                className="w-full pl-8 pr-3 py-1.5 rounded border text-sm"
                style={{ 
                  background: 'var(--bg-primary)', 
                  borderColor: 'var(--border-color)', 
                  color: 'var(--text-primary)' 
                }}
              />
            </div>
            <div className="flex items-center justify-between mt-1.5 px-0.5">
              <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {totalTools} tools available
              </span>
              {selectedCount > 0 && (
                <button
                  onClick={() => selectedTools.forEach(t => onRemoveTool(t))}
                  className="text-xs hover:underline"
                  style={{ color: 'var(--text-muted)' }}
                >
                  Clear all
                </button>
              )}
            </div>
          </div>
          
          {/* Grouped Tools List */}
          <div className="overflow-y-auto" style={{ maxHeight: '320px' }}>
            {filteredGroups.length === 0 ? (
              <div className="p-4 text-center text-sm" style={{ color: 'var(--text-muted)' }}>
                No tools found matching "{searchQuery}"
              </div>
            ) : (
              filteredGroups.map(([source, tools]) => {
                const isExpanded = expandedGroups[source] !== false // Default to expanded
                const selectedInGroup = tools.filter(t => selectedTools.includes(t.name)).length
                
                return (
                  <div key={source} className="border-b last:border-b-0" style={{ borderColor: 'var(--border-color)' }}>
                    {/* Group Header */}
                    <button
                      onClick={() => toggleGroup(source)}
                      className="w-full px-3 py-2 flex items-center justify-between hover:bg-purple-500/10 transition-colors"
                      style={{ background: 'var(--bg-tertiary)' }}
                    >
                      <div className="flex items-center gap-2">
                        {isExpanded ? (
                          <ChevronDown size={14} style={{ color: 'var(--text-muted)' }} />
                        ) : (
                          <ChevronRight size={14} style={{ color: 'var(--text-muted)' }} />
                        )}
                        <Server size={14} style={{ color: '#a855f7' }} />
                        <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                          {source}
                        </span>
                        <span 
                          className="text-xs px-1.5 py-0.5 rounded"
                          style={{ background: 'var(--bg-primary)', color: 'var(--text-muted)' }}
                        >
                          {tools.length}
                        </span>
                      </div>
                      {selectedInGroup > 0 && (
                        <span 
                          className="text-xs px-1.5 py-0.5 rounded"
                          style={{ background: 'rgba(124, 58, 237, 0.2)', color: '#a855f7' }}
                        >
                          {selectedInGroup} selected
                        </span>
                      )}
                    </button>
                    
                    {/* Tools in Group */}
                    {isExpanded && (
                      <div className="py-1">
                        {tools.map(tool => {
                          const isSelected = selectedTools.includes(tool.name)
                          
                          return (
                            <button
                              key={tool.name}
                              onClick={() => handleToolClick(tool)}
                              className={`w-full px-3 py-2 pl-9 text-left hover:bg-purple-500/10 transition-colors ${
                                isSelected ? 'bg-purple-500/5' : ''
                              }`}
                            >
                              <div className="flex items-start gap-2">
                                <div 
                                  className={`w-4 h-4 rounded border flex-shrink-0 mt-0.5 flex items-center justify-center ${
                                    isSelected ? 'bg-purple-600 border-purple-600' : ''
                                  }`}
                                  style={!isSelected ? { borderColor: 'var(--border-color)' } : {}}
                                >
                                  {isSelected && <Check size={10} className="text-white" />}
                                </div>
                                <div className="flex-1 min-w-0">
                                  <div 
                                    className="text-sm font-mono truncate"
                                    style={{ color: 'var(--text-primary)' }}
                                  >
                                    {tool.name}
                                  </div>
                                  {tool.description && (
                                    <div 
                                      className="text-xs mt-0.5 line-clamp-2"
                                      style={{ color: 'var(--text-muted)' }}
                                    >
                                      {tool.description}
                                    </div>
                                  )}
                                </div>
                              </div>
                            </button>
                          )
                        })}
                      </div>
                    )}
                  </div>
                )
              })
            )}
          </div>
        </div>
      )}
    </div>
  )
}
