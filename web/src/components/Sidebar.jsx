import { Users, Plus, Trash2 } from 'lucide-react'
import { snakeToTitleCase } from '../utils/formatters'

export default function Sidebar({ 
  agents, 
  selectedAgent, 
  onAgentSelect, 
  onCreateNew, 
  onDeleteAgent,
  isLoading
}) {
  return (
    <div className="w-64 flex flex-col" style={{ background: 'var(--bg-secondary)', borderRight: '1px solid var(--border-color)' }}>
      {/* Create New Agent Button */}
      <div className="p-4" style={{ borderBottom: '1px solid var(--border-color)' }}>
        <button
          onClick={onCreateNew}
          className="w-full flex items-center justify-center gap-2 py-3 px-4 bg-[#805AD5] hover:bg-[#6B46C1] text-white font-medium rounded-lg transition-colors shadow-sm"
        >
          <Plus size={20} />
          Create New Agent
        </button>
      </div>

      {/* Agent List */}
      <div className="flex-1 overflow-y-auto">
        <div className="px-4 py-2">
          <div className="flex items-center gap-2 text-sm mb-2" style={{ color: 'var(--text-muted)' }}>
            <Users size={16} />
            <span>Agents</span>
          </div>
        </div>

        <div className="space-y-1 px-2">
          {isLoading ? (
            <div className="text-center py-4" style={{ color: 'var(--text-muted)' }}>
              <span className="text-sm">Loading agents...</span>
            </div>
          ) : agents.length === 0 ? (
            <div className="text-center py-4" style={{ color: 'var(--text-muted)' }}>
              <span className="text-sm">No agents found</span>
            </div>
          ) : (
            agents.map((agent) => (
              <div
                key={agent.id}
                className={`group flex items-center gap-1 px-3 py-2.5 rounded-lg transition-colors ${
                  selectedAgent?.id === agent.id
                    ? 'bg-purple-500/20 border-l-4 border-[#6B46C1]'
                    : 'hover:bg-purple-500/10'
                }`}
              >
                <button
                  onClick={() => onAgentSelect(agent)}
                  className="flex-1 text-left"
                  style={{ color: selectedAgent?.id === agent.id ? '#9F7AEA' : 'var(--text-secondary)' }}
                  title={agent.description || ''}
                >
                  <div className="font-medium text-sm">{snakeToTitleCase(agent.name)}</div>
                  {agent.description && (
                    <div 
                      className="text-xs mt-0.5 truncate" 
                      style={{ color: 'var(--text-muted)', maxWidth: '180px' }}
                    >
                      {agent.description}
                    </div>
                  )}
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    onDeleteAgent(agent)
                  }}
                  className="p-1.5 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all"
                  title="Delete agent"
                >
                  <Trash2 size={14} className="text-red-400" />
                </button>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
