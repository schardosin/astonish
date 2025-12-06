import { Play, Square, Code, LogOut } from 'lucide-react'

export default function Header({ agentName, showYaml, onToggleYaml, isRunning, onRun, onStop }) {
  return (
    <div className="h-14 bg-white border-b border-gray-200 flex items-center justify-between px-6">
      {/* Left: Agent Title */}
      <div className="flex items-center gap-4">
        <h1 className="text-lg font-semibold text-gray-800">
          {isRunning ? 'Run an Agent' : 'Edit Agent'}
        </h1>
        <span className="text-gray-400">|</span>
        <span className="text-gray-600">{agentName}</span>
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
                  ? 'bg-purple-100 text-[#6B46C1]'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
              }`}
            >
              <Code size={18} />
              {showYaml ? 'Hide Source' : 'View Source'}
            </button>

            {/* Save Button */}
            <button className="px-4 py-2 bg-gray-100 text-gray-600 hover:bg-gray-200 rounded-lg text-sm font-medium transition-colors">
              Save
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
          <button className="p-2 text-gray-400 hover:text-gray-600">
            <LogOut size={20} />
          </button>
        )}
      </div>
    </div>
  )
}
