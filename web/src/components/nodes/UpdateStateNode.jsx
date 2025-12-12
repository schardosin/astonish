import { Handle, Position } from '@xyflow/react'
import { Settings } from 'lucide-react'

export default function UpdateStateNode({ data }) {
  return (
    <div 
      className={`px-4 py-3 rounded-lg min-w-[180px] ${data.isActive ? 'node-active' : ''}`}
      style={{
        background: 'linear-gradient(135deg, #4A5568 0%, #2D3748 100%)',
        border: '2px solid #2D3748',
        color: 'white',
        boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)'
      }}
    >
      <Handle type="target" position={Position.Top} className="!bg-gray-400 !w-3 !h-3" />
      <div className="flex items-center gap-2">
        <Settings size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      <Handle type="source" position={Position.Bottom} className="!bg-gray-400 !w-3 !h-3" />
    </div>
  )
}
