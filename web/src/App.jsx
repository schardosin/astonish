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
import SetupWizard from './components/SetupWizard'
import HomePage from './components/HomePage'
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
  
  // UI State
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showAIChat, setShowAIChat] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(null)
  
  // Flow State
  const [availableTools, setAvailableTools] = useState([])
  const [nodes, setNodes] = useState([])
  const [edges, setEdges] = useState([])

  // Refs
  const currentFlowNodesRef = useRef([])
  const currentFlowEdgesRef = useRef([])
  const abortControllerRef = useRef(null)
  
  // Chat State
  const [chatMessages, setChatMessages] = useState([
    { type: 'agent', content: 'Welcome! Click "Run" to start the agent flow.' },
  ])
  const [aiChatContext, setAIChatContext] = useState('create_flow')
  const [aiFocusedNode, setAIFocusedNode] = useState(null)  // Node being edited when AI chat opens
  const [aiSelectedNodeIds, setAISelectedNodeIds] = useState([])  // Multi-selected nodes for AI
  const [defaultProvider, setDefaultProvider] = useState('')
  const [defaultModel, setDefaultModel] = useState('')
  const [runningNodeId, setRunningNodeId] = useState(null)
  const [sessionId, setSessionId] = useState(null)
  const [isWaitingForInput, setIsWaitingForInput] = useState(false)
  
  // Undo/Redo History (max 100 versions)
  const [yamlHistory, setYamlHistory] = useState([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const MAX_HISTORY = 100

  // Derive showSettings from path
  const showSettings = path.view === 'settings'
  const settingsSection = path.params.section || 'general'

  // Setup wizard state
  const [showSetupWizard, setShowSetupWizard] = useState(false)
  const [isCheckingSetup, setIsCheckingSetup] = useState(true)

  // Check if setup is required on mount
  useEffect(() => {
    checkSetupStatus()
  }, [])

  const checkSetupStatus = async () => {
    try {
      setIsCheckingSetup(true)
      const res = await fetch('/api/settings/status')
      if (res.ok) {
        const data = await res.json()
        setShowSetupWizard(data.setupRequired)
      }
    } catch (err) {
      console.error('Failed to check setup status:', err)
      // If we can't check, assume setup is required
      setShowSetupWizard(true)
    } finally {
      setIsCheckingSetup(false)
    }
  }

  // Load agents, tools, and settings from API on mount
  useEffect(() => {
    if (!showSetupWizard && !isCheckingSetup) {
      loadAgents()
      loadTools()
      loadSettings()
    }
  }, [showSetupWizard, isCheckingSetup])

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

  // Push new YAML to history (called after applying changes)
  const pushToHistory = useCallback((newYaml) => {
    if (!newYaml) return
    setYamlHistory(prev => {
      // If we're not at the end, truncate forward history
      const truncated = prev.slice(0, historyIndex + 1)
      // Add new entry
      const updated = [...truncated, newYaml]
      // Keep only last MAX_HISTORY entries
      if (updated.length > MAX_HISTORY) {
        return updated.slice(-MAX_HISTORY)
      }
      return updated
    })
    setHistoryIndex(prev => Math.min(prev + 1, MAX_HISTORY - 1))
  }, [historyIndex])

  // Undo: go back in history
  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      setHistoryIndex(prev => prev - 1)
      setYamlContent(yamlHistory[historyIndex - 1])
    }
  }, [historyIndex, yamlHistory])

  // Redo: go forward in history
  const handleRedo = useCallback(() => {
    if (historyIndex < yamlHistory.length - 1) {
      setHistoryIndex(prev => prev + 1)
      setYamlContent(yamlHistory[historyIndex + 1])
    }
  }, [historyIndex, yamlHistory])

  // Keyboard shortcuts for undo/redo
  useEffect(() => {
    const handleKeyDown = (e) => {
      // Only handle when not in an input/textarea
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return
      
      if ((e.metaKey || e.ctrlKey) && e.key === 'z') {
        e.preventDefault()
        if (e.shiftKey) {
          handleRedo()
        } else {
          handleUndo()
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleUndo, handleRedo])

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
    // Reset running state when switching agents
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    setIsRunning(false)
    setRunningNodeId(null)
    setSessionId(null)
    setChatMessages([])
    setIsWaitingForInput(false)
    
    // Reset undo/redo history for new agent
    setYamlHistory([])
    setHistoryIndex(-1)
    
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
      const loadedYaml = data.yaml || defaultYaml
      setYamlContent(loadedYaml)
      // Initialize history with loaded state
      setYamlHistory([loadedYaml])
      setHistoryIndex(0)
    } catch (err) {
      console.error('Failed to load agent:', err)
      setYamlContent(defaultYaml)
      setYamlHistory([defaultYaml])
      setHistoryIndex(0)
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
    // Reset and initialize history for new agent
    setYamlHistory([newYaml])
    setHistoryIndex(0)
    setShowCreateModal(false)
    
    // Update URL using navigate (triggers hashchange) so we stay on this agent after save
    navigate(`/agent/${encodeURIComponent(id)}`)
  }, [navigate])

  const connectToChat = useCallback(async (currentSessionId, message = '') => {
    try {
      if (message) {
        setChatMessages(prev => [...prev, { type: 'user', content: message }])
        setIsWaitingForInput(false)
      }

      const controller = new AbortController()
      abortControllerRef.current = controller

      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          agentId: selectedAgent.id,
          message: message,
          sessionId: currentSessionId,
          provider: defaultProvider, // Use default or selected
          model: defaultModel
        })
      })

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n\n')
        buffer = lines.pop() // Keep incomplete line

        for (const block of lines) {
          // SSE blocks can contain multiple lines (event: ..., data: ...)
          const blockLines = block.split('\n')
          
          for (const line of blockLines) {
            if (line.startsWith('data: ')) {
              const dataStr = line.slice(6)
              try {
                const data = JSON.parse(dataStr)
                
                if (data.error) {
                   setChatMessages(prev => [...prev, { type: 'error', content: data.error }])
                } else if (data.text) {
                  // Determine if we should append to last agent message or create new one
                  setChatMessages(prev => {
                    const last = prev[prev.length - 1]
                    if (last && last.type === 'agent') {
                      return [...prev.slice(0, -1), { ...last, content: last.content + data.text }]
                    }
                    return [...prev, { type: 'agent', content: data.text }]
                  })
                } else if (data.node) {
                  setRunningNodeId(data.node)
                  setChatMessages(prev => [...prev, { type: 'node', nodeName: data.node }])
                  if (data.node === 'END') {
                     setRunningNodeId(null)
                     setChatMessages(prev => [...prev, { type: 'flow_complete' }])
                  }
                } else if (data.options !== undefined) { // Handle input_request (options may be empty for free-text)
                  setIsWaitingForInput(true)
                  // Only add to chat if there are options to display
                  if (data.options.length > 0) {
                    setChatMessages(prev => [...prev, { 
                      type: 'input_request', 
                      options: data.options 
                    }])
                  }
                } else if (data.input_request) { // Handle nested format just in case
                   setIsWaitingForInput(true)
                   if (data.input_request.options && data.input_request.options.length > 0) {
                     setChatMessages(prev => [...prev, { 
                       type: 'input_request', 
                       options: data.input_request.options 
                     }])
                   }
                } else if (data.done) {
                   // Clean finish
                }
              } catch (e) {
                console.error('Error parsing SSE data:', e, 'Line:', line)
              }
            }
          }
        }
      }
    } catch (err) {
      if (err.name === 'AbortError') {
        setChatMessages(prev => [
           ...prev, 
           { type: 'system', content: 'Execution stopped by user.' },
           { type: 'flow_complete' }
        ])
        setRunningNodeId(null)
        setIsWaitingForInput(false)
      } else {
        console.error('Chat error:', err)
        setChatMessages(prev => [...prev, { type: 'error', content: err.message }])
        setRunningNodeId(null)
      }
    } finally {
      abortControllerRef.current = null
    }
  }, [selectedAgent, defaultProvider, defaultModel])

  const handleRun = useCallback(() => {
    setIsRunning(true)
    setEditingNode(null)
    setChatMessages([]) // Clear history
    setRunningNodeId(null)
    // Don't auto-start, wait for user to click Start
  }, [])

  const handleStartRun = useCallback(() => {
    setChatMessages([{ type: 'system', content: 'Execution started...' }])
    setRunningNodeId(null)
    const newSessionId = `session-${Date.now()}`
    setSessionId(newSessionId)
    connectToChat(newSessionId)
  }, [connectToChat])

  const handleSendMessage = useCallback((msg) => {
    if (sessionId) {
      connectToChat(sessionId, msg)
    }
  }, [sessionId, connectToChat])

  const handleStopRun = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    } else if (isWaitingForInput) {
       // If waiting for input, the connection is closed, so we must manually trigger stop state
       setChatMessages(prev => [
         ...prev, 
         { type: 'system', content: 'Execution stopped by user.' },
         { type: 'flow_complete' }
      ])
      setRunningNodeId(null)
      setIsWaitingForInput(false)
    }
  }, [isWaitingForInput])

  const handleExitRun = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    setIsRunning(false)
    setRunningNodeId(null)
    setChatMessages([])
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

  // Handle node deletion from FlowCanvas - update YAML immediately
  const handleNodeDelete = useCallback((nodeId) => {
    console.log(`[NODE DELETE] Removing node: ${nodeId}`)
    
    // Parse current YAML and remove the node
    try {
      const parsed = yaml.load(yamlContent) || {}
      
      if (parsed.nodes && Array.isArray(parsed.nodes)) {
        parsed.nodes = parsed.nodes.filter(n => n.name !== nodeId)
      }
      
      // Also clean up flow edges that reference this node
      if (parsed.flow && Array.isArray(parsed.flow)) {
        parsed.flow = parsed.flow.filter(edge => {
          if (edge.from === nodeId) return false
          if (edge.to === nodeId) return false
          // Handle conditional edges
          if (edge.edges && Array.isArray(edge.edges)) {
            edge.edges = edge.edges.filter(e => e.to !== nodeId)
            return edge.edges.length > 0 || edge.to
          }
          return true
        })
      }
      
      // Clean up layout
      if (parsed.layout && parsed.layout.nodes) {
        delete parsed.layout.nodes[nodeId]
      }
      
      const newYaml = yaml.dump(parsed, { 
        indent: 2, 
        lineWidth: -1,
        noRefs: true,
        sortKeys: false
      })
      
      setYamlContent(newYaml)
      
      // Also update local nodes/edges state to keep in sync
      setNodes(prev => prev.filter(n => n.id !== nodeId))
      setEdges(prev => prev.filter(e => e.source !== nodeId && e.target !== nodeId))
      
    } catch (err) {
      console.error('Failed to delete node from YAML:', err)
    }
  }, [yamlContent])

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
      
      // Sync node deletions: only keep nodes that exist in the current flow
      // (handles case where user deleted nodes in the UI)
      const currentFlowNodeIds = new Set(
        currentFlowNodesRef.current
          ?.filter(n => !['start', 'end', 'waypoint'].includes(n.type))
          .map(n => n.id) || []
      )
      
      if (parsed.nodes && Array.isArray(parsed.nodes)) {
        // Filter out nodes that were deleted
        const originalNodeCount = parsed.nodes.length
        parsed.nodes = parsed.nodes.filter(node => currentFlowNodeIds.has(node.name))
        
        if (parsed.nodes.length !== originalNodeCount) {
          console.log(`[SAVE] Synced node deletions: ${originalNodeCount} -> ${parsed.nodes.length} nodes`)
          
          // Also clean up flow edges that reference deleted nodes
          if (parsed.flow && Array.isArray(parsed.flow)) {
            const validNodeNames = new Set(['START', 'END', ...parsed.nodes.map(n => n.name)])
            parsed.flow = parsed.flow.filter(edge => {
              const fromValid = validNodeNames.has(edge.from)
              const toValid = !edge.to || validNodeNames.has(edge.to)
              const edgesValid = !edge.edges || edge.edges.every(e => !e.to || validNodeNames.has(e.to))
              return fromValid && toValid && edgesValid
            })
          }
        }
      }
      
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
      {/* Setup Wizard */}
      {showSetupWizard && !isCheckingSetup && (
        <SetupWizard
          theme={theme}
          onComplete={() => {
            setShowSetupWizard(false)
            loadSettings()
            loadAgents()
            loadTools()
          }}
        />
      )}

      {/* Loading state while checking setup */}
      {isCheckingSetup && (
        <div 
          className="flex flex-col h-screen items-center justify-center"
          style={{ background: 'var(--bg-primary)' }}
        >
          <div className="animate-pulse text-purple-400 text-lg">Loading...</div>
        </div>
      )}

      {/* Main App (only show when not in setup wizard and not checking) */}
      {!showSetupWizard && !isCheckingSetup && (
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
          {/* Show HomePage when no agent is selected */}
          {!selectedAgent ? (
            <HomePage
              onCreateAgent={handleCreateNew}
              onOpenSettings={() => navigate(buildPath('settings', { section: 'general' }))}
              onOpenMCP={() => navigate(buildPath('settings', { section: 'mcp' }))}
              defaultProvider={defaultProvider}
              defaultModel={defaultModel}
              theme={theme}
            />
          ) : (
          <>
          <Header
            agentName={selectedAgent?.name || 'Select Agent'}
            showYaml={showYaml}
            onToggleYaml={() => setShowYaml(!showYaml)}
            isRunning={isRunning}
            onRun={handleRun}
            onStop={handleStopRun}
            onExit={handleExitRun}
            onSave={handleSave}
            isSaving={isSaving}
            theme={theme}
            canUndo={historyIndex > 0}
            canRedo={historyIndex < yamlHistory.length - 1}
            onUndo={handleUndo}
            onRedo={handleRedo}
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
                runningNodeId={runningNodeId}
                onAddNode={handleAddNode}
                onConnect={handleConnect}
                onEdgeRemove={handleEdgeRemove}
                onLayoutChange={handleLayoutChange}
                onNodeDelete={handleNodeDelete}
                onOpenAIChat={(options) => {
                  if (options?.context === 'multi_node' && options?.nodeIds) {
                    // Multi-node context
                    setAIChatContext('multi_node')
                    setAISelectedNodeIds(options.nodeIds)
                    setAIFocusedNode(null)
                  } else {
                    // Check if flow has existing nodes (not just START->END)
                    const hasExistingNodes = nodes.length > 0 && nodes.some(n => n.id !== 'START' && n.id !== 'END')
                    setAIChatContext(hasExistingNodes ? 'modify_flow' : 'create_flow')
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
                  onSendMessage={handleSendMessage}
                  onStartRun={handleStartRun}
                  onStop={handleStopRun}
                  isWaitingForInput={isWaitingForInput}
                  hasActiveSession={sessionId !== null}
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
          </>
          )}
        </div>
        </div>
      </div>
      )}
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
        onApplyYaml={(newYaml) => {
          // Apply the new YAML
          setYamlContent(newYaml)
          // Push new state to history (for undo)
          pushToHistory(newYaml)
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
      {selectedAgent && !isRunning && (
        <button
          onClick={() => {
            // Detect if flow has existing nodes
            const hasExistingNodes = nodes.length > 0 && nodes.some(n => n.id !== 'START' && n.id !== 'END')
            setAIChatContext(hasExistingNodes ? 'modify_flow' : 'create_flow')
            setAISelectedNodeIds([])
            setAIFocusedNode(null)
            setShowAIChat(true)
          }}
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
          onToolsRefresh={loadTools}
        />
      )}
    </ReactFlowProvider>
  )
}

export default App
