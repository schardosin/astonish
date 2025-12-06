import { Handle, Position } from '@xyflow/react'
import { Edit3 } from 'lucide-react'

export default function InputNode({ data }) {
  return (
    <div 
      className={`px-4 py-3 rounded-lg min-w-[180px] ${data.isActive ? 'node-active' : ''}`}
      style={{
        background: 'var(--node-input)',
        border: '2px solid rgba(107, 70, 193, 0.3)',
        boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)'
      }}
    >
      <Handle type="target" position={Position.Left} className="!bg-purple-400 !w-3 !h-3" />
      <div className="flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
        <Edit3 size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      {data.description && (
        <p className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>{data.description}</p>
      )}
      <Handle type="source" position={Position.Right} className="!bg-purple-400 !w-3 !h-3" />
    </div>
  )
}
