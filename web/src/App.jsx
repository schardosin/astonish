import { useState, useCallback, useMemo } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import yaml from 'js-yaml'
import Sidebar from './components/Sidebar'
import FlowCanvas from './components/FlowCanvas'
import ChatPanel from './components/ChatPanel'
import YamlDrawer from './components/YamlDrawer'
import Header from './components/Header'
import NodeEditor from './components/NodeEditor'
import { useTheme } from './hooks/useTheme'
import { yamlToFlow } from './utils/yamlToFlow'
import { addStandaloneNode, addConnection, removeConnection, updateNode } from './utils/flowToYaml'
import './index.css'

// Mock data for agents
const mockAgents = [
  { id: 'github-pr-review', name: 'GitHub PR Review', description: 'Review pull requests' },
  { id: 'file-summarizer', name: 'File Summarizer', description: 'Summarize file contents' },
  { id: 'agent-listager', name: 'Agent Listager', description: 'List available agents' },
]

// Sample YAML that demonstrates various node types
const sampleYaml = `description: GitHub PR Review Agent

nodes:
  - name: get_prs
    type: tool
    tool_name: list_pull_requests
    output_model:
      prs: list

  - name: list_prs
    type: llm
    prompt: "List the available PRs"
    output_model:
      pr_list: str

  - name: select_pr
    type: input
    prompt: "Select a PR number to review:"
    output_model:
      selected_pr: str

  - name: get_pr_diff
    type: tool
    tool_name: get_pull_request_diff
    output_model:
      diff: str

  - name: analyze_pr
    type: llm
    prompt: "Analyze the PR diff and provide feedback"
    output_model:
      analysis: str

  - name: new_pr
    type: input
    prompt: "Analyze another PR? (yes/no)"
    output_model:
      analyze_another: str

flow:
  - from: START
    to: get_prs
  - from: get_prs
    to: list_prs
  - from: list_prs
    to: select_pr
  - from: select_pr
    to: get_pr_diff
  - from: get_pr_diff
    to: analyze_pr
  - from: analyze_pr
    to: new_pr
  - from: new_pr
    edges:
      - to: list_prs
        condition: "lambda x: x['analyze_another'] == 'yes'"
      - to: END
        condition: "lambda x: x['analyze_another'] == 'no'"
`

