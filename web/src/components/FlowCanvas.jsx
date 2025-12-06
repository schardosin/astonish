import { useCallback, useMemo } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import StartNode from './nodes/StartNode'
import EndNode from './nodes/EndNode'
import InputNode from './nodes/InputNode'
import LlmNode from './nodes/LlmNode'
import ToolNode from './nodes/ToolNode'
import OutputNode from './nodes/OutputNode'

const nodeTypes = {
  start: StartNode,
  end: EndNode,
  input: InputNode,
  llm: LlmNode,
  tool: ToolNode,
  output: OutputNode,
}

export default function FlowCanvas({ nodes: initialNodes, edges: initialEdges, onNodesChange, onEdgesChange, isRunning }) {
  const [nodes, setNodes, handleNodesChange] = useNodesState(initialNodes)
  const [edges, setEdges, handleEdgesChange] = useEdgesState(initialEdges)

  const onConnect = useCallback(
    (params) => setEdges((eds) => addEdge({ ...params, animated: true }, eds)),
    [setEdges]
  )

  const defaultEdgeOptions = useMemo(() => ({
    style: { stroke: '#805AD5', strokeWidth: 2 },
    type: 'smoothstep',
  }), [])

  return (
    <div className="w-full h-full bg-[#F7F5FB]">
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
      >
        <Background color="#E2E8F0" gap={20} />
        <Controls className="bg-white rounded-lg shadow-md" />
        <MiniMap
          nodeColor={(node) => {
            switch (node.type) {
              case 'start': return '#38A169'
              case 'end': return '#E53E3E'
              case 'input': return '#DDD6FE'
              case 'llm': return '#6B46C1'
              case 'tool': return '#805AD5'
              case 'output': return '#9F7AEA'
              default: return '#CBD5E0'
            }
          }}
          className="bg-white rounded-lg shadow-md"
        />
      </ReactFlow>

      {/* Powered by React Flow */}
      <div className="absolute bottom-4 left-4 text-xs text-gray-400">
        Powered by <a href="https://reactflow.dev" target="_blank" rel="noopener noreferrer" className="underline hover:text-gray-600">React Flow</a>
      </div>
    </div>
  )
}
