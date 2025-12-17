import { useCallback, useMemo } from 'react'
import { 
  BaseEdge, 
  EdgeLabelRenderer, 
  useReactFlow,
  useStore
} from '@xyflow/react'

/**
 * EditableEdge - Orthogonal edge with segment manipulation
 */
export default function EditableEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style = {},
  markerEnd,
  selected,
  data,
}) {
  const { setEdges, screenToFlowPosition } = useReactFlow()

  // 1. Get points or initialize defaults
  // We don't save defaults back immediately to avoid infinite loops, 
  // we just calculate them for render if missing.
  // Actually, for interaction we need them stable. 
  // Let's use memoized points calculation.
  const points = useMemo(() => {
    if (data?.points && Array.isArray(data.points) && data.points.length > 0) {
      return data.points
    }
    // Default: Center Step (Z-shape)
    // Horizontal start -> Vertical mid -> Horizontal end
    const midX = (sourceX + targetX) / 2
    return [
      { x: midX, y: sourceY },
      { x: midX, y: targetY }
    ]
  }, [data?.points, sourceX, sourceY, targetX, targetY])

  // 2. Build Path
  // Path goes: Source -> Point[0] -> Point[1] -> ... -> Target
  const path = useMemo(() => {
    let p = `M ${sourceX} ${sourceY}`
    
    // Safety check
    if (!points.length) {
       p += ` L ${targetX} ${targetY}`
       return p
    }

    for (const point of points) {
      p += ` L ${point.x} ${point.y}`
    }
    p += ` L ${targetX} ${targetY}`
    return p
  }, [sourceX, sourceY, targetX, targetY, points])

  // 3. Handle Drag Logic
  const onHandleMouseDown = useCallback((e, index) => {
    e.stopPropagation()
    e.preventDefault()

    // Determine segment we are dragging
    // Segments: 
    // 0: Source -> P[0]
    // i: P[i-1] -> P[i]
    // n: P[n-1] -> Target
    
    // We need the start and end of the segment to know its orientation
    const start = index === 0 ? { x: sourceX, y: sourceY } : points[index - 1]
    const end = index === points.length ? { x: targetX, y: targetY } : points[index]

    const isHorizontal = Math.abs(start.y - end.y) < Math.abs(start.x - end.x)

    // Capture initial state
    const startX = e.clientX
    const startY = e.clientY
    const initialPoints = [...points]

    const handleMouseMove = (moveEvent) => {
      const currentFlow = screenToFlowPosition({ x: moveEvent.clientX, y: moveEvent.clientY })
      const startFlow = screenToFlowPosition({ x: startX, y: startY })
      
      const deltaX = currentFlow.x - startFlow.x
      const deltaY = currentFlow.y - startFlow.y

      const newPoints = initialPoints.map(p => ({ ...p }))

      if (isHorizontal) {
        // Moving Horizontal Segment UP/DOWN
        // We need to move the 'y' of both endpoints of this segment.
        
        // If start is Source (index 0), we can't move SourceY!
        // But maybe we handle that by just not doing anything? 
        // Or users expect it to move? If attached to a node, usually fixed.
        
        // Let's try to update 'y' for the involved internal points.
        if (index > 0) {
           newPoints[index - 1].y = initialPoints[index - 1].y + deltaY
        }
        if (index < points.length) {
           newPoints[index].y = initialPoints[index].y + deltaY
        }
      } else {
        // Moving Vertical Segment LEFT/RIGHT
        if (index > 0) {
           newPoints[index - 1].x = initialPoints[index - 1].x + deltaX
        }
        if (index < points.length) {
           newPoints[index].x = initialPoints[index].x + deltaX
        }
      }

      setEdges(edges => edges.map(edge => {
        if (edge.id === id) {
          return { ...edge, data: { ...edge.data, points: newPoints } }
        }
        return edge
      }))
    }

    const handleMouseUp = () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

  }, [points, sourceX, sourceY, targetX, targetY, id, setEdges, screenToFlowPosition])


  // 4. Calculate Handle Positions (Midpoints of segments)
  const handles = []
  
  // Segment 0: Source -> P[0]
  if (points.length > 0) {
    handles.push({
      x: (sourceX + points[0].x) / 2,
      y: (sourceY + points[0].y) / 2,
      index: 0
    })
    
    // Internal Segments
    for (let i = 0; i < points.length - 1; i++) {
      handles.push({
        x: (points[i].x + points[i+1].x) / 2,
        y: (points[i].y + points[i+1].y) / 2,
        index: i + 1
      })
    }
    
    // Last Segment: P[last] -> Target
    const lastP = points[points.length - 1]
    handles.push({
      x: (lastP.x + targetX) / 2,
      y: (lastP.y + targetY) / 2,
      index: points.length
    })
  } else {
     // No points -> Source -> Target direct
     handles.push({
        x: (sourceX + targetX) / 2,
        y: (sourceY + targetY) / 2,
        index: 0
     })
  }

  return (
    <>
      <BaseEdge 
        path={path} 
        markerEnd={markerEnd} 
        style={{
          ...style,
          strokeWidth: selected ? 3 : 2,
          stroke: selected ? '#5b21b6' : style.stroke || '#805AD5' // Use theme colors ideally
        }}
        interactionWidth={20}
      />
      
      {/* Invisible wider path for easier selection */}
      <path
        d={path}
        fill="none"
        stroke="transparent"
        strokeWidth={20}
        style={{ cursor: 'pointer' }}
      />
      
      {/* Handles */}
      {selected && (
        <EdgeLabelRenderer>
          {handles.map((handle, i) => (
             <div
               key={i}
               className="nodrag nopan"
               style={{
                 position: 'absolute',
                 transform: `translate(-50%, -50%) translate(${handle.x}px, ${handle.y}px)`,
                 width: 12,
                 height: 12,
                 backgroundColor: '#3b82f6',
                 border: '2px solid white',
                 borderRadius: 2,
                 cursor: 'grab',
                 pointerEvents: 'all',
                 zIndex: 2000
               }}
               onMouseDown={(e) => onHandleMouseDown(e, handle.index)}
             />
          ))}
        </EdgeLabelRenderer>
      )}
    </>
  )
}
