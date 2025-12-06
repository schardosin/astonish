import { Handle, Position } from '@xyflow/react'
import { Play } from 'lucide-react'

export default function StartNode({ data }) {
  return (
    <div className="node-start px-4 py-3 rounded-lg min-w-[160px]">
      <div className="flex items-center gap-2">
        <Play size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      {data.description && (
        <p className="text-xs opacity-80 mt-1">{data.description}</p>
      )}
      <Handle type="source" position={Position.Bottom} className="!bg-green-600 !w-3 !h-3" />
    </div>
  )
}
