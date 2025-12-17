import { Handle, Position } from '@xyflow/react'
import { AlertTriangle, Check } from 'lucide-react'

/**
 * Base node component with Overflow-style design
 * Uses CSS variables for theme-aware colors (light/dark)
 */
export default function OverflowNode({ 
  data, 
  selected, 
  icon: Icon, 
  nodeType,  // e.g. "LLM", "Start", etc.
  hasTopHandle = true,
  hasBottomHandle = true,
  iconColor = '#8b5cf6'  // Purple accent (can be overridden per node type)
}) {
  const hasError = data.hasError
  const isActive = data.isActive

  // Determine which icon to show
  const renderIcon = () => {
    if (hasError) {
      return <AlertTriangle size={20} style={{ color: '#ef4444' }} />
    }
    if (isActive) {
      return (
        <div className="w-6 h-6 rounded-full bg-white flex items-center justify-center">
          <Check size={16} style={{ color: '#6B46C1' }} />
        </div>
      )
    }
    return <Icon size={20} style={{ color: iconColor }} />
  }

  return (
    <div 
      className="overflow-node"
      style={{
        background: 'var(--overflow-node-bg)',
        borderRadius: '12px',
        border: hasError 
          ? '2px solid #ef4444' 
          : selected 
            ? '2px solid var(--accent)'
            : '1px solid var(--overflow-node-border)',
        boxShadow: hasError 
          ? '0 0 10px rgba(239, 68, 68, 0.4)' 
          : selected 
            ? '0 0 0 2px var(--accent-soft), 0 4px 12px rgba(0,0,0,0.15)' 
            : '0 4px 12px rgba(0,0,0,0.1)',
        minWidth: '180px',
        maxWidth: '220px',
        padding: '14px 16px',
      }}
    >
      {/* Top Handle */}
      {hasTopHandle && (
        <Handle 
          type="target" 
          position={Position.Top} 
          className="!w-2 !h-2"
          style={{ 
            background: 'var(--overflow-handle-bg)',
            borderWidth: '2px',
            borderColor: 'var(--overflow-handle-border)',
          }}
        />
      )}
      
      {/* Content */}
      <div className="flex items-center gap-3">
        {/* Icon Container */}
        <div 
          style={{
            background: 'var(--overflow-icon-bg)',
            borderRadius: '8px',
            padding: '8px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          {renderIcon()}
        </div>
        
        {/* Text */}
        <div className="flex flex-col min-w-0">
          <span 
            style={{ 
              color: 'var(--overflow-node-title)',
              fontWeight: 600,
              fontSize: '14px',
              lineHeight: '1.3',
            }}
          >
            {nodeType}
          </span>
          <span 
            style={{ 
              color: 'var(--overflow-node-subtitle)',
              fontSize: '12px',
              lineHeight: '1.3',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {data.label}
          </span>
        </div>
      </div>
      
      {/* Error Message */}
      {hasError && data.errorMessage && (
        <p 
          className="truncate mt-2"
          style={{ 
            color: '#f87171', 
            fontSize: '11px',
            maxWidth: '180px',
          }}
        >
          {data.errorMessage}
        </p>
      )}
      
      {/* Bottom Handle */}
      {hasBottomHandle && (
        <Handle 
          type="source" 
          position={Position.Bottom} 
          className="!w-2 !h-2"
          style={{ 
            background: 'var(--overflow-handle-bg)',
            borderWidth: '2px',
            borderColor: 'var(--overflow-handle-border)',
          }}
        />
      )}
      
      {/* Hidden handles for back-edges (loops that go upward) */}
      <Handle 
        type="source" 
        position={Position.Top} 
        id="top-source" 
        className="!opacity-0 !w-1 !h-1" 
        style={{ left: '30%' }} 
      />
      <Handle 
        type="target" 
        position={Position.Left} 
        id="left" 
        className="!opacity-0 !w-1 !h-1" 
      />
    </div>
  )
}
