import { Handle, Position } from '@xyflow/react'
import { Edit3 } from 'lucide-react'

export default function InputNode({ data }) {
  return (
    <div className={`node-input px-4 py-3 rounded-lg min-w-[180px] ${data.isActive ? 'node-active' : ''}`}>
      <Handle type="target" position={Position.Top} className="!bg-purple-400 !w-3 !h-3" />
      <div className="flex items-center gap-2 text-gray-700">
        <Edit3 size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      {data.description && (
        <p className="text-xs text-gray-600 mt-1">{data.description}</p>
      )}
      <Handle type="source" position={Position.Bottom} className="!bg-purple-400 !w-3 !h-3" />
    </div>
  )
}
