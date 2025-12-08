import { Play, Square, Code, LogOut, Loader } from 'lucide-react'
import { snakeToTitleCase } from '../utils/formatters'

export default function Header({ agentName, showYaml, onToggleYaml, isRunning, onRun, onStop, onExit, onSave, isSaving, theme }) {
  const displayName = snakeToTitleCase(agentName) || agentName
  
  return (
    <div className="h-14 flex items-center justify-between px-6" style={{ background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border-color)' }}>
      {/* Left: Agent Title */}
      <div className="flex items-center gap-4">
        <h1 className="text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          {isRunning ? 'Run an Agent' : 'Edit Agent'}
        </h1>
        <span style={{ color: 'var(--text-muted)' }}>|</span>
        <span style={{ color: 'var(--text-secondary)' }}>{displayName}</span>
      </div>

      {/* Right: Actions */}
      <div className="flex items-center gap-3">
        {!isRunning && (
          <>
            {/* View Source Toggle */}
            <button
              onClick={onToggleYaml}
              className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                showYaml
                  ? 'bg-purple-500/20 text-purple-400'
                  : ''
              }`}
              style={!showYaml ? { background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' } : {}}
            >
              <Code size={18} />
              {showYaml ? 'Hide Source' : 'View Source'}
            </button>

            {/* Save Button */}
            <button 
              onClick={onSave}
              disabled={isSaving}
              className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
              style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
            >
              {isSaving && <Loader size={16} className="animate-spin" />}
              {isSaving ? 'Saving...' : 'Save'}
            </button>
          </>
        )}

        {/* Run/Stop Button */}
        {isRunning ? (
          <button
            onClick={onStop}
            className="flex items-center gap-2 px-5 py-2 bg-red-500 hover:bg-red-600 text-white font-medium rounded-lg transition-colors shadow-sm"
          >
            <Square size={18} />
            Stop
          </button>
        ) : (
          <button
            onClick={onRun}
            className="flex items-center gap-2 px-5 py-2 bg-[#805AD5] hover:bg-[#6B46C1] text-white font-medium rounded-lg transition-colors shadow-sm"
          >
            <Play size={18} />
            Run
          </button>
        )}

        {/* Exit Button (when running) */}
        {isRunning && (
          <button 
            onClick={onExit}
            className="p-2 hover:bg-gray-700/50 rounded-lg transition-colors" 
            style={{ color: 'var(--text-muted)' }}
            title="Exit Run Mode"
          >
            <LogOut size={20} />
          </button>
        )}
      </div>
    </div>
  )
}
