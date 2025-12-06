import { useState, useCallback, useMemo, useEffect } from 'react'
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
import { fetchAgents, fetchAgent, saveAgent } from './api/agents'
import './index.css'

// Default YAML for new agents
const defaultYaml = `description: New Agent

nodes: []

flow:
  - from: START
    to: END
`

function App() {
  const { theme, toggleTheme } = useTheme()
  const [agents, setAgents] = useState([])
  const [isLoadingAgents, setIsLoadingAgents] = useState(true)
  const [selectedAgent, setSelectedAgent] = useState(null)
  const [yamlContent, setYamlContent] = useState(defaultYaml)
  const [showYaml, setShowYaml] = useState(false)
  const [isRunning, setIsRunning] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [isSaving, setIsSaving] = useState(false)
  const [chatMessages, setChatMessages] = useState([
    { type: 'agent', content: 'Welcome! Click "Run" to start the agent flow.' },
  ])

  // Load agents from API on mount
  useEffect(() => {
    loadAgents()
  }, [])

  const loadAgents = async () => {
    try {
      setIsLoadingAgents(true)
      const data = await fetchAgents()
      setAgents(data.agents || [])
      
      // Auto-select first agent if available
      if (data.agents && data.agents.length > 0 && !selectedAgent) {
        handleAgentSelect(data.agents[0])
      }
    } catch (err) {
      console.error('Failed to load agents:', err)
      // Keep empty array if API fails
      setAgents([])
    } finally {
      setIsLoadingAgents(false)
    }
  }

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

  const handleAgentSelect = useCallback(async (agent) => {
    setSelectedAgent(agent)
    setSelectedNodeId(null)
    setEditingNode(null)
    
    // Load agent YAML from API
    try {
      const data = await fetchAgent(agent.id)
      setYamlContent(data.yaml || defaultYaml)
    } catch (err) {
      console.error('Failed to load agent:', err)
      setYamlContent(defaultYaml)
    }
  }, [])

  const handleCreateNew = useCallback(() => {
    setSelectedAgent({ id: 'new-agent', name: 'New Agent', description: '', isNew: true })
    setSelectedNodeId(null)
    setEditingNode(null)
    setYamlContent(defaultYaml)
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

  // Save agent to backend
  const handleSave = useCallback(async () => {
    if (!selectedAgent) return
    
    setIsSaving(true)
    try {
      await saveAgent(selectedAgent.id, yamlContent)
      // Refresh agent list in case description changed
      loadAgents()
    } catch (err) {
      console.error('Failed to save agent:', err)
      alert('Failed to save agent: ' + err.message)
    } finally {
      setIsSaving(false)
    }
  }, [selectedAgent, yamlContent])

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
          isLoading={isLoadingAgents}
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
            onSave={handleSave}
            isSaving={isSaving}
            theme={theme}
          />

          {/* Flow + Chat Area */}
          <div className={`flex-1 flex overflow-hidden ${!isRunning && (editingNode || showYaml) ? 'h-1/2' : ''}`}>
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
