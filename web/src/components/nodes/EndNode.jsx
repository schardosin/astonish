import { Handle, Position } from '@xyflow/react'
import { Square } from 'lucide-react'

export default function EndNode({ data }) {
  return (
    <div className="node-end px-4 py-3 rounded-lg min-w-[160px]">
      <Handle type="target" position={Position.Top} className="!bg-red-600 !w-3 !h-3" />
      <div className="flex items-center gap-2">
        <Square size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
    </div>
  )
}
