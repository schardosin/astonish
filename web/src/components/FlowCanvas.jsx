import { useCallback, useMemo, useEffect } from 'react'
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
  onOpenAIChat
}) {
  const [nodes, setNodes, handleNodesChange] = useNodesState([])
  const [edges, setEdges, handleEdgesChange] = useEdgesState([])
  
  // Check if canvas is empty (only START and END nodes)
  const isEmptyCanvas = propNodes.filter(n => n.type !== 'start' && n.type !== 'end').length === 0

  // Sync nodes from props - update selection state but preserve waypoint nodes
  // UNLESS propNodes already contains waypoints (from YAML)
  useEffect(() => {
    if (propNodes && propNodes.length > 0) {
      // Check if propNodes already has waypoints (from YAML loading)
      const propsHaveWaypoints = propNodes.some(n => n.type === 'waypoint')
      
      setNodes((currentNodes) => {
        // Add selected state to prop nodes
        const nodesWithSelection = propNodes.map(node => ({
          ...node,
          selected: node.id === selectedNodeId,
          data: {
            ...node.data,
            isSelected: node.id === selectedNodeId
          }
        }))
        
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
  }, [propNodes, selectedNodeId])

  // Sync edges from props - but preserve runtime waypoint edges
  // UNLESS propEdges already contains waypoint edges (from YAML)
  useEffect(() => {
    if (propEdges) {
      // Check if propEdges already has waypoint edges (from YAML loading)
      const propsHaveWaypointEdges = propEdges.some(e => 
        e.source.startsWith('waypoint-') || e.target.startsWith('waypoint-')
      )
      
      if (propsHaveWaypointEdges) {
        // propEdges already has waypoint edges from YAML - use them directly
        setEdges(propEdges)
        return
      }
      
      // Local waypoint preservation logic (only when propEdges doesn't have waypoints)
      setEdges((currentEdges) => {
        // Keep any runtime waypoint edges (edges connected to waypoint nodes)
        const waypointEdges = currentEdges.filter(e => 
          e.source.startsWith('waypoint-') || e.target.startsWith('waypoint-')
        )
        
        if (waypointEdges.length === 0) {
          return propEdges
        }
        
        // Filter out propEdges that have been split by waypoints
        const filteredPropEdges = propEdges.filter(e => {
          const hasWaypointOnSource = waypointEdges.some(we => we.source === e.source && we.target.startsWith('waypoint-'))
          const hasWaypointOnTarget = waypointEdges.some(we => we.target === e.target && we.source.startsWith('waypoint-'))
          return !(hasWaypointOnSource && hasWaypointOnTarget)
        })
        
        return [...filteredPropEdges, ...waypointEdges]
      })
    }
  }, [propEdges])

  // Notify parent of layout changes (for saving)
  useEffect(() => {
    if (onLayoutChange && nodes.length > 0) {
      onLayoutChange(nodes, edges)
    }
  }, [nodes, edges, onLayoutChange])

  // Get React Flow instance for coordinate conversion
  const { screenToFlowPosition } = useReactFlow()

  // Handle double-click on edge to add waypoint
  const onEdgeDoubleClick = useCallback((event, edge) => {
    event.stopPropagation()
    event.preventDefault()

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

    // Find source and target node positions to determine handle connections
    setNodes((currentNodes) => {
      const sourceNode = currentNodes.find(n => n.id === edge.source)
      const targetNode = currentNodes.find(n => n.id === edge.target)
      
      // Determine handles based on relative positions
      // For edge FROM source TO waypoint:
      // - If source is above waypoint, source exits from bottom, waypoint receives at top
      // - If source is below waypoint, source exits from top/left, waypoint receives at bottom
      const sourceY = sourceNode?.position?.y || 0
      const targetY = targetNode?.position?.y || 0
      const waypointY = position.y
      
      // Source -> Waypoint edge handles
      let edge1SourceHandle = null // Default: bottom (normal node)
      let edge1TargetHandle = null // Default: top (normal node) or specific for waypoint
      
      if (sourceY > waypointY) {
        // Source is BELOW waypoint - this is a back-edge going UP
        edge1SourceHandle = 'top-source'
        edge1TargetHandle = 'bottom-target'
      } else {
        // Source is ABOVE waypoint - normal flow (top to bottom)
        // Explicitly set waypoint handle for proper routing
        edge1TargetHandle = 'top-target'
      }
      
      // Waypoint -> Target edge handles
      let edge2SourceHandle = null // Default: bottom (normal node) or specific for waypoint
      let edge2TargetHandle = null // Default: top (normal node)
      
      if (waypointY > targetY) {
        // Waypoint is BELOW target - back-edge going UP
        edge2SourceHandle = 'top-source'
        edge2TargetHandle = 'left'
      } else {
        // Waypoint is ABOVE target - normal flow (top to bottom)
        // Explicitly set waypoint handle for proper routing
        edge2SourceHandle = 'bottom-source'
      }
      
      // Create two new edges with proper handles
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

      // Update edges
      setEdges((eds) => eds.filter((e) => e.id !== edge.id).concat([newEdge1, newEdge2]))
      
      // Return updated nodes with the new waypoint
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
    if (onNodeSelect) {
      onNodeSelect(node.id)
    }
  }, [onNodeSelect])

  const handleNodeDoubleClick = useCallback((event, node) => {
    // If double-clicking a waypoint node, remove it and rejoin the edges
    if (node.type === 'waypoint') {
      event.stopPropagation()
      
      // Find edges connected to this waypoint
      setEdges((currentEdges) => {
        const incomingEdge = currentEdges.find(e => e.target === node.id)
        const outgoingEdge = currentEdges.find(e => e.source === node.id)
        
        if (incomingEdge && outgoingEdge) {
          // Create a new edge connecting the original source to original target
          // Reset handles to null for direct node-to-node connection
          const rejoinedEdge = {
            id: `e-${incomingEdge.source}-${outgoingEdge.target}-${Date.now()}`,
            source: incomingEdge.source,
            target: outgoingEdge.target,
            sourceHandle: null, // Reset - no waypoint handles needed
            targetHandle: null, // Reset - no waypoint handles needed
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'smoothstep',
            // Preserve label if it was on the incoming edge
            label: incomingEdge.label || '',
            labelStyle: incomingEdge.labelStyle,
            labelBgStyle: incomingEdge.labelBgStyle,
            labelBgPadding: incomingEdge.labelBgPadding,
          }
          
          // Remove the waypoint edges and add the rejoined edge
          return currentEdges
            .filter(e => e.id !== incomingEdge.id && e.id !== outgoingEdge.id)
            .concat([rejoinedEdge])
        }
        
        return currentEdges
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

  const onPaneClick = useCallback(() => {
    // Clicking on empty space deselects
    if (onNodeSelect) {
      onNodeSelect(null)
    }
  }, [onNodeSelect])

  const defaultEdgeOptions = useMemo(() => ({
    style: { stroke: '#805AD5', strokeWidth: 2 },
    type: 'smoothstep',
  }), [])

  return (
    <div className="w-full h-full" style={{ background: theme === 'dark' ? '#0d121f' : '#F7F5FB' }}>
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
          <Panel position="top-center" className="mt-16">
            <div 
              className="flex flex-col items-center gap-4 p-6 rounded-2xl shadow-xl"
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
          </Panel>
        )}
        
        {/* Instructions */}
        {!isRunning && (
          <Panel position="bottom-left" className="m-2">
            <div 
              className="text-xs px-3 py-2 rounded-lg"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', color: 'var(--text-muted)' }}
            >
              Drag from node handles to connect • Select edge + Delete to remove
            </div>
          </Panel>
        )}
      </ReactFlow>
    </div>
  )
}

// Wrapper component that provides a key to force re-mount when flow structure changes dramatically
export default function FlowCanvas({ nodes, edges, isRunning, theme, onNodeSelect, onNodeDoubleClick, selectedNodeId, onAddNode, onConnect, onEdgeRemove, onLayoutChange, onOpenAIChat }) {
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
    />
  )
}

