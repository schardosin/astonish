import { useCallback, useMemo, useEffect, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  useReactFlow,
  Panel,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Edit3, Brain, Wrench, Settings, MessageSquare, Sparkles } from 'lucide-react'

import StartNode from './nodes/StartNode'
import EndNode from './nodes/EndNode'
import InputNode from './nodes/InputNode'
import LlmNode from './nodes/LlmNode'
import ToolNode from './nodes/ToolNode'
import OutputNode from './nodes/OutputNode'
import UpdateStateNode from './nodes/UpdateStateNode'
import WaypointNode from './nodes/WaypointNode'

const nodeTypes = {
  start: StartNode,
  end: EndNode,
  input: InputNode,
  llm: LlmNode,
  tool: ToolNode,
  output: OutputNode,
  updateState: UpdateStateNode,
  waypoint: WaypointNode,
}

// Node type definitions for toolbar
const NODE_TYPES = [
  { type: 'input', label: 'Input', icon: Edit3, color: '#E9D5FF', darkColor: '#3B2667' },
  { type: 'llm', label: 'LLM', icon: Brain, color: '#6B46C1', darkColor: '#6B46C1' },
  { type: 'tool', label: 'Tool', icon: Wrench, color: '#805AD5', darkColor: '#805AD5' },
  { type: 'updateState', label: 'State', icon: Settings, color: '#4A5568', darkColor: '#4A5568' },
  { type: 'output', label: 'Output', icon: MessageSquare, color: '#9F7AEA', darkColor: '#9F7AEA' },
]

