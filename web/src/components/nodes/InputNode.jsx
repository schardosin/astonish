import { Handle, Position } from '@xyflow/react'
import { Edit3, AlertTriangle } from 'lucide-react'

export default function InputNode({ data }) {
  const hasError = data.hasError
  
  return (
    <div 
      className={`px-4 py-3 rounded-lg min-w-[180px] ${data.isActive ? 'node-active' : ''}`}
      style={{
        background: 'var(--node-input)',
        border: hasError ? '2px solid #ef4444' : '2px solid rgba(107, 70, 193, 0.3)',
        boxShadow: hasError ? '0 0 10px rgba(239, 68, 68, 0.4)' : '0 4px 6px -1px rgba(0, 0, 0, 0.1)'
      }}
      title={hasError ? data.errorMessage : undefined}
    >
      <Handle type="target" position={Position.Top} className="!bg-purple-400 !w-3 !h-3" />
      <div className="flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
        {hasError ? <AlertTriangle size={16} className="text-red-500" /> : <Edit3 size={16} />}
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      {data.description && (
        <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>{data.description}</p>
      )}
      {hasError && (
        <p className="text-xs mt-1 text-red-400 truncate max-w-[200px]">{data.errorMessage}</p>
      )}
      <Handle type="source" position={Position.Bottom} className="!bg-purple-400 !w-3 !h-3" />
      {/* Hidden handles for back-edges - invisible but functional */}
      <Handle type="source" position={Position.Top} id="top-source" className="!opacity-0 !w-1 !h-1" style={{ left: '30%' }} />
      <Handle type="target" position={Position.Left} id="left" className="!opacity-0 !w-1 !h-1" />
    </div>
  )
}
