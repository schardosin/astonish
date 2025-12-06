import { useCallback, useMemo, useEffect, useRef } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  useReactFlow,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

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

function FlowCanvasInner({ nodes: propNodes, edges: propEdges, isRunning, theme }) {
  const [nodes, setNodes, handleNodesChange] = useNodesState([])
  const [edges, setEdges, handleEdgesChange] = useEdgesState([])
  const { fitView } = useReactFlow()
  const prevNodesLengthRef = useRef(0)

  // Sync nodes/edges from props when they change and trigger fitView
  useEffect(() => {
    if (propNodes && propNodes.length > 0) {
      setNodes(propNodes)
      
      // Trigger fitView when nodes change significantly
      // Small delay to let React Flow update first
      const timer = setTimeout(() => {
        fitView({ padding: 0.2, duration: 300 })
      }, 50)
      
      prevNodesLengthRef.current = propNodes.length
      return () => clearTimeout(timer)
    }
  }, [propNodes, setNodes, fitView])

  useEffect(() => {
    if (propEdges) {
      setEdges(propEdges)
    }
  }, [propEdges, setEdges])

  const onConnect = useCallback(
    (params) => setEdges((eds) => addEdge({ ...params, animated: true }, eds)),
    [setEdges]
  )

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
      </ReactFlow>
    </div>
  )
}

// Wrapper component that provides a key to force re-mount when flow structure changes dramatically
export default function FlowCanvas({ nodes, edges, isRunning, theme }) {
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
    />
  )
}
