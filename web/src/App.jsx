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
import AIChatPanel from './components/AIChatPanel'
import SettingsPage from './components/SettingsPage'
import { useTheme } from './hooks/useTheme'
import { useHashRouter, buildPath } from './hooks/useHashRouter'
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
  const { path, navigate, replaceHash } = useHashRouter()
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
  const [showAIChat, setShowAIChat] = useState(false)
  const [aiChatContext, setAIChatContext] = useState('create_flow')
  const [aiFocusedNode, setAIFocusedNode] = useState(null)  // Node being edited when AI chat opens
  const [aiSelectedNodeIds, setAISelectedNodeIds] = useState([])  // Multi-selected nodes for AI
  const [defaultProvider, setDefaultProvider] = useState('')
  const [defaultModel, setDefaultModel] = useState('')

  // Derive showSettings from path
  const showSettings = path.view === 'settings'
  const settingsSection = path.params.section || 'general'

  // Load agents, tools, and settings from API on mount
  useEffect(() => {
    loadAgents()
    loadTools()
    loadSettings()
  }, [])

  const loadSettings = async () => {
    try {
      const res = await fetch('/api/settings/config')
      if (res.ok) {
        const data = await res.json()
        // Use display name from API for proper formatting
        setDefaultProvider(data.general?.default_provider_display_name || data.general?.default_provider || '')
        setDefaultModel(data.general?.default_model || '')
      }
    } catch (err) {
      console.error('Failed to load settings:', err)
    }
  }

  const loadAgents = async () => {
    try {
      setIsLoadingAgents(true)
      const data = await fetchAgents()
      const agentsList = data.agents || []
      setAgents(agentsList)
      
      // Check if URL specifies an agent
      const urlAgentName = path.view === 'agent' ? path.params.agentName : null
      
      if (urlAgentName && agentsList.length > 0) {
        // Try to find the agent from URL
        const urlAgent = agentsList.find(a => a.id === urlAgentName || a.name === urlAgentName)
        if (urlAgent) {
          handleAgentSelectInternal(urlAgent, false) // Don't update URL, already there
        } else if (agentsList.length > 0) {
          // Agent not found, select first and update URL
          handleAgentSelectInternal(agentsList[0], true)
        }
      } else if (agentsList.length > 0 && !selectedAgent && path.view !== 'settings') {
        // No URL agent, auto-select first
        handleAgentSelectInternal(agentsList[0], true)
      }
    } catch (err) {
      console.error('Failed to load agents:', err)
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

  // React to URL path changes (hash navigation)
  useEffect(() => {
    if (path.view === 'agent' && path.params.agentName && agents.length > 0) {
      const targetAgent = agents.find(a => a.id === path.params.agentName || a.name === path.params.agentName)
      // Only switch if it's a different agent than currently selected
      if (targetAgent && targetAgent.id !== selectedAgent?.id) {
        handleAgentSelectInternal(targetAgent, false)
      }
    }
  }, [path, agents]) // Re-run when path or agents list changes

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

  // Refresh editingNode when nodes update (e.g., after AI applies changes)
  useEffect(() => {
    if (editingNode && nodes.length > 0) {
      const updatedNode = nodes.find(n => n.id === editingNode.id)
      if (updatedNode && JSON.stringify(updatedNode) !== JSON.stringify(editingNode)) {
        setEditingNode(updatedNode)
      }
    }
  }, [nodes, editingNode])

  // Internal agent select (optionally updates URL)
  const handleAgentSelectInternal = useCallback(async (agent, updateUrl = true) => {
    setSelectedAgent(agent)
    setSelectedNodeId(null)
    setEditingNode(null)
    
    // Update URL if requested
    if (updateUrl) {
      navigate(buildPath('agent', { agentName: agent.id }))
    }
    
    // Load agent YAML from API
    try {
      const data = await fetchAgent(agent.id)
      setYamlContent(data.yaml || defaultYaml)
    } catch (err) {
      console.error('Failed to load agent:', err)
      setYamlContent(defaultYaml)
    }
  }, [navigate])

  // Public agent select (always updates URL)
  const handleAgentSelect = useCallback(async (agent) => {
    await handleAgentSelectInternal(agent, true)
  }, [handleAgentSelectInternal])

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
    
    // Update URL using navigate (triggers hashchange) so we stay on this agent after save
    navigate(`/agent/${encodeURIComponent(id)}`)
  }, [navigate])

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
      
      const result = await saveAgent(selectedAgent.id, updatedYaml)
      
      // Update local YAML content with layout
      setYamlContent(updatedYaml)
      
      // If this was a new agent, mark it as saved (no longer new)
      if (selectedAgent.isNew) {
        setSelectedAgent({ ...selectedAgent, isNew: false })
      }
      
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
          onOpenSettings={() => navigate(buildPath('settings', { section: 'general' }))}
          defaultProvider={defaultProvider}
          defaultModel={defaultModel}
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
                key={selectedAgent ? selectedAgent.id : 'empty'}
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
                onOpenAIChat={(options) => {
                  if (options?.context === 'multi_node' && options?.nodeIds) {
                    // Multi-node context
                    setAIChatContext('multi_node')
                    setAISelectedNodeIds(options.nodeIds)
                    setAIFocusedNode(null)
                  } else {
                    // Default flow context
                    setAIChatContext('create_flow')
                    setAISelectedNodeIds([])
                    setAIFocusedNode(null)
                  }
                  setShowAIChat(true)
                }}
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
                  onAIAssist={(node, nodeName, nodeData) => {
                    setAIChatContext('node_config')
                    setAIFocusedNode({ name: nodeName, type: node.data?.nodeType || node.type, data: nodeData })
                    setShowAIChat(true)
                  }}
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

      {/* AI Chat Panel */}
      <AIChatPanel
        isOpen={showAIChat}
        onClose={() => { setShowAIChat(false); setAIFocusedNode(null); setAISelectedNodeIds([]); }}
        context={aiChatContext}
        currentYaml={yamlContent}
        selectedNodes={aiSelectedNodeIds.length > 0 ? aiSelectedNodeIds : (selectedNodeId ? [selectedNodeId] : [])}
        focusedNode={aiFocusedNode}
        agentId={selectedAgent?.id}
        onPreviewYaml={(newYaml) => {
          // Preview: update flow but don't save
          setYamlContent(newYaml)
        }}
        onApplyYaml={(newYaml) => {
          // Apply: update flow and trigger save
          setYamlContent(newYaml)
          // Auto-save after applying
          if (selectedAgent) {
            saveAgent(selectedAgent.id, newYaml).then(() => {
              console.log('Auto-saved after AI changes')
            }).catch(err => {
              console.error('Failed to auto-save:', err)
            })
          }
        }}
      />

      {/* AI Chat Toggle Button */}
      {selectedAgent && (
        <button
          onClick={() => setShowAIChat(true)}
          className="fixed bottom-4 right-4 w-14 h-14 bg-gradient-to-r from-purple-600 to-blue-600 hover:from-purple-500 hover:to-blue-500 rounded-full shadow-lg flex items-center justify-center transition-all hover:scale-110 z-40"
          title="AI Assistant"
        >
          <svg className="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
          </svg>
        </button>
      )}

      {/* Settings Page */}
      {showSettings && (
        <SettingsPage
          onClose={() => {
            if (selectedAgent) {
              navigate(buildPath('agent', { agentName: selectedAgent.id }))
            } else {
              navigate('/')
            }
          }}
          activeSection={settingsSection}
          onSectionChange={(section) => replaceHash(buildPath('settings', { section }))}
          theme={theme}
        />
      )}
    </ReactFlowProvider>
  )
}

export default App
