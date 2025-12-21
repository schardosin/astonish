import { useCallback, useMemo, useEffect, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  useReactFlow,
  useNodes,
  useEdges,
  Panel,
  ReactFlowProvider,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Edit3, Brain, Wrench, Settings, MessageSquare, Sparkles, LayoutDashboard, Maximize } from 'lucide-react'

import StartNode from './nodes/StartNode'
import EndNode from './nodes/EndNode'
import InputNode from './nodes/InputNode'
import LlmNode from './nodes/LlmNode'
import ToolNode from './nodes/ToolNode'
import OutputNode from './nodes/OutputNode'
import UpdateStateNode from './nodes/UpdateStateNode'
import EditableEdge from './edges/EditableEdge'
import NodeTypePopover from './NodeTypePopover'


const nodeTypes = {
  start: StartNode,
  end: EndNode,
  input: InputNode,
  llm: LlmNode,
  tool: ToolNode,
  output: OutputNode,
  updateState: UpdateStateNode,
}

// Custom edge types
const edgeTypes = {
  editable: EditableEdge,
}

// Node type definitions for toolbar - matches Overflow node styling
const NODE_TYPES = [
  { type: 'input', label: 'Input', icon: Edit3, iconColor: '#a78bfa' },
  { type: 'llm', label: 'LLM', icon: Brain, iconColor: '#8b5cf6' },
  { type: 'tool', label: 'Tool', icon: Wrench, iconColor: '#7c3aed' },
  { type: 'updateState', label: 'State', icon: Settings, iconColor: '#8b5cf6' },
  { type: 'output', label: 'Output', icon: MessageSquare, iconColor: '#9f7aea' },
]