function App() {
  const { theme, toggleTheme } = useTheme()
  const [agents] = useState(mockAgents)
  const [selectedAgent, setSelectedAgent] = useState(mockAgents[0])
  const [yamlContent, setYamlContent] = useState(sampleYaml)
  const [showYaml, setShowYaml] = useState(false)
  const [isRunning, setIsRunning] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [chatMessages, setChatMessages] = useState([
    { type: 'agent', content: 'Welcome! Click "Run" to start the agent flow.' },
  ])

  // Parse YAML and generate flow
  const { nodes, edges } = useMemo(() => {
    try {
      const parsed = yaml.load(yamlContent)
      return yamlToFlow(parsed)
    } catch (e) {
      console.error('YAML parse error:', e)
      return { nodes: [], edges: [] }
    }
  }, [yamlContent])

  const handleAgentSelect = useCallback((agent) => {
    setSelectedAgent(agent)
    setSelectedNodeId(null)
    setEditingNode(null)
  }, [])

  const handleCreateNew = useCallback(() => {
    setSelectedAgent({ id: 'new', name: 'New Agent', description: '' })
    setSelectedNodeId(null)
    setEditingNode(null)
    setYamlContent(`description: New Agent

nodes: []

flow:
  - from: START
    to: END
`)
  }, [])

  const handleRun = useCallback(() => {
    setIsRunning(true)
    setEditingNode(null)
  }, [])

  const handleStopRun = useCallback(() => {
    setIsRunning(false)
  }, [])

  const handleYamlChange = useCallback((newYaml) => {
    setYamlContent(newYaml)
  }, [])

  const handleNodeSelect = useCallback((nodeId) => {
    setSelectedNodeId(nodeId)
  }, [])

  // Double-click to open editor
  const handleNodeDoubleClick = useCallback((nodeId) => {
    const node = nodes.find(n => n.id === nodeId)
    if (node && node.type !== 'start' && node.type !== 'end') {
      setEditingNode(node)
    }
  }, [nodes])

  // Add standalone node
  const handleAddNode = useCallback((nodeType) => {
    const newYaml = addStandaloneNode(yamlContent, nodeType)
    setYamlContent(newYaml)
  }, [yamlContent])

  // Handle new connection
  const handleConnect = useCallback((sourceId, targetId) => {
    const newYaml = addConnection(yamlContent, sourceId, targetId)
    setYamlContent(newYaml)
  }, [yamlContent])

  // Handle edge removal
  const handleEdgeRemove = useCallback((sourceId, targetId) => {
    const newYaml = removeConnection(yamlContent, sourceId, targetId)
    setYamlContent(newYaml)
  }, [yamlContent])

  // Save node edits
  const handleNodeSave = useCallback((nodeId, newData) => {
    const newYaml = updateNode(yamlContent, nodeId, newData)
    setYamlContent(newYaml)
    setEditingNode(null)
  }, [yamlContent])

  // Close node editor
  const handleNodeEditorClose = useCallback(() => {
    setEditingNode(null)
  }, [])

  return (
    <ReactFlowProvider>
      <div className="flex h-screen" style={{ background: 'var(--bg-primary)' }}>
        {/* Sidebar */}
        <Sidebar
          agents={agents}
          selectedAgent={selectedAgent}
          onAgentSelect={handleAgentSelect}
          onCreateNew={handleCreateNew}
          theme={theme}
          onToggleTheme={toggleTheme}
        />

        {/* Main Content */}
        <div className="flex-1 flex flex-col overflow-hidden">
          <Header
            agentName={selectedAgent?.name || 'Select Agent'}
            showYaml={showYaml}
            onToggleYaml={() => setShowYaml(!showYaml)}
            isRunning={isRunning}
            onRun={handleRun}
            onStop={handleStopRun}
            theme={theme}
          />

          {/* Flow + Chat + Editor Area */}
          <div className={`flex-1 flex overflow-hidden ${showYaml && !isRunning ? 'h-1/2' : ''}`}>
            {/* Flow Canvas */}
            <div className={`flex-1 transition-all duration-300 ${isRunning ? 'w-1/2' : ''}`}>
              <FlowCanvas
                nodes={nodes}
                edges={edges}
                isRunning={isRunning}
                theme={theme}
                onNodeSelect={handleNodeSelect}
                onNodeDoubleClick={handleNodeDoubleClick}
                selectedNodeId={selectedNodeId}
                onAddNode={handleAddNode}
                onConnect={handleConnect}
                onEdgeRemove={handleEdgeRemove}
              />
            </div>

            {/* Chat Panel (visible when running) */}
            {isRunning && (
              <div className="w-1/2" style={{ borderLeft: '1px solid var(--border-color)' }}>
                <ChatPanel
                  messages={chatMessages}
                  onSendMessage={(msg) => setChatMessages([...chatMessages, { type: 'user', content: msg }])}
                  theme={theme}
                />
              </div>
            )}
          </div>

          {/* Bottom Panel - Node Editor OR YAML Drawer */}
          {!isRunning && (editingNode || showYaml) && (
            <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
              {editingNode ? (
                <NodeEditor
                  node={editingNode}
                  onSave={handleNodeSave}
                  onClose={handleNodeEditorClose}
                  theme={theme}
                />
              ) : (
                <YamlDrawer
                  content={yamlContent}
                  onChange={handleYamlChange}
                  onClose={() => setShowYaml(false)}
                  theme={theme}
                />
              )}
            </div>
          )}
        </div>
      </div>
    </ReactFlowProvider>
  )
}

export default App
