import { useCallback, useMemo, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  useReactFlow,
  Panel,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Edit3, Brain, Wrench, Settings, MessageSquare } from 'lucide-react'

import StartNode from './nodes/StartNode'
import EndNode from './nodes/EndNode'
import InputNode from './nodes/InputNode'
import LlmNode from './nodes/LlmNode'
import ToolNode from './nodes/ToolNode'
import OutputNode from './nodes/OutputNode'
import UpdateStateNode from './nodes/UpdateStateNode'

const nodeTypes = {
  start: StartNode,
  end: EndNode,
  input: InputNode,
  llm: LlmNode,
  tool: ToolNode,
  output: OutputNode,
  updateState: UpdateStateNode,
}

// Node type definitions for toolbar
const NODE_TYPES = [
  { type: 'input', label: 'Input', icon: Edit3, color: '#E9D5FF', darkColor: '#3B2667' },
  { type: 'llm', label: 'LLM', icon: Brain, color: '#6B46C1', darkColor: '#6B46C1' },
  { type: 'tool', label: 'Tool', icon: Wrench, color: '#805AD5', darkColor: '#805AD5' },
  { type: 'updateState', label: 'State', icon: Settings, color: '#4A5568', darkColor: '#4A5568' },
  { type: 'output', label: 'Output', icon: MessageSquare, color: '#9F7AEA', darkColor: '#9F7AEA' },
]

function FlowCanvasInner({ nodes: propNodes, edges: propEdges, isRunning, theme, onNodeSelect, selectedNodeId, onAddNode }) {
  const [nodes, setNodes, handleNodesChange] = useNodesState([])
  const [edges, setEdges, handleEdgesChange] = useEdgesState([])
  const { fitView } = useReactFlow()

  // Sync nodes/edges from props when they change and trigger fitView
  useEffect(() => {
    if (propNodes && propNodes.length > 0) {
      // Add selected state to nodes
      const nodesWithSelection = propNodes.map(node => ({
        ...node,
        selected: node.id === selectedNodeId,
        data: {
          ...node.data,
          isSelected: node.id === selectedNodeId
        }
      }))
      setNodes(nodesWithSelection)
      
      // Trigger fitView when nodes change significantly
      const timer = setTimeout(() => {
        fitView({ padding: 0.2, duration: 300 })
      }, 50)
      
      return () => clearTimeout(timer)
    }
  }, [propNodes, selectedNodeId, setNodes, fitView])

  useEffect(() => {
    if (propEdges) {
      setEdges(propEdges)
    }
  }, [propEdges, setEdges])

  const onConnect = useCallback(
    (params) => setEdges((eds) => addEdge({ ...params, animated: true }, eds)),
    [setEdges]
  )

  const onNodeClick = useCallback((event, node) => {
    if (onNodeSelect) {
      onNodeSelect(node.id)
    }
  }, [onNodeSelect])

  const onPaneClick = useCallback(() => {
    // Clicking on empty space selects START
    if (onNodeSelect) {
      onNodeSelect('START')
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
        onNodeClick={onNodeClick}
        onPaneClick={onPaneClick}
        nodeTypes={nodeTypes}
        defaultEdgeOptions={defaultEdgeOptions}
        fitView
        proOptions={{ hideAttribution: true }}
        nodesDraggable={!isRunning}
        nodesConnectable={!isRunning}
        elementsSelectable={!isRunning}
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
          <Panel position="top-left" className="m-2">
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
                  title={`Add ${label} node after ${selectedNodeId || 'START'}`}
                >
                  <Icon size={18} />
                  <span className="text-[10px] font-medium">{label}</span>
                </button>
              ))}
            </div>
          </Panel>
        )}
      </ReactFlow>
    </div>
  )
}

// Wrapper component that provides a key to force re-mount when flow structure changes dramatically
export default function FlowCanvas({ nodes, edges, isRunning, theme, onNodeSelect, selectedNodeId, onAddNode }) {
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
      selectedNodeId={selectedNodeId}
      onAddNode={onAddNode}
    />
  )
}