function FlowCanvasInner({ 
  nodes: propNodes, 
  edges: propEdges, 
  isRunning, 
  readOnly,
  theme, 
  onNodeSelect, 
  onNodeDoubleClick,
  onEdgeSelect,
  selectedNodeId, 
  onAddNode,
  onConnect: onConnectCallback,
  onEdgeRemove,
  onLayoutChange,
  onLayoutSave,
  onOpenAIChat,
  onNodeDelete,
  onAutoLayout,
  runningNodeId,
  onCreateConnectedNode  // Callback for quick node creation from + button
}) {
  const { getNodes, getEdges } = useReactFlow()
  const [nodes, setNodes, onNodesChangeBase] = useNodesState([])
  const [edges, setEdges, handleEdgesChange] = useEdgesState([])
  
  // Track edge dragging to prevent sync conflicts
  const isDraggingEdgeRef = useRef(false)

  // Listen for edge drag events
  useEffect(() => {
    const onDragStart = () => { isDraggingEdgeRef.current = true }
    // Add small delay to drag stop to ensure pending prop updates don't overwrite immediately
    const onDragStop = () => { 
      setTimeout(() => { isDraggingEdgeRef.current = false }, 500)
      // Save immediately to ensure changes persist
      if (onLayoutSave) {
        onLayoutSave(getNodes(), getEdges())
      }
    }
    
    window.addEventListener('astonish:edge-drag-start', onDragStart)
    window.addEventListener('astonish:edge-drag-stop', onDragStop)
    
    return () => {
      window.removeEventListener('astonish:edge-drag-start', onDragStart)
      window.removeEventListener('astonish:edge-drag-stop', onDragStop)
    }
  }, [onLayoutSave, getNodes, getEdges])

  // State for quick add node popover
  const [addNodePopover, setAddNodePopover] = useState(null) // { sourceId, position: {x, y} }

  // Listen for add-node-click events from nodes
  useEffect(() => {
    const handleAddNodeClick = (e) => {
      if (!readOnly) {
        setAddNodePopover(e.detail)
      }
    }
    
    window.addEventListener('astonish:add-node-click', handleAddNodeClick)
    return () => window.removeEventListener('astonish:add-node-click', handleAddNodeClick)
  }, [readOnly])

  // Handle node type selection from popover
  const handleNodeTypeSelect = useCallback((nodeType) => {
    if (addNodePopover && onCreateConnectedNode) {
      // Find source node to get its position
      const sourceNode = nodes.find(n => n.id === addNodePopover.sourceId)
      const position = sourceNode 
        ? { x: sourceNode.position.x, y: sourceNode.position.y + 150 }
        : { x: 300, y: 300 }
      
      onCreateConnectedNode(addNodePopover.sourceId, nodeType, position)
    }
    setAddNodePopover(null)
  }, [addNodePopover, nodes, onCreateConnectedNode])

  
  // Wrap node change handler to detect deletions and notify parent
  const handleNodesChange = useCallback((changes) => {
    // Check for node removals
    const removals = changes.filter(change => change.type === 'remove')
    if (removals.length > 0 && onNodeDelete) {
      // Get the IDs of removed nodes that are not start/end
      const removedIds = removals
        .map(r => r.id)
        .filter(id => id !== 'START' && id !== 'END')
      
      if (removedIds.length > 0) {
        // Notify parent of deletions BEFORE applying the change
        removedIds.forEach(id => onNodeDelete(id))
      }
    }
    
    // Apply the change locally
    onNodesChangeBase(changes)
  }, [onNodesChangeBase, onNodeDelete])
  

  
  // Check if canvas is empty (only START and END nodes)
  const isEmptyCanvas = propNodes.filter(n => n.type !== 'start' && n.type !== 'end').length === 0

  // Track multi-selection for AI assist
  const [selectedNodeIds, setSelectedNodeIds] = useState([])
  
  // Filter to get only "real" nodes (not START, END)
  const selectedRealNodes = useMemo(() => {
    return selectedNodeIds.filter(id => {
      const node = nodes.find(n => n.id === id)
      return node && !['start', 'end'].includes(node.type)
    })
  }, [selectedNodeIds, nodes])
  
  const hasMultiSelection = selectedRealNodes.length >= 2

  // Sync nodes from props - update selection state
  // Sync nodes from props - update selection state
  useEffect(() => {
    if (propNodes && propNodes.length > 0) {
      // Build set of nodes that have outgoing connections
      const nodesWithOutgoing = new Set()
      if (propEdges) {
        propEdges.forEach(edge => nodesWithOutgoing.add(edge.source))
      }
      
      setNodes((currentNodes) => {
        // Add selected state to prop nodes
        return propNodes.map(node => {
          return {
            ...node,
            selected: node.id === selectedNodeId,
            data: {
              ...node.data,
              isSelected: node.id === selectedNodeId,
              isActive: node.id === runningNodeId,
              hasOutgoingConnection: nodesWithOutgoing.has(node.id)
            }
          }
        })
      })
    }
  }, [propNodes, propEdges, selectedNodeId, runningNodeId])

  // Sync edges from props
  // Sync edges from props
  useEffect(() => {
    if (propEdges && !isDraggingEdgeRef.current) {
      setEdges(propEdges)
    }
  }, [propEdges, setEdges])

  // Notify parent of layout changes (for saving)
  // Get reactive state from store to ensure we capture updates from custom edges/nodes
  // that modify the store directly (like EditableEdge)
  const storeNodes = useNodes()
  const storeEdges = useEdges()

  // Notify parent of layout changes (for saving)
  useEffect(() => {
    if (onLayoutChange && storeNodes.length > 0) {
      onLayoutChange(storeNodes, storeEdges)
    }
  }, [storeNodes, storeEdges, onLayoutChange])

  // Debounced save for edge changes (since EditableEdge updates do not trigger onNodeDragStop)
  useEffect(() => {
    if (!onLayoutSave || storeEdges.length === 0) return

    const timer = setTimeout(() => {
      onLayoutSave(storeNodes, storeEdges)
    }, 1000) // Debounce 1s to avoid excessive YAML (re)generation during drag

    return () => clearTimeout(timer)
  }, [storeEdges, storeNodes, onLayoutSave])

  // Listener for immediate save on edge drag stop (custom event from EditableEdge)
  useEffect(() => {
    if (!onLayoutSave) return

    const handleEdgeDragStop = () => {
      // Small timeout to ensure store update has propagated
      setTimeout(() => {
        onLayoutSave(storeNodes, storeEdges)
      }, 50) 
    }

    window.addEventListener('astonish:edge-drag-stop', handleEdgeDragStop)
    return () => window.removeEventListener('astonish:edge-drag-stop', handleEdgeDragStop)
  }, [storeNodes, storeEdges, onLayoutSave])

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
        setViewport({ x: centerX, y: 30, zoom: 0.9 }, { duration: 0 })
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

  // Recenter flow horizontally when isRunning changes (container size changes)
  // NOTE: We use a ref to access nodes to avoid running this when nodes change
  const nodesRef = useRef(nodes)
  nodesRef.current = nodes
  
  useEffect(() => {
    // Small delay to let the container resize complete
    const timer = setTimeout(() => {
      const viewport = getViewport()
      const containerWidth = containerRef.current?.offsetWidth || 800
      const currentNodes = nodesRef.current
      const startNode = currentNodes.find(n => n.id === 'START')
      if (startNode && startNode.position) {
        // Calculate X to center START node horizontally, keep same Y and zoom
        const centerX = -(startNode.position.x - containerWidth / 2 + 60) * viewport.zoom
        setViewport({ x: centerX, y: viewport.y, zoom: viewport.zoom }, { duration: 300 })
      }
    }, 100)
    return () => clearTimeout(timer)
  }, [isRunning, getViewport, setViewport]) // Removed nodes from dependencies






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
    // For regular nodes, call the parent handler
    if (onNodeDoubleClick) {
      onNodeDoubleClick(node.id)
    }
  }, [onNodeDoubleClick])

  // Handle edge double-click for editing conditions
  const handleEdgeDoubleClick = useCallback((event, edge) => {
    if (onEdgeSelect) {
      onEdgeSelect(edge)
    }
  }, [onEdgeSelect])

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
    // Close context menu if open
    setContextMenu(null)
  }, [onNodeSelect])

  // Context menu state
  const [contextMenu, setContextMenu] = useState(null)

  // Handle right-click on pane for context menu
  const onPaneContextMenu = useCallback((event) => {
    event.preventDefault()
    if (isRunning || readOnly) return
    setContextMenu({
      x: event.clientX,
      y: event.clientY,
    })
  }, [isRunning, readOnly])

  // Handle auto layout
  const handleAutoLayout = useCallback(() => {
    setContextMenu(null)
    if (onAutoLayout) {
      onAutoLayout()
    }
  }, [onAutoLayout])

  // Handle reset zoom
  const handleResetZoom = useCallback(() => {
    setContextMenu(null)
    const viewport = getViewport()
    setViewport({ x: viewport.x, y: viewport.y, zoom: 0.9 }, { duration: 300 })
  }, [getViewport, setViewport])

  const defaultEdgeOptions = useMemo(() => ({
    style: { stroke: '#805AD5', strokeWidth: 2 },
    type: 'editable',  // Use custom editable edge with inline waypoints
  }), [])





  // Handle node drag stop to save layout immediately
  const handleNodeDragStop = useCallback((event, node, draggedNodes) => {
    if (onLayoutSave) {
      // Use getNodes() to get the current state of ALL nodes
      // The 'draggedNodes' argument (3rd arg) only contains nodes that were dragged
      const currentNodes = getNodes()
      onLayoutSave(currentNodes, edges) 
    }
  }, [onLayoutSave, edges, getNodes])

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
        onEdgeDoubleClick={handleEdgeDoubleClick}
        onPaneClick={onPaneClick}
        onPaneContextMenu={onPaneContextMenu}
        onSelectionChange={onSelectionChange}
        selectionOnDrag={false}
        selectNodesOnDrag={false}
        panOnDrag={true}
        panOnScroll={true}
        selectionKeyCode="Shift"
        multiSelectionKeyCode="Shift"
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={defaultEdgeOptions}
        defaultViewport={{ x: 50, y: 30, zoom: 0.8 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable={!isRunning}
        nodesConnectable={!isRunning}
        elementsSelectable={!isRunning}
        deleteKeyCode={['Backspace', 'Delete']}
        colorMode={theme}
        minZoom={0.1}
        maxZoom={2}
        onNodeDragStop={handleNodeDragStop}
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
              className="flex flex-col gap-2 p-2 rounded-xl shadow-lg"
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border-color)' }}
            >
              <div className="text-xs text-center mb-1" style={{ color: 'var(--text-muted)' }}>
                Add Node
              </div>
              {NODE_TYPES.map(({ type, label, icon: Icon, iconColor }) => (
                <button
                  key={type}
                  onClick={() => onAddNode && onAddNode(type)}
                  className="flex items-center gap-2 px-3 py-2 rounded-lg transition-all hover:scale-105"
                  style={{ 
                    background: 'var(--overflow-node-bg)',
                    border: '1px solid var(--overflow-node-border)',
                    minWidth: '80px'
                  }}
                  title={`Add ${label} node`}
                >
                  <div 
                    style={{
                      background: 'var(--overflow-icon-bg)',
                      borderRadius: '6px',
                      padding: '6px',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                    }}
                  >
                    <Icon size={14} style={{ color: iconColor }} />
                  </div>
                  <span className="text-xs font-medium" style={{ color: 'var(--overflow-node-title)' }}>{label}</span>
                </button>
              ))}
            </div>
          </Panel>
        )}
        
        {/* Empty Canvas State - Create with AI - hidden in read-only mode */}
        {!isRunning && !readOnly && isEmptyCanvas && onOpenAIChat && (
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
        
        {/* Multi-Selection AI Assist Button - hidden in read-only mode */}
        {!isRunning && !readOnly && hasMultiSelection && onOpenAIChat && (
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
      
      {/* Context Menu */}
      {contextMenu && (
        <div
          className="fixed z-50 py-1 rounded-lg shadow-xl"
          style={{
            left: contextMenu.x,
            top: contextMenu.y,
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border-color)',
            minWidth: '160px'
          }}
        >
          <button
            onClick={handleAutoLayout}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-purple-500/15 transition-colors"
            style={{ color: 'var(--text-primary)' }}
          >
            <LayoutDashboard size={16} className="text-purple-400" />
            Auto Layout
          </button>
          <button
            onClick={handleResetZoom}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-purple-500/15 transition-colors"
            style={{ color: 'var(--text-primary)' }}
          >
            <Maximize size={16} className="text-purple-400" />
            Reset Zoom
          </button>
        </div>
      )}
      
      {/* Quick Add Node Popover */}
      {addNodePopover && (
        <NodeTypePopover
          position={addNodePopover.position}
          onSelect={handleNodeTypeSelect}
          onClose={() => setAddNodePopover(null)}
          theme={theme}
        />
      )}
    </div>
  )
}

// Wrapper component - agent changes handled by key prop in App.jsx
export default function FlowCanvas(props) {
  return (
    <ReactFlowProvider>
      <FlowCanvasInner {...props} />
    </ReactFlowProvider>
  )
}

