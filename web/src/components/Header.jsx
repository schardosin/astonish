import { Play, Code, LogOut, Undo2, Redo2, CircleDot, Copy, Lock } from 'lucide-react'
import { snakeToTitleCase } from '../utils/formatters'

export default function Header({
  agentName, agentSource, showYaml, onToggleYaml, isRunning, onRun, onExit,
  canUndo, canRedo, onUndo, onRedo, readOnly, onCopyToLocal
}) {
  let displayName = agentName
  if (agentSource === 'store' && agentName.includes('/')) {
    const [tap, flowName] = agentName.split('/')
    displayName = `${tap.toLowerCase()} - ${snakeToTitleCase(flowName)}`
  } else {
    displayName = snakeToTitleCase(agentName) || agentName
  }

  // Determine mode text and styling
  const getModeInfo = () => {
    if (isRunning) return { text: 'Run mode', icon: CircleDot, color: 'var(--accent)' }
    if (readOnly) return { text: 'Read Only', icon: Lock, color: '#f59e0b' }
    return { text: 'Design mode', icon: CircleDot, color: 'var(--accent)' }
  }
  const modeInfo = getModeInfo()
  const ModeIcon = modeInfo.icon

  return (
    <div className="h-14 flex items-center justify-between px-5" style={{ background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border-color)' }}>
      <div className="flex items-center gap-3">
        <div 
          className="px-3 py-1 rounded-full text-xs font-semibold flex items-center gap-2" 
          style={{ 
            background: readOnly ? 'rgba(245, 158, 11, 0.15)' : 'var(--accent-soft)', 
            color: modeInfo.color 
          }}
        >
          <ModeIcon size={14} />
          {modeInfo.text}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
            {displayName}
          </span>
          <span className="text-xs px-2 py-1 rounded-full" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>
            {agentSource === 'store' ? 'Store' : 'Local'}
          </span>
        </div>
      </div>

      <div className="flex items-center gap-2">
        {!isRunning && (
          <div className="flex items-center gap-1 mr-2">
            <button
              onClick={onUndo}
              disabled={!canUndo}
              className="p-2 rounded-full transition-colors disabled:opacity-30"
              style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
              title="Undo"
            >
              <Undo2 size={18} />
            </button>
            <button
              onClick={onRedo}
              disabled={!canRedo}
              className="p-2 rounded-full transition-colors disabled:opacity-30"
              style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}
              title="Redo"
            >
              <Redo2 size={18} />
            </button>
          </div>
        )}

        {!isRunning && (
          <button
            onClick={onToggleYaml}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            style={{ background: showYaml ? 'var(--accent-soft)' : 'var(--bg-tertiary)', color: showYaml ? 'var(--accent)' : 'var(--text-secondary)' }}
          >
            <Code size={18} />
            {showYaml ? 'Hide Source' : 'View Source'}
          </button>
        )}



        {!isRunning && (
          <button
            onClick={onRun}
            className="flex items-center gap-2 px-5 py-2 text-white font-semibold rounded-lg transition-all shadow-md hover:shadow-lg hover:scale-[1.02]"
            style={{ background: 'linear-gradient(135deg, #a855f7 0%, #7c3aed 100%)' }}
          >
            <Play size={18} />
            Run
          </button>
        )}

        {/* Copy to Local button for read-only store flows */}
        {!isRunning && readOnly && onCopyToLocal && (
          <button
            onClick={onCopyToLocal}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors text-white"
            style={{ background: 'linear-gradient(135deg, #3b82f6 0%, #10b981 100%)' }}
          >
            <Copy size={16} />
            Copy to Local
          </button>
        )}

        {isRunning && (
          <button
            onClick={onExit}
            className="p-2 rounded-full transition-colors"
            style={{ color: 'var(--text-muted)', border: '1px solid var(--border-color)' }}
            title="Exit Run Mode"
          >
            <LogOut size={20} />
          </button>
        )}
      </div>
    </div>
  )
}
