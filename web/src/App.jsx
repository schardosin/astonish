import { useState, useCallback, useMemo, useEffect, useRef } from 'react'
import { ReactFlowProvider } from '@xyflow/react'
import yaml from 'js-yaml'
import TopBar from './components/TopBar'
import Sidebar from './components/Sidebar'
import FlowCanvas from './components/FlowCanvas'
import ChatPanel from './components/ChatPanel'
import YamlDrawer from './components/YamlDrawer'
import Header from './components/Header'
import NodeEditor from './components/NodeEditor'
import CreateAgentModal from './components/CreateAgentModal'
import ConfirmDeleteModal from './components/ConfirmDeleteModal'
import { useTheme } from './hooks/useTheme'
import { yamlToFlowAsync, extractLayout } from './utils/yamlToFlow'
import { addStandaloneNode, addConnection, removeConnection, updateNode } from './utils/flowToYaml'
import { fetchAgents, fetchAgent, saveAgent, deleteAgent, fetchTools } from './api/agents'
import { snakeToTitleCase } from './utils/formatters'
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
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [availableTools, setAvailableTools] = useState([])
  const [nodes, setNodes] = useState([])
  const [edges, setEdges] = useState([])
  const currentFlowNodesRef = useRef([])
  const currentFlowEdgesRef = useRef([])
  const [chatMessages, setChatMessages] = useState([
    { type: 'agent', content: 'Welcome! Click "Run" to start the agent flow.' },
  ])

  // Load agents and tools from API on mount
  useEffect(() => {
    loadAgents()
    loadTools()
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

  const loadTools = async () => {
    try {
      const data = await fetchTools()
      setAvailableTools(data.tools || [])
    } catch (err) {
      console.error('Failed to load tools:', err)
      setAvailableTools([])
    }
  }

  // Parse YAML and generate flow (async with ELKjs)
  useEffect(() => {
    const layoutFlow = async () => {
      try {
        const parsed = yaml.load(yamlContent)
        const result = await yamlToFlowAsync(parsed)
        setNodes(result.nodes)
        setEdges(result.edges)
      } catch (e) {
        console.error('YAML parse error:', e)
        setNodes([])
        setEdges([])
      }
    }
    layoutFlow()
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
    setShowCreateModal(true)
  }, [])

  const handleCreateAgent = useCallback(({ id, name, description }) => {
    const newYaml = `description: ${description || name}

nodes: []

flow:
  - from: START
    to: END
`
    
    setSelectedAgent({ id, name, description: description || name, isNew: true })
    setSelectedNodeId(null)
    setEditingNode(null)
    setYamlContent(newYaml)
    setShowCreateModal(false)
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

  // Track layout changes from FlowCanvas
  const handleLayoutChange = useCallback((flowNodes, flowEdges) => {
    currentFlowNodesRef.current = flowNodes
    currentFlowEdgesRef.current = flowEdges
  }, [])

  // Save agent to backend (including layout)
  const handleSave = useCallback(async () => {
    if (!selectedAgent) return
    
    setIsSaving(true)
    try {
      // Parse current YAML
      const parsed = yaml.load(yamlContent) || {}
      
      // Extract layout from current flow state
      const layout = extractLayout(currentFlowNodesRef.current, currentFlowEdgesRef.current)
      
      // Merge layout into parsed YAML
      parsed.layout = layout
      
      // Convert back to YAML string
      const updatedYaml = yaml.dump(parsed, { 
        indent: 2, 
        lineWidth: -1, // Don't wrap lines
        noRefs: true,
        sortKeys: false // Preserve order
      })
      
      await saveAgent(selectedAgent.id, updatedYaml)
      
      // Update local YAML content with layout
      setYamlContent(updatedYaml)
      
      // Refresh agent list in case description changed
      loadAgents()
    } catch (err) {
      console.error('Failed to save agent:', err)
      alert('Failed to save agent: ' + err.message)
    } finally {
      setIsSaving(false)
    }
  }, [selectedAgent, yamlContent])

  // Delete agent
  const handleDeleteAgent = useCallback((agent) => {
    setDeleteTarget(agent)
  }, [])

  const confirmDelete = useCallback(async () => {
    if (!deleteTarget) return
    
    try {
      await deleteAgent(deleteTarget.id)
      // Refresh agent list
      loadAgents()
      // If we deleted the selected agent, clear selection
      if (selectedAgent?.id === deleteTarget.id) {
        setSelectedAgent(null)
        setYamlContent(defaultYaml)
      }
    } catch (err) {
      console.error('Failed to delete agent:', err)
      alert('Failed to delete agent: ' + err.message)
    } finally {
      setDeleteTarget(null)
    }
  }, [deleteTarget, selectedAgent])

  return (
    <ReactFlowProvider>
      <div className="flex flex-col h-screen" style={{ background: 'var(--bg-primary)' }}>
        {/* Top Bar */}
        <TopBar 
          theme={theme} 
          onToggleTheme={toggleTheme}
          onOpenSettings={() => console.log('Settings clicked')}
        />

        {/* Main Content Area */}
        <div className="flex flex-1 overflow-hidden">
          {/* Sidebar */}
          <Sidebar
            agents={agents}
            selectedAgent={selectedAgent}
            onAgentSelect={handleAgentSelect}
            onCreateNew={handleCreateNew}
            onDeleteAgent={handleDeleteAgent}
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
                onLayoutChange={handleLayoutChange}
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
                  availableTools={availableTools}
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
      </div>

      {/* Create Agent Modal */}
      <CreateAgentModal
        isOpen={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onCreate={handleCreateAgent}
      />

      {/* Delete Confirmation Modal */}
      <ConfirmDeleteModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={confirmDelete}
        agentName={deleteTarget ? snakeToTitleCase(deleteTarget.name) : ''}
      />
    </ReactFlowProvider>
  )
}

export default App
