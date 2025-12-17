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
    let currentDragPoints = [...initialPoints] // specific variable to track latest state for mouseUp

    const handleMouseMove = (moveEvent) => {
      const currentFlow = screenToFlowPosition({ x: moveEvent.clientX, y: moveEvent.clientY })
      const startFlow = screenToFlowPosition({ x: startX, y: startY })
      
      const deltaX = currentFlow.x - startFlow.x
      const deltaY = currentFlow.y - startFlow.y

      const newPoints = initialPoints.map(p => ({ ...p }))

      if (isHorizontal) {
        // Moving Horizontal Segment UP/DOWN
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
      
      currentDragPoints = newPoints // update latest

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
      
      // Simplify path: Remove collinear or overlapping points
      if (currentDragPoints.length > 0) {
        const simplified = []
        // We need coordinates of Source and Target to check full path
        // but Source/Target props might be stale if we relied on them changing? 
        // No, sourceX/targetX are from current render cycle. 
        // We assume they haven't moved during the drag (user is dragging edge, not node).
        
        const fullPath = [
          { x: sourceX, y: sourceY }, 
          ...currentDragPoints, 
          { x: targetX, y: targetY }
        ]
        
        // Check internal points (indices 1 to length-2 in fullPath)
        // Corresponds to indices 0 to length-1 in currentDragPoints
        for (let i = 1; i < fullPath.length - 1; i++) {
          const prev = fullPath[i-1]
          const curr = fullPath[i]
          const next = fullPath[i+1]
          
          // 1. Check if point is very close to previous (redundant)
          if (Math.hypot(curr.x - prev.x, curr.y - prev.y) < 5) {
             continue // Drop curr
          }
           // 2. Check if point is very close to next (redundant)
          if (Math.hypot(curr.x - next.x, curr.y - next.y) < 5) {
             continue // Drop curr
          }
          
          // 3. Check collinearity
          const isCollinearHorizontal = Math.abs(prev.y - curr.y) < 2 && Math.abs(curr.y - next.y) < 2
          const isCollinearVertical = Math.abs(prev.x - curr.x) < 2 && Math.abs(curr.x - next.x) < 2
          
          if (isCollinearHorizontal || isCollinearVertical) {
            continue // Drop curr
          }
          
          simplified.push(curr)
        }
        
        // If we simplified anything, update store
        if (simplified.length !== currentDragPoints.length) {
           setEdges(edges => edges.map(edge => {
            if (edge.id === id) {
              return { ...edge, data: { ...edge.data, points: simplified } }
            }
            return edge
          }))
        }
      }
      
      // Dispatch event to trigger immediate save (handled in FlowCanvas)
      window.dispatchEvent(new CustomEvent('astonish:edge-drag-stop'))
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)

  }, [points, sourceX, sourceY, targetX, targetY, id, setEdges, screenToFlowPosition])


  // 4. Calculate Handle Positions (Midpoints of segments)
  // 4. Calculate Handle Positions (Midpoints of segments)
  const handles = []
  const MIN_SEGMENT_LENGTH = 30
  
  // Helper to add handle if segment is long enough
  const addHandleIfLongEnough = (p1, p2, index) => {
    const dist = Math.hypot(p2.x - p1.x, p2.y - p1.y)
    if (dist >= MIN_SEGMENT_LENGTH) {
      handles.push({
        x: (p1.x + p2.x) / 2,
        y: (p1.y + p2.y) / 2,
        index
      })
    }
  }
  
  // Segment 0: Source -> P[0]
  if (points.length > 0) {
    addHandleIfLongEnough({ x: sourceX, y: sourceY }, points[0], 0)
    
    // Internal Segments
    for (let i = 0; i < points.length - 1; i++) {
      addHandleIfLongEnough(points[i], points[i+1], i + 1)
    }
    
    // Last Segment: P[last] -> Target
    const lastP = points[points.length - 1]
    addHandleIfLongEnough(lastP, { x: targetX, y: targetY }, points.length)
  } else {
     // No points -> Source -> Target direct
     addHandleIfLongEnough({ x: sourceX, y: sourceY }, { x: targetX, y: targetY }, 0)
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
