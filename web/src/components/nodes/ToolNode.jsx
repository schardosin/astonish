import { Handle, Position } from '@xyflow/react'
import { Wrench } from 'lucide-react'

export default function ToolNode({ data }) {
  return (
    <div className={`node-tool px-4 py-3 rounded-lg min-w-[180px] ${data.isActive ? 'node-active' : ''}`}>
      <Handle type="target" position={Position.Top} className="!bg-purple-300 !w-3 !h-3" />
      <div className="flex items-center gap-2">
        <Wrench size={16} />
        <span className="font-semibold text-sm">{data.label}</span>
      </div>
      {data.description && (
        <p className="text-xs opacity-80 mt-1">{data.description}</p>
      )}
      <Handle type="source" position={Position.Bottom} className="!bg-purple-300 !w-3 !h-3" />
      {/* Hidden handles for back-edges - invisible but functional */}
      <Handle type="source" position={Position.Top} id="top-source" className="!opacity-0 !w-1 !h-1" style={{ left: '30%' }} />
      <Handle type="target" position={Position.Left} id="left" className="!opacity-0 !w-1 !h-1" />
    </div>
  )
}