function FlowCanvasInner({ 
  nodes: propNodes, 
  edges: propEdges, 
  isRunning, 
  theme, 
  onNodeSelect, 
  onNodeDoubleClick,
  selectedNodeId, 
  onAddNode,
  onConnect: onConnectCallback,
  onEdgeRemove,
  onLayoutChange,
  onOpenAIChat,
  onNodeDelete,
  runningNodeId
}) {
  const [nodes, setNodes, onNodesChangeBase] = useNodesState([])
  const [edges, setEdges, handleEdgesChange] = useEdgesState([])
  
  // Wrap node change handler to detect deletions and notify parent
  const handleNodesChange = useCallback((changes) => {
    // Check for node removals
    const removals = changes.filter(change => change.type === 'remove')
    if (removals.length > 0 && onNodeDelete) {
      // Get the IDs of removed nodes that are not waypoints/start/end
      const removedIds = removals
        .map(r => r.id)
        .filter(id => id !== 'START' && id !== 'END' && !id.startsWith('waypoint'))
      
      if (removedIds.length > 0) {
        // Notify parent of deletions BEFORE applying the change
        removedIds.forEach(id => onNodeDelete(id))
      }
    }
    
    // Apply the change locally
    onNodesChangeBase(changes)
  }, [onNodesChangeBase, onNodeDelete])
  
  // Track local modifications (like waypoint deletion) that shouldn't be overwritten by prop sync
  const localModificationRef = useRef(false)
  
  // Track edges that have been "split" by waypoints - these should be excluded from prop sync
  // Format: Set of edge keys like "source->target"
  const splitEdgesRef = useRef(new Set())
  
  // Check if canvas is empty (only START and END nodes)
  const isEmptyCanvas = propNodes.filter(n => n.type !== 'start' && n.type !== 'end').length === 0

  // Track multi-selection for AI assist
  const [selectedNodeIds, setSelectedNodeIds] = useState([])
  
  // Filter to get only "real" nodes (not START, END, waypoint)
  const selectedRealNodes = useMemo(() => {
    return selectedNodeIds.filter(id => {
      const node = nodes.find(n => n.id === id)
      return node && !['start', 'end', 'waypoint'].includes(node.type)
    })
  }, [selectedNodeIds, nodes])
  
  const hasMultiSelection = selectedRealNodes.length >= 2

  // Sync nodes from props - update selection state but preserve waypoint nodes
  // UNLESS propNodes already contains waypoints (from YAML)
  useEffect(() => {
    // Skip sync if we just made a local modification (like waypoint deletion)
    if (localModificationRef.current) {
      return
    }
    
    if (propNodes && propNodes.length > 0) {
      // Check if propNodes already has waypoints (from YAML loading)
      const propsHaveWaypoints = propNodes.some(n => n.type === 'waypoint')
      
      setNodes((currentNodes) => {
        // Add selected state to prop nodes
        const nodesWithSelection = propNodes.map(node => {
          // Check if we have a current position for this node to preserve dragged position
          // But only if the node ID matches (to avoid using position from different agent's node)
          const currentNode = currentNodes.find(n => n.id === node.id)
          const position = currentNode ? currentNode.position : node.position
          
          return {
            ...node,
            position,
            selected: node.id === selectedNodeId,
            data: {
              ...node.data,
              isSelected: node.id === selectedNodeId,
              isActive: node.id === runningNodeId
            }
          }
        })
        
        // Only preserve local waypoints if propNodes doesn't already have waypoints
        if (propsHaveWaypoints) {
          // propNodes already has waypoints from YAML - use them directly
          return nodesWithSelection
        }
        
        // Keep any runtime waypoint nodes (only when propNodes doesn't have waypoints)
        const waypointNodes = currentNodes.filter(n => n.type === 'waypoint')
        return [...nodesWithSelection, ...waypointNodes]
      })
    }
  }, [propNodes, selectedNodeId, runningNodeId])

  // Sync edges from props - but preserve runtime waypoint edges
  // UNLESS propEdges already contains waypoint edges (from YAML)
  useEffect(() => {
    // Skip sync if we just made a local modification (like waypoint deletion)
    if (localModificationRef.current) {
      // Clear the flag after skipping once - this allows future prop changes to sync
      localModificationRef.current = false
      return
    }
    
    if (propEdges) {
      // Check if propEdges already has waypoint edges (from YAML loading)
      const propsHaveWaypointEdges = propEdges.some(e => 
        e.source.startsWith('waypoint-') || e.target.startsWith('waypoint-')
      )
      
      if (propsHaveWaypointEdges) {
        // propEdges already has waypoint edges from YAML - clear split tracking and use them directly
        splitEdgesRef.current.clear()
        setEdges(propEdges)
        return
      }
      
      // Local waypoint preservation logic (only when propEdges doesn't have waypoints)
      setEdges((currentEdges) => {
        // Keep any runtime waypoint edges (edges connected to waypoint nodes)
        const waypointEdges = currentEdges.filter(e => 
          e.source.startsWith('waypoint-') || e.target.startsWith('waypoint-')
        )
        
        // Filter out propEdges that:
        // 1. Have been split by waypoints (tracked in splitEdgesRef)
        // 2. Are currently split by active waypoints
        const filteredPropEdges = propEdges.filter(e => {
          const edgeKey = `${e.source}->${e.target}`
          
          // Check if this edge is in our split tracking
          if (splitEdgesRef.current.has(edgeKey)) {
            return false
          }
          
          // Check if this edge is actively split by waypoints
          const hasWaypointOnSource = waypointEdges.some(we => we.source === e.source && we.target.startsWith('waypoint-'))
          const hasWaypointOnTarget = waypointEdges.some(we => we.target === e.target && we.source.startsWith('waypoint-'))
          if (hasWaypointOnSource && hasWaypointOnTarget) {
            return false
          }
          
          return true
        })
        
        // Preserve any local edges (non-waypoint edges that aren't in propEdges)
        const localNonWaypointEdges = currentEdges.filter(ce => 
          !ce.source.startsWith('waypoint-') && 
          !ce.target.startsWith('waypoint-') &&
          !propEdges.some(pe => pe.source === ce.source && pe.target === ce.target)
        )
        
        return [...filteredPropEdges, ...waypointEdges, ...localNonWaypointEdges]
      })
    }
  }, [propEdges])

  // Notify parent of layout changes (for saving)
  useEffect(() => {
    if (onLayoutChange && nodes.length > 0) {
      onLayoutChange(nodes, edges)
    }
  }, [nodes, edges, onLayoutChange])

  // Get React Flow instance for coordinate conversion and viewport control
  const { screenToFlowPosition, setViewport, getViewport } = useReactFlow()
  const hasCentered = useRef(false)
  const containerRef = useRef(null)
  
  // Center viewport horizontally on START node when flow first loads (view only, no flash)
  useEffect(() => {
    if (nodes.length > 0 && !hasCentered.current) {
      const startNode = nodes.find(n => n.id === 'START')
      if (startNode && startNode.position) {
        // Get container width to calculate center offset
        const containerWidth = containerRef.current?.offsetWidth || 800
        // Calculate X to center START node horizontally
        const centerX = -(startNode.position.x - containerWidth / 2 + 60)
        // Set viewport: center horizontally, keep top visible
        setViewport({ x: centerX, y: 30, zoom: 1 }, { duration: 0 })
        hasCentered.current = true
      }
    }
  }, [nodes, setViewport])
  
  // Reset centering flag when agent changes (nodes cleared)
  useEffect(() => {
    if (nodes.length === 0) {
      hasCentered.current = false
    }
  }, [nodes.length])

  // Handle double-click on edge to add waypoint
  const onEdgeDoubleClick = useCallback((event, edge) => {
    event.stopPropagation()
    event.preventDefault()

    // Track this edge as "split" - it shouldn't be re-added by prop sync
    const edgeKey = `${edge.source}->${edge.target}`
    splitEdgesRef.current.add(edgeKey)
    
    // Mark as local modification
    localModificationRef.current = true

    // Convert screen position to flow coordinates
    const position = screenToFlowPosition({
      x: event.clientX,
      y: event.clientY,
    })

    // Create unique ID for the waypoint with random suffix
    const uniqueSuffix = `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
    const waypointId = `waypoint-${uniqueSuffix}`

    // Create the new waypoint node
    const waypointNode = {
      id: waypointId,
      type: 'waypoint',
      position: position,
      data: { label: '' },
      draggable: true,
    }

    // We need to read current node positions to determine handle positions
    // Do this synchronously by reading from the current nodes state
    setNodes((currentNodes) => {
      const sourceNode = currentNodes.find(n => n.id === edge.source)
      const targetNode = currentNodes.find(n => n.id === edge.target)
      const sourceY = sourceNode?.position?.y || 0
      const targetY = targetNode?.position?.y || 0
      const waypointY = position.y
      
      // Check if source or target are waypoints (for chains)
      const sourceIsWaypoint = sourceNode?.type === 'waypoint'
      const targetIsWaypoint = targetNode?.type === 'waypoint'
      
      // Determine handles for edge1 (source -> new waypoint)
      // New waypoint's target handle
      let edge1SourceHandle = null
      let edge1TargetHandle = 'top-target' // New waypoint's top-target
      
      if (sourceIsWaypoint) {
        // Source is also a waypoint - use waypoint source handles
        edge1SourceHandle = sourceY > waypointY ? 'top-source' : 'bottom-source'
      } else if (sourceY > waypointY) {
        // Source is below waypoint - back-edge going UP
        edge1SourceHandle = 'top-source' // Regular node's top-source
      }
      
      if (sourceY > waypointY) {
        edge1TargetHandle = 'bottom-target'
      }
      
      // Determine handles for edge2 (new waypoint -> target)
      let edge2SourceHandle = 'bottom-source' // New waypoint's bottom-source
      let edge2TargetHandle = null // Target's handle
      
      if (waypointY > targetY) {
        // New waypoint is BELOW target - back-edge going UP
        edge2SourceHandle = 'top-source'
        
        if (targetIsWaypoint) {
          // Target is a waypoint - use waypoint target handle
          edge2TargetHandle = 'bottom-target'
        } else {
          // Target is regular node - use left handle
          edge2TargetHandle = 'left'
        }
      } else {
        // Normal flow - new waypoint above target
        if (targetIsWaypoint) {
          edge2TargetHandle = 'top-target'
        }
        // Regular nodes use null for top-down flow
      }
      
      // Create edges with correct handles (computed from actual positions)
      const newEdge1 = {
        ...edge,
        id: `e-${edge.source}-${waypointId}-${uniqueSuffix}`,
        source: edge.source,
        target: waypointId,
        sourceHandle: edge1SourceHandle,
        targetHandle: edge1TargetHandle,
      }

      const newEdge2 = {
        ...edge,
        id: `e-${waypointId}-${edge.target}-${uniqueSuffix}`,
        source: waypointId,
        target: edge.target,
        sourceHandle: edge2SourceHandle,
        targetHandle: edge2TargetHandle,
      }

      // Update edges from within setNodes to avoid timing issues
      setEdges((currentEdges) => {
        // Check if we already added these edges (React Strict Mode can cause double calls)
        const alreadyHasEdge1 = currentEdges.some(e => e.id === newEdge1.id)
        if (alreadyHasEdge1) {
          return currentEdges // Already added, don't duplicate
        }
        
        return currentEdges
          .filter((e) => e.id !== edge.id)
          .concat([newEdge1, newEdge2])
      })

      return [...currentNodes, waypointNode]
    })
  }, [screenToFlowPosition, setNodes, setEdges])

  // Handle new connection (drag from one node to another)
  const onConnect = useCallback((params) => {
    if (onConnectCallback) {
      // Call parent to update YAML, which will re-render with new edge
      onConnectCallback(params.source, params.target)
    }
  }, [onConnectCallback])

  // Handle edge deletion
  const onEdgesDelete = useCallback((deletedEdges) => {
    if (onEdgeRemove) {
      deletedEdges.forEach(edge => {
        onEdgeRemove(edge.source, edge.target)
      })
    }
  }, [onEdgeRemove])

  const onNodeClick = useCallback((event, node) => {
    // Skip parent's single-select handler when shift is held (multi-selection)
    if (event.shiftKey) {
      return // Let React Flow handle multi-selection
    }
    if (onNodeSelect) {
      onNodeSelect(node.id)
    }
  }, [onNodeSelect])

  const handleNodeDoubleClick = useCallback((event, node) => {
    // If double-clicking a waypoint node, remove it and rejoin the edges
    if (node.type === 'waypoint') {
      event.stopPropagation()
      
      // Mark that we're making a local modification - prevents sync from overwriting
      localModificationRef.current = true
      
      // Find edges connected to this waypoint and rejoin them
      setEdges((currentEdges) => {
        // Find ANY edge going INTO or OUT OF this waypoint
        const incomingEdge = currentEdges.find(e => e.target === node.id)
        const outgoingEdge = currentEdges.find(e => e.source === node.id)
        
        if (incomingEdge && outgoingEdge) {
          // Remove this edge from split tracking since we're rejoining it
          const edgeKey = `${incomingEdge.source}->${outgoingEdge.target}`
          splitEdgesRef.current.delete(edgeKey)
          
          // Create a new edge connecting the original source to original target
          const rejoinedEdge = {
            id: `e-${incomingEdge.source}-${outgoingEdge.target}-${Date.now()}`,
            source: incomingEdge.source,
            target: outgoingEdge.target,
            sourceHandle: null,
            targetHandle: null,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'smoothstep',
            label: incomingEdge.label || '',
            labelStyle: incomingEdge.labelStyle,
            labelBgStyle: incomingEdge.labelBgStyle,
            labelBgPadding: incomingEdge.labelBgPadding,
          }
          
          // Remove ALL edges connected to this waypoint (handles duplicates)
          // Filter by source/target rather than just ID
          const newEdges = currentEdges
            .filter(e => e.target !== node.id && e.source !== node.id)
            .concat([rejoinedEdge])
          
          return newEdges
        }
        
        // Fallback: just remove any edges connected to this waypoint
        return currentEdges.filter(e => e.target !== node.id && e.source !== node.id)
      })
      
      // Remove the waypoint node
      setNodes((currentNodes) => currentNodes.filter(n => n.id !== node.id))
      
      return
    }
    
    // For regular nodes, call the parent handler
    if (onNodeDoubleClick) {
      onNodeDoubleClick(node.id)
    }
  }, [onNodeDoubleClick, setNodes, setEdges])

  // Handle selection changes from React Flow
  const onSelectionChange = useCallback(({ nodes: selectedNodes }) => {
    const ids = selectedNodes?.map(n => n.id) || []
    setSelectedNodeIds(ids)
  }, [])

  const onPaneClick = useCallback(() => {
    // Clicking on empty space deselects
    if (onNodeSelect) {
      onNodeSelect(null)
    }
    // Also clear multi-selection
    setSelectedNodeIds([])
  }, [onNodeSelect])

  const defaultEdgeOptions = useMemo(() => ({
    style: { stroke: '#805AD5', strokeWidth: 2 },
    type: 'smoothstep',
  }), [])

  return (
    <div ref={containerRef} className="w-full h-full" style={{ background: theme === 'dark' ? '#0d121f' : '#F7F5FB' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={handleNodesChange}
        onEdgesChange={handleEdgesChange}
        onConnect={onConnect}
        onEdgesDelete={onEdgesDelete}
        onNodeClick={onNodeClick}
        onNodeDoubleClick={handleNodeDoubleClick}
        onEdgeDoubleClick={onEdgeDoubleClick}
        onPaneClick={onPaneClick}
        onSelectionChange={onSelectionChange}
        selectionOnDrag={false}
        selectNodesOnDrag={false}
        panOnDrag={true}
        panOnScroll={true}
        selectionKeyCode="Shift"
        multiSelectionKeyCode="Shift"
        nodeTypes={nodeTypes}
        defaultEdgeOptions={defaultEdgeOptions}
        defaultViewport={{ x: 50, y: 30, zoom: 1 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={!isRunning}
        nodesConnectable={!isRunning}
        elementsSelectable={!isRunning}
        deleteKeyCode={['Backspace', 'Delete']}
        colorMode={theme}
        minZoom={0.1}
        maxZoom={2}
      >
        <Background color={theme === 'dark' ? '#374151' : '#E2E8F0'} gap={20} />
        <Controls className="rounded-lg shadow-md" style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border-color)' }} />
        <MiniMap
          nodeColor={(node) => {
            switch (node.type) {
              case 'start': return '#38A169'
              case 'end': return '#E53E3E'
              case 'input': return theme === 'dark' ? '#3B2667' : '#DDD6FE'
              case 'llm': return '#6B46C1'
              case 'tool': return '#805AD5'
              case 'output': return '#9F7AEA'
              case 'updateState': return '#4A5568'
              default: return '#CBD5E0'
            }
          }}
          className="rounded-lg shadow-md"
          style={{ background: 'var(--bg-secondary)' }}
          maskColor={theme === 'dark' ? 'rgba(0, 0, 0, 0.5)' : 'rgba(255, 255, 255, 0.5)'}
        />
        
        {/* Node Type Toolbar */}
        {!isRunning && (
          <Panel position="top-right" className="m-2">
            <div 
              className="flex flex-col gap-2 p-2 rounded-lg shadow-lg"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
            >
              <div className="text-xs text-center mb-1" style={{ color: 'var(--text-muted)' }}>
                Add Node
              </div>
              {NODE_TYPES.map(({ type, label, icon: Icon, color, darkColor }) => (
                <button
                  key={type}
                  onClick={() => onAddNode && onAddNode(type)}
                  className="flex flex-col items-center gap-1 p-2 rounded-lg transition-all hover:scale-110"
                  style={{ 
                    background: theme === 'dark' ? darkColor : color,
                    color: type === 'input' && theme !== 'dark' ? '#1F2937' : 'white',
                    minWidth: '48px'
                  }}
                  title={`Add ${label} node (standalone)`}
                >
                  <Icon size={18} />
                  <span className="text-[10px] font-medium">{label}</span>
                </button>
              ))}
            </div>
          </Panel>
        )}
        
        {/* Empty Canvas State - Create with AI */}
        {!isRunning && isEmptyCanvas && onOpenAIChat && (
          <div 
            className="absolute inset-0 flex items-center justify-center pointer-events-none"
            style={{ zIndex: 10 }}
          >
            <div 
              className="flex flex-col items-center gap-4 p-6 rounded-2xl shadow-xl pointer-events-auto"
              style={{ 
                background: 'var(--bg-secondary)', 
                border: '2px solid var(--border-color)',
                minWidth: '320px'
              }}
            >
              <div className="text-center">
                <h3 className="text-lg font-semibold mb-1" style={{ color: 'var(--text-primary)' }}>
                  Start Building Your Flow
                </h3>
                <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                  Describe what you want or drag nodes from the toolbar
                </p>
              </div>
              
              <button
                onClick={onOpenAIChat}
                className="flex items-center gap-2 px-6 py-3 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 text-white font-medium rounded-xl shadow-lg transition-all hover:scale-105"
              >
                <Sparkles className="w-5 h-5" />
                Create with AI
              </button>
              
              <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
                or use the node toolbar on the right →
              </div>
            </div>
          </div>
        )}
        
        {/* Instructions */}
        {!isRunning && (
          <Panel position="bottom-left" className="m-2">
            <div 
              className="text-xs px-3 py-2 rounded-lg"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', color: 'var(--text-muted)' }}
            >
              Drag from node handles to connect • Select edge + Delete to remove • Shift+drag to multi-select
            </div>
          </Panel>
        )}
        
        {/* Multi-Selection AI Assist Button */}
        {!isRunning && hasMultiSelection && onOpenAIChat && (
          <Panel position="bottom-center" className="mb-4">
            <button
              onClick={() => onOpenAIChat({ context: 'multi_node', nodeIds: selectedRealNodes })}
              className="flex items-center gap-2 px-4 py-2.5 rounded-lg shadow-lg text-white font-medium transition-all hover:scale-105"
              style={{ 
                background: 'linear-gradient(135deg, #6B46C1 0%, #4F46E5 100%)',
                border: '1px solid rgba(255,255,255,0.2)'
              }}
            >
              <Sparkles size={18} />
              AI Assist ({selectedRealNodes.length} nodes)
            </button>
          </Panel>
        )}
      </ReactFlow>
    </div>
  )
}

// Wrapper component that provides a key to force re-mount when flow structure changes dramatically
export default function FlowCanvas({ nodes, edges, isRunning, theme, onNodeSelect, onNodeDoubleClick, selectedNodeId, runningNodeId, onAddNode, onConnect, onEdgeRemove, onLayoutChange, onOpenAIChat, onNodeDelete }) {
  // Generate a key based on node IDs to force re-mount when nodes change completely
  const flowKey = useMemo(() => {
    if (!nodes || nodes.length === 0) return 'empty'
    return nodes.map(n => n.id).sort().join(',')
  }, [nodes])

  return (
    <FlowCanvasInner
      key={flowKey}
      nodes={nodes}
      edges={edges}
      isRunning={isRunning}
      theme={theme}
      onNodeSelect={onNodeSelect}
      onNodeDoubleClick={onNodeDoubleClick}
      selectedNodeId={selectedNodeId}
      onAddNode={onAddNode}
      onConnect={onConnect}
      onEdgeRemove={onEdgeRemove}
      onLayoutChange={onLayoutChange}
      onOpenAIChat={onOpenAIChat}
      onNodeDelete={onNodeDelete}
      runningNodeId={runningNodeId}
    />
  )
}

