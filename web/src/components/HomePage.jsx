import { Plus, Settings, Plug, Cpu, ArrowRight } from 'lucide-react'

export default function HomePage({ 
  onCreateAgent, 
  onOpenSettings, 
  onOpenMCP,
  onBrowseFlows,
  defaultProvider,
  defaultModel,
  theme 
}) {
  return (
    <div 
      className="flex-1 flex flex-col items-center justify-center p-8 overflow-y-auto"
      style={{ background: 'var(--bg-primary)' }}
    >
      {/* Welcome Hero */}
      <div className="text-center mb-12">
        <div className="inline-flex items-center justify-center w-28 h-28 mb-6">
          <img 
            src="/astonish-logo.svg" 
            alt="Astonish Logo" 
            className="w-full h-full"
          />
        </div>
        <h1 className="text-4xl font-bold mb-3" style={{ color: 'var(--text-primary)' }}>
          Welcome to Astonish Studio
        </h1>
        <p className="text-lg max-w-xl mx-auto" style={{ color: 'var(--text-muted)' }}>
          Build powerful AI agents with a visual flow editor. Create, test, and deploy intelligent automation.
        </p>
        
        {/* Provider Badge */}
        {defaultProvider && (
          <div className="mt-6 inline-flex items-center gap-2 px-4 py-2 rounded-full" 
            style={{ background: 'var(--bg-tertiary)', border: '1px solid var(--border-color)' }}
          >
            <Cpu size={16} className="text-purple-400" />
            <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>
              {defaultProvider} • {defaultModel}
            </span>
          </div>
        )}
      </div>

      {/* Quick Actions */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl w-full">
        {/* Browse Flows */}
        <button
          onClick={onBrowseFlows}
          className="group p-6 rounded-2xl text-left transition-all hover:scale-[1.02] hover:shadow-xl"
          style={{ 
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border-color)'
          }}
        >
          <div className="w-14 h-14 rounded-xl bg-gradient-to-br from-indigo-600 to-blue-600 flex items-center justify-center mb-4 shadow-lg group-hover:shadow-indigo-500/20 transition-all">
            <Settings size={28} className="text-white transform rotate-90" /> {/* Using Settings rotated as a placeholder or import Layout/Folder */}
          </div>
          <h3 className="text-xl font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            Browse Flows
            <ArrowRight size={18} className="opacity-0 group-hover:opacity-100 transition-opacity text-indigo-400" />
          </h3>
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
            View and manage your existing library of AI agent flows.
          </p>
        </button>

        {/* Create Agent */}
        <button
          onClick={onCreateAgent}
          className="group p-6 rounded-2xl text-left transition-all hover:scale-[1.02] hover:shadow-xl"
          style={{ 
            background: 'linear-gradient(135deg, rgba(147, 51, 234, 0.15), rgba(59, 130, 246, 0.15))',
            border: '1px solid rgba(147, 51, 234, 0.3)'
          }}
        >
          <div className="w-14 h-14 rounded-xl bg-gradient-to-br from-purple-600 to-blue-500 flex items-center justify-center mb-4 shadow-lg group-hover:shadow-purple-500/30 transition-all">
            <Plus size={28} className="text-white" />
          </div>
          <h3 className="text-xl font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            Create Agent
            <ArrowRight size={18} className="opacity-0 group-hover:opacity-100 transition-opacity text-purple-400" />
          </h3>
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
            Build a new AI agent flow from scratch or use the AI assistant.
          </p>
        </button>

        {/* Settings */}
        <button
          onClick={onOpenSettings}
          className="group p-6 rounded-2xl text-left transition-all hover:scale-[1.02] hover:shadow-xl"
          style={{ 
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border-color)'
          }}
        >
          <div className="w-14 h-14 rounded-xl bg-gradient-to-br from-gray-600 to-gray-700 flex items-center justify-center mb-4 shadow-lg group-hover:shadow-gray-500/20 transition-all">
            <Settings size={28} className="text-white" />
          </div>
          <h3 className="text-xl font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            Settings
            <ArrowRight size={18} className="opacity-0 group-hover:opacity-100 transition-opacity text-gray-400" />
          </h3>
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
            Configure AI providers, API keys, and default models.
          </p>
        </button>

        {/* MCP Servers */}
        <button
          onClick={onOpenMCP}
          className="group p-6 rounded-2xl text-left transition-all hover:scale-[1.02] hover:shadow-xl"
          style={{ 
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border-color)'
          }}
        >
          <div className="w-14 h-14 rounded-xl bg-gradient-to-br from-teal-600 to-cyan-500 flex items-center justify-center mb-4 shadow-lg group-hover:shadow-teal-500/20 transition-all">
            <Plug size={28} className="text-white" />
          </div>
          <h3 className="text-xl font-semibold mb-2 flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
            MCP Servers
            <ArrowRight size={18} className="opacity-0 group-hover:opacity-100 transition-opacity text-teal-400" />
          </h3>
          <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
            Connect external tools via Model Context Protocol.
          </p>
        </button>
      </div>

      {/* Bottom hint */}
      <div className="mt-12 text-center">
        <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
          Select an agent from the sidebar or create a new one to get started
        </p>
      </div>
    </div>
  )
}
