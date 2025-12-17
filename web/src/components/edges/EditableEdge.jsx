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
  // Label props
  label,
  labelStyle,
  labelShowBg = true, 
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
}) {
  const { setEdges, screenToFlowPosition } = useReactFlow()

  // 1. Get points or initialize defaults
  const points = useMemo(() => {
    if (data?.points && Array.isArray(data.points) && data.points.length > 0) {
      return data.points
    }
    // Default: Center Step (Z-shape)
    const midX = (sourceX + targetX) / 2
    return [
      { x: midX, y: sourceY },
      { x: midX, y: targetY }
    ]
  }, [data?.points, sourceX, sourceY, targetX, targetY])

  // 2. Build Path and Label Position
  const { path, labelX, labelY } = useMemo(() => {
    let p = `M ${sourceX} ${sourceY}`
    const allPoints = [{x: sourceX, y: sourceY}, ...points, {x: targetX, y: targetY}]
    
    // Build path string
    for (let i = 0; i < points.length; i++) {
      p += ` L ${points[i].x} ${points[i].y}`
    }
    p += ` L ${targetX} ${targetY}`
    
    // Calculate Label Position (Polyline Midpoint)
    let totalLen = 0
    const segmentLens = []
    
    // Calculate lengths
    for (let i=0; i<allPoints.length-1; i++) {
        const p1 = allPoints[i]
        const p2 = allPoints[i+1]
        const d = Math.hypot(p2.x - p1.x, p2.y - p1.y)
        segmentLens.push(d)
        totalLen += d
    }
    
    let targetLen = totalLen / 2
    let currentLen = 0
    let lx = (sourceX + targetX) / 2
    let ly = (sourceY + targetY) / 2
    
    // Find point
    for (let i=0; i<allPoints.length-1; i++) {
        const len = segmentLens[i]
        if (currentLen + len >= targetLen) {
            const remaining = targetLen - currentLen
            const ratio = len === 0 ? 0 : remaining / len
            const p1 = allPoints[i]
            const p2 = allPoints[i+1]
            lx = p1.x + (p2.x - p1.x) * ratio
            ly = p1.y + (p2.y - p1.y) * ratio
            break
        }
        currentLen += len
    }

    return { path: p, labelX: lx, labelY: ly }
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

    // --- DRAG START LOGIC ---
    // We determine if we need to isolate the segment from Fixed Nodes (Source/Target)
    // to allow orthogonal movement without creating diagonals.
    
    let currentPoints = [...points]
    let activeLeftIndex = index - 1 // Index in 'currentPoints' of the start of the dragged segment
    let activeRightIndex = index    // Index in 'currentPoints' of the end of the dragged segment
    
    const STUB_LEN = 30
    
    // 1. Isolate FROM SOURCE if needed
    if (activeLeftIndex === -1) {
      // Connected to Source. We need to insert a stub to preserve Source connection direction.
      // Direction: If dragging Horizontal, segment is Horizontal. 
      // We assume Source allows Horizontal connection.
      // If we move Y, we need: Source -> Stub(Fixed Y) -> Corner(Moving Y) -> ...
      
      const dirX = Math.sign(targetX - sourceX) || 1
      const dirY = Math.sign(targetY - sourceY) || 1
      
      let pStub, pCorner
      
      if (isHorizontal) {
         // Horizontal Drag (Moving Y). Original segment S->P0 is Horizontal.
         // Insert Horizontal stub: (Sx + len, Sy)
         pStub = { x: sourceX + (dirX * STUB_LEN), y: sourceY }
         pCorner = { x: sourceX + (dirX * STUB_LEN), y: sourceY } // Initially same Y, will move
      } else {
         // Vertical Drag (Moving X). Original segment S->P0 is Vertical.
         // Insert Vertical stub: (Sx, Sy + len)
         pStub = { x: sourceX, y: sourceY + (dirY * STUB_LEN) }
         pCorner = { x: sourceX, y: sourceY + (dirY * STUB_LEN) }
      }
      
      currentPoints.unshift(pStub, pCorner)
      activeLeftIndex += 2
      activeRightIndex += 2
    }
    
    // 2. Isolate FROM TARGET if needed
    if (activeRightIndex === points.length) { // points.length is passed from closure, but 'currentPoints' might have changed size?
      // Wait, 'points' closure variable is unchanged. 'activeRightIndex' is relative to 'currentPoints'.
      // If we unshifted 2, activeRightIndex increased by 2.
      // But we need to check if it matches the *end* of the array.
      // Original check: index === points.length. 
      // Now: activeRightIndex === currentPoints.length? YES.
    }
    
    // Actually simpler: re-check against currentPoints using the shifted index
    if (activeRightIndex === currentPoints.length) {
       // Connected to Target.
       const dirX = Math.sign(sourceX - targetX) || -1 // backwards from target
       const dirY = Math.sign(sourceY - targetY) || -1
       
       let pStub, pCorner
       
       if (isHorizontal) {
         // Horizontal Drag. T is fixed.
         // Segment ...->T is Horizontal.
         // We need: ... -> Corner(Moving Y) -> Stub(Fixed Y) -> T
         pStub = { x: targetX + (dirX * STUB_LEN), y: targetY }
         pCorner = { x: targetX + (dirX * STUB_LEN), y: targetY }
       } else {
         // Vertical Drag.
         pStub = { x: targetX, y: targetY + (dirY * STUB_LEN) }
         pCorner = { x: targetX, y: targetY + (dirY * STUB_LEN) }
       }
       
       currentPoints.push(pCorner, pStub)
       // Indices don't shift for right-side insertion
    }

    // Apply insertions immediately so the user sees the 'break'
    if (currentPoints.length !== points.length) {
       setEdges(edges => edges.map(edge => {
        if (edge.id === id) return { ...edge, data: { ...edge.data, points: currentPoints } }
        return edge
      }))
    }
    
    // Capture state for dragging
    const startX = e.clientX
    const startY = e.clientY
    // map currentPoints to ensure deep copy for base calculation
    const initialDragPoints = currentPoints.map(p => ({...p})) 
    let currentDragPoints = [...initialDragPoints]

    const handleMouseMove = (moveEvent) => {
      const currentFlow = screenToFlowPosition({ x: moveEvent.clientX, y: moveEvent.clientY })
      const startFlow = screenToFlowPosition({ x: startX, y: startY })
      
      const deltaX = currentFlow.x - startFlow.x
      const deltaY = currentFlow.y - startFlow.y

      const newPoints = initialDragPoints.map(p => ({ ...p }))

      if (isHorizontal) {
        // Move Y
        if (activeLeftIndex >= 0) {
           newPoints[activeLeftIndex].y = initialDragPoints[activeLeftIndex].y + deltaY
        }
        if (activeRightIndex < newPoints.length) {
           newPoints[activeRightIndex].y = initialDragPoints[activeRightIndex].y + deltaY
        }
      } else {
        // Move X
        if (activeLeftIndex >= 0) {
           newPoints[activeLeftIndex].x = initialDragPoints[activeLeftIndex].x + deltaX
        }
        if (activeRightIndex < newPoints.length) {
           newPoints[activeRightIndex].x = initialDragPoints[activeRightIndex].x + deltaX
        }
      }
      
      currentDragPoints = newPoints

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
        label={label}
        labelX={labelX}
        labelY={labelY}
        labelStyle={labelStyle}
        labelShowBg={labelShowBg}
        labelBgStyle={labelBgStyle}
        labelBgPadding={labelBgPadding}
        labelBgBorderRadius={labelBgBorderRadius}
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
