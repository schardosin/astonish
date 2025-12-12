import { Handle, Position } from '@xyflow/react'
import { Brain, Check, AlertTriangle } from 'lucide-react'

export default function LlmNode({ data }) {
  const hasError = data.hasError
  
  return (
    <div 
      className={`node-llm px-4 py-3 rounded-lg min-w-[200px] ${data.isActive ? 'node-active' : ''}`}
      style={hasError ? { border: '2px solid #ef4444', boxShadow: '0 0 10px rgba(239, 68, 68, 0.4)' } : undefined}
      title={hasError ? data.errorMessage : undefined}
    >
      <Handle type="target" position={Position.Top} className="!bg-purple-300 !w-3 !h-3" />
      <div className="flex items-center gap-2">
        {hasError ? (
          <AlertTriangle size={16} className="text-red-500" />
        ) : data.isActive ? (
          <div className="w-4 h-4 rounded-full bg-white flex items-center justify-center">
            <Check size={12} className="text-[#6B46C1]" />
          </div>
        ) : (
          <Brain size={16} />
        )}
        <span className="font-semibold text-sm">{data.label}</span>
        <span className="text-xs opacity-70">(LLM)</span>
      </div>
      {data.description && (
        <p className="text-xs opacity-80 mt-1">{data.description}</p>
      )}
      {hasError && (
        <p className="text-xs mt-1 text-red-400 truncate max-w-[200px]">{data.errorMessage}</p>
      )}
      <Handle type="source" position={Position.Bottom} className="!bg-purple-300 !w-3 !h-3" />
    </div>
  )
}
