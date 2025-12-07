import { Handle, Position } from '@xyflow/react'

export default function WaypointNode({ selected }) {
  return (
    <div className="relative flex items-center justify-center" style={{ width: '16px', height: '16px' }}>
      {/* Top handles for normal top-down flow */}
      <Handle 
        type="target" 
        position={Position.Top}
        id="top-target"
        className="!opacity-0 !w-3 !h-1" 
        style={{ top: 0 }}
      />
      <Handle 
        type="source" 
        position={Position.Top}
        id="top-source"
        className="!opacity-0 !w-3 !h-1" 
        style={{ top: 0 }}
      />
      
      {/* Bottom handles for normal top-down flow */}
      <Handle 
        type="target" 
        position={Position.Bottom}
        id="bottom-target"
        className="!opacity-0 !w-3 !h-1" 
        style={{ bottom: 0 }}
      />
      <Handle 
        type="source" 
        position={Position.Bottom}
        id="bottom-source"
        className="!opacity-0 !w-3 !h-1" 
        style={{ bottom: 0 }}
      />
      
      {/* Default handles (no ID) for backward compatibility */}
      <Handle 
        type="target" 
        position={Position.Top}
        className="!opacity-0 !w-3 !h-1" 
        style={{ top: 0 }}
      />
      <Handle 
        type="source" 
        position={Position.Bottom}
        className="!opacity-0 !w-3 !h-1" 
        style={{ bottom: 0 }}
      />
      
      {/* The Visual Dot - visible on hover or selection */}
      <div 
        className={`
          w-3 h-3 rounded-full cursor-move transition-all
          ${selected 
            ? 'bg-purple-500 ring-2 ring-purple-300 scale-125' 
            : 'bg-gray-400 hover:bg-purple-400 hover:scale-110'
          }
        `}
        style={{
          boxShadow: selected ? '0 0 8px rgba(139, 92, 246, 0.5)' : 'none'
        }}
      />
    </div>
  )
}
