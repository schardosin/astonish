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
import EdgeEditor from './components/EdgeEditor'
import CreateAgentModal from './components/CreateAgentModal'
import ConfirmDeleteModal from './components/ConfirmDeleteModal'
import AIChatPanel from './components/AIChatPanel'
import SettingsPage from './components/SettingsPage'
import SetupWizard from './components/SetupWizard'
import HomePage from './components/HomePage'
import MCPDependenciesPanel from './components/MCPDependenciesPanel'
import InstallMcpModal from './components/InstallMcpModal'
import { useTheme } from './hooks/useTheme'
import { useHashRouter, buildPath } from './hooks/useHashRouter'
import { yamlToFlowAsync, extractLayout } from './utils/yamlToFlow'
import { addStandaloneNode, addConnection, removeConnection, updateNode, orderYamlKeys } from './utils/flowToYaml'
import { fetchAgents, fetchAgent, saveAgent, deleteAgent, fetchTools, checkMcpDependencies, installMcpServer, getMcpStoreServer, installInlineMcpServer } from './api/agents'
import { snakeToTitleCase } from './utils/formatters'
import { Store, Lock, Copy, Loader2 } from 'lucide-react'
import './index.css'

// Default YAML for new agents
const defaultYaml = `description: New Agent

nodes: []

flow: []
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
  const [autoApprove, setAutoApprove] = useState(false)
  const [selectedNodeId, setSelectedNodeId] = useState(null)
  const [editingNode, setEditingNode] = useState(null)
  const [editingEdge, setEditingEdge] = useState(null)
  
  // UI State
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showAIChat, setShowAIChat] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [toast, setToast] = useState(null) // { message, type: 'success' | 'error' }
  
  // Flow State
  const [availableTools, setAvailableTools] = useState([])
  const [nodes, setNodes] = useState([])
  const [edges, setEdges] = useState([])

  // Refs
  const currentFlowNodesRef = useRef([])
  const currentFlowEdgesRef = useRef([])
  const abortControllerRef = useRef(null)
  const autoSaveTimerRef = useRef(null)  // Debounce timer for auto-save
  const layoutSaveTimerRef = useRef(null) // Debounce timer for layout changes
  
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
  
  // MCP Dependencies State
  const [mcpDependencies, setMcpDependencies] = useState(null) // {dependencies: [], all_installed: bool, missing: int}
  const [installingDep, setInstallingDep] = useState(null) // server name being installed
  const [installModalServer, setInstallModalServer] = useState(null)
  
  // Undo/Redo History (max 100 versions)
  const [yamlHistory, setYamlHistory] = useState([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const MAX_HISTORY = 100

  // Derive showSettings from path
  const showSettings = path.view === 'settings'
  const settingsSection = path.params.section || 'general'

  // Extract available variables from all nodes' output_model and raw_tool_output, grouped by node
  const availableVariables = useMemo(() => {
    const grouped = []
    nodes.forEach(node => {
      const outputModel = node.data?.yaml?.output_model
      const rawToolOutput = node.data?.yaml?.raw_tool_output
      
      // Collect variables from both output_model and raw_tool_output
      const vars = new Set()
      
      if (outputModel && typeof outputModel === 'object') {
        Object.keys(outputModel).forEach(key => vars.add(key))
      }
      
      if (rawToolOutput && typeof rawToolOutput === 'object') {
        Object.keys(rawToolOutput).forEach(key => vars.add(key))
      }
      
      if (vars.size > 0) {
        grouped.push({
          nodeName: node.data?.label || node.id,
          nodeType: node.data?.nodeType || node.type,
          variables: Array.from(vars).sort()
        })
      }
    })
    return grouped
  }, [nodes])

  // Setup wizard state
  const [showSetupWizard, setShowSetupWizard] = useState(false)
  const [isCheckingSetup, setIsCheckingSetup] = useState(true)
  const [view, setView] = useState('home')

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

  // Auto-dismiss toast after 3 seconds
  useEffect(() => {
    if (toast) {
      const timer = setTimeout(() => {
        setToast(null)
      }, 3000)
      return () => clearTimeout(timer)
    }
  }, [toast])

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
        // Try to find the agent from URL - prioritize exact ID match
        const urlAgent = agentsList.find(a => a.id === urlAgentName) || agentsList.find(a => a.name === urlAgentName)
        if (urlAgent) {
          handleAgentSelectInternal(urlAgent, false) // Don't update URL, already there
          setView('canvas')
        } else if (agentsList.length > 0) {
          // Agent not found, select first and update URL
          handleAgentSelectInternal(agentsList[0], true)
          setView('canvas')
        }
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

  // Debounced auto-save to disk (1000ms delay) - sends YAML directly without reformatting
  const debouncedAutoSave = useCallback((newYaml) => {
    if (!selectedAgent) return
    
    // Skip saving for store flows (read-only)
    if (selectedAgent.source === 'store') {
      console.log('[Auto-save] Skipped - store flow is read-only')
      return
    }
    
    // Clear any pending save
    if (autoSaveTimerRef.current) {
      clearTimeout(autoSaveTimerRef.current)
    }
    
    // Schedule new save with longer debounce (1s) to avoid interrupting editing
    autoSaveTimerRef.current = setTimeout(async () => {
      try {
        // Save the user's YAML as-is (no reformatting)
        // Layout is saved separately via handleLayoutSave when canvas changes
        const result = await saveAgent(selectedAgent.id, newYaml)
        console.log('[Auto-save] Saved')
        
        // If server returned YAML with NEW content (like mcp_dependencies), update local state
        // Compare parsed content to avoid triggering on format-only differences
        if (result.yaml) {
          try {
            const serverParsed = yaml.load(result.yaml)
            const localParsed = yaml.load(newYaml)
            
            // Check if mcp_dependencies actually changed (the main reason server modifies YAML)
            const serverDeps = JSON.stringify(serverParsed?.mcp_dependencies || null)
            const localDeps = JSON.stringify(localParsed?.mcp_dependencies || null)
            
            if (serverDeps !== localDeps) {
              setYamlContent(result.yaml)
              console.log('[Auto-save] Updated with server-generated mcp_dependencies')
            }
          } catch (parseErr) {
            // If parse fails, fall back to not updating
            console.warn('[Auto-save] Could not compare YAML:', parseErr)
          }
        }
      } catch (err) {
        console.error('[Auto-save] Failed:', err)
      }
    }, 1000)
  }, [selectedAgent])

  // Unified function to update YAML - handles state, history, and auto-save
  const updateYaml = useCallback((newYaml, skipHistory = false) => {
    if (!newYaml) return
    setYamlContent(newYaml)
    if (!skipHistory) {
      pushToHistory(newYaml)
    }
    debouncedAutoSave(newYaml)
  }, [pushToHistory, debouncedAutoSave])

  // Undo: go back in history
  const handleUndo = useCallback(() => {
    if (historyIndex > 0) {
      const prevYaml = yamlHistory[historyIndex - 1]
      setHistoryIndex(prev => prev - 1)
      setYamlContent(prevYaml)
      debouncedAutoSave(prevYaml)  // Auto-save the undone state
    }
  }, [historyIndex, yamlHistory, debouncedAutoSave])

  // Redo: go forward in history
  const handleRedo = useCallback(() => {
    if (historyIndex < yamlHistory.length - 1) {
      const nextYaml = yamlHistory[historyIndex + 1]
      setHistoryIndex(prev => prev + 1)
      setYamlContent(nextYaml)
      debouncedAutoSave(nextYaml)  // Auto-save the redone state
    }
  }, [historyIndex, yamlHistory, debouncedAutoSave])

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
      // Prioritize exact ID match over name match
      const targetAgent = agents.find(a => a.id === path.params.agentName) || agents.find(a => a.name === path.params.agentName)
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
      
      // Check MCP dependencies
      try {
        const parsed = yaml.load(loadedYaml)
        if (parsed?.mcp_dependencies && parsed.mcp_dependencies.length > 0) {
          const depStatus = await checkMcpDependencies(parsed.mcp_dependencies)
          setMcpDependencies(depStatus)
        } else {
          setMcpDependencies(null)
        }
      } catch (depErr) {
        console.error('Failed to check MCP dependencies:', depErr)
        setMcpDependencies(null)
      }
    } catch (err) {
      console.error('Failed to load agent:', err)
      setYamlContent(defaultYaml)
      setYamlHistory([defaultYaml])
      setHistoryIndex(0)
      setMcpDependencies(null)
    }
  }, [navigate])

  // Public agent select (always updates URL)
  const handleAgentSelect = useCallback(async (agent) => {
    await handleAgentSelectInternal(agent, true)
    setView('canvas')
  }, [handleAgentSelectInternal])

  const handleCreateNew = useCallback(() => {
    setShowCreateModal(true)
  }, [])

  const handleCreateAgent = useCallback(async ({ id, name, description }) => {
    const newYaml = `description: ${description || name}

nodes: []

flow: []

layout:
  nodes:
    START:
      x: 200
      y: 50
    END:
      x: 200
      y: 250
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
    
    // Save immediately so it appears in the left menu
    try {
      await saveAgent(id, newYaml)
      await loadAgents()
      console.log('New agent saved and appears in menu')
    } catch (err) {
      console.error('Failed to save new agent:', err)
    }
  }, [navigate, loadAgents])

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
          provider: defaultProvider,
          model: defaultModel,
          autoApprove: autoApprove
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
                    if (last && last.type === 'agent' && !data.preserveWhitespace && !last.preserveWhitespace) {
                      // Only append if both are streaming (not output node)
                      return [...prev.slice(0, -1), { ...last, content: last.content + data.text }]
                    }
                    return [...prev, { type: 'agent', content: data.text, preserveWhitespace: data.preserveWhitespace || false }]
                  })
                } else if (data.node) {
                  setRunningNodeId(data.node)
                  // Only add node message to chat if not silent
                  if (!data.silent) {
                    setChatMessages(prev => [...prev, { type: 'node', nodeName: data.node }])
                  }
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
                } else if (data.attempt !== undefined && data.maxRetries !== undefined) {
                  // Handle retry events
                  setChatMessages(prev => [...prev, { 
                    type: 'retry', 
                    attempt: data.attempt,
                    maxRetries: data.maxRetries,
                    reason: data.reason
                  }])
                } else if (data.title && data.reason && data.originalError !== undefined) {
                  // Handle error_info events (smart error handling)
                  setChatMessages(prev => [...prev, { 
                    type: 'error_info', 
                    title: data.title,
                    reason: data.reason,
                    suggestion: data.suggestion,
                    originalError: data.originalError
                  }])
                } else if (data.auto_approved && data.approval_tool) {
                  // Handle tool auto-approval notification
                  setChatMessages(prev => [...prev, { 
                    type: 'tool_auto_approved', 
                    toolName: data.approval_tool
                  }])
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
  }, [selectedAgent, defaultProvider, defaultModel, autoApprove])

  const handleRun = useCallback(() => {
    setIsRunning(true)
    setEditingNode(null)
    setShowAIChat(false) // Close AI Assistant when opening Run dialog
    setChatMessages([]) // Clear history
    setRunningNodeId(null)
    // Don't auto-start, wait for user to click Start
  }, [])


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
    // Cleanup MCP on server side
    if (sessionId) {
      fetch(`/api/session/${sessionId}/stop`, { method: 'POST' }).catch(() => {})
    }
    setSessionId(null) // Clear session to re-enable auto-approve toggle
  }, [isWaitingForInput, sessionId])

  const handleExitRun = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    // Cleanup MCP on server side
    if (sessionId) {
      fetch(`/api/session/${sessionId}/stop`, { method: 'POST' }).catch(() => {})
    }
    setIsRunning(false)
    setRunningNodeId(null)
    setChatMessages([])
    setSessionId(null) // Clear session to re-enable auto-approve toggle
  }, [sessionId])

  // Keepalive: Ping server every 30 seconds while session is active to prevent timeout
  // This keeps the session and MCP servers alive while user is interacting with the flow
  useEffect(() => {
    if (!sessionId || !isRunning) return

    const keepaliveInterval = setInterval(() => {
      fetch(`/api/session/${sessionId}/keepalive`, { method: 'POST' })
        .catch(err => console.warn('Keepalive ping failed:', err))
    }, 30000) // 30 seconds

    return () => clearInterval(keepaliveInterval)
  }, [sessionId, isRunning])

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
      setEditingEdge(null) // Close edge editor if open
    }
  }, [nodes])

  // Click on edge to open edge editor
  const handleEdgeSelect = useCallback((edge) => {
    setEditingEdge(edge)
    setEditingNode(null) // Close node editor if open
  }, [])

  // Helper to merge current accumulated layout changes into YAML string
  const getYamlWithLayout = useCallback((baseYaml) => {
    try {
      if (!currentFlowNodesRef.current || currentFlowNodesRef.current.length === 0) return baseYaml
      
      const parsed = yaml.load(baseYaml) || {}
      const layout = extractLayout(currentFlowNodesRef.current, currentFlowEdgesRef.current)
      parsed.layout = layout
      
      return yaml.dump(orderYamlKeys(parsed), { 
        indent: 2,
        lineWidth: -1, 
        noRefs: true, 
        sortKeys: false,
        styles: { '!!str': 'literal' }  // Force block scalars for multiline strings
      })
    } catch (e) {
      console.error('Failed to merge layout:', e)
      return baseYaml
    }
  }, [])

  // Add standalone node
  const handleAddNode = useCallback((nodeType) => {
    // First merge current layout so positions are preserved
    const currentYaml = getYamlWithLayout(yamlContent)
    const newYaml = addStandaloneNode(currentYaml, nodeType)
    updateYaml(newYaml)
  }, [yamlContent, updateYaml, getYamlWithLayout])

  // Create a new connected node from the + button on an existing node
  const handleCreateConnectedNode = useCallback((sourceId, nodeType, position) => {
    // First add the standalone node
    const currentYaml = getYamlWithLayout(yamlContent)
    let newYaml = addStandaloneNode(currentYaml, nodeType)
    
    // Parse the new YAML to get the new node's name
    const parsedYaml = yaml.load(newYaml)
    const newNodeName = parsedYaml?.nodes?.[parsedYaml.nodes.length - 1]?.name
    
    if (newNodeName) {
      // Update position for the new node
      if (position && parsedYaml.layout?.nodes) {
        parsedYaml.layout.nodes[newNodeName] = {
          x: Math.round(position.x),
          y: Math.round(position.y)
        }
        newYaml = yaml.dump(orderYamlKeys(parsedYaml), { 
          lineWidth: -1, noRefs: true, quotingType: '"', forceQuotes: false 
        })
      }
      
      // Add the connection from source to new node
      newYaml = addConnection(newYaml, sourceId, newNodeName)
    }
    
    updateYaml(newYaml)
  }, [yamlContent, updateYaml, getYamlWithLayout])

  // Handle new connection
  const handleConnect = useCallback((sourceId, targetId) => {
    const currentYaml = getYamlWithLayout(yamlContent)
    const newYaml = addConnection(currentYaml, sourceId, targetId)
    updateYaml(newYaml)
  }, [yamlContent, updateYaml, getYamlWithLayout])

  // Handle edge removal
  const handleEdgeRemove = useCallback((sourceId, targetId) => {
    const currentYaml = getYamlWithLayout(yamlContent)
    const newYaml = removeConnection(currentYaml, sourceId, targetId)
    updateYaml(newYaml)
  }, [yamlContent, updateYaml, getYamlWithLayout])

  // Save node edits (called from NodeEditor on every change)
  const handleNodeSave = useCallback((nodeId, newData) => {
    // Optimization: Skip update if data hasn't changed (prevents undo history spam)
    if (editingNode && editingNode.id === nodeId && editingNode.data?.yaml) {
      // Simple deep comparison to see if logic actually changed
      if (JSON.stringify(editingNode.data.yaml) === JSON.stringify(newData)) {
        return
      }
    }

    const newYaml = updateNode(yamlContent, nodeId, newData)
    updateYaml(newYaml)
    // Don't close editor here - it auto-saves continuously, user closes via Done button
  }, [yamlContent, updateYaml, editingNode])

  // Close node editor
  const handleNodeEditorClose = useCallback(() => {
    setEditingNode(null)
  }, [])

  // Save edge condition
  const handleEdgeSave = useCallback((edgeId, { condition }) => {
    // Get source and target from the editing edge (which was passed full edge object)
    const edge = editingEdge
    if (!edge) return
    
    const sourceId = edge.source
    const targetId = edge.target
    
    try {
      const parsed = yaml.load(yamlContent) || {}
      const flow = parsed.flow || []
      
      // Find and update the flow entry
      let updated = false
      for (let i = 0; i < flow.length; i++) {
        const entry = flow[i]
        if (entry.from === sourceId) {
          // Check if this is a simple edge or has edges array
          if (entry.to === targetId) {
            // Simple edge - convert to edges array if condition is set
            if (condition) {
              delete entry.to
              entry.edges = [{ to: targetId, condition }]
            }
            updated = true
            break
          } else if (entry.edges) {
            // Has edges array - find and update specific edge
            for (let j = 0; j < entry.edges.length; j++) {
              if (entry.edges[j].to === targetId) {
                if (condition) {
                  entry.edges[j].condition = condition
                } else {
                  delete entry.edges[j].condition
                }
                updated = true
                break
              }
            }
          }
        }
      }
      
      if (updated) {
        const newYaml = yaml.dump(orderYamlKeys(parsed), { 
          indent: 2,
          lineWidth: -1, 
          noRefs: true, 
          sortKeys: false 
        })
        updateYaml(newYaml)
        setEditingEdge(null)
      }
    } catch (e) {
      console.error('Failed to save edge condition:', e)
    }
  }, [yamlContent, updateYaml, editingEdge])

  // Delete edge
  const handleEdgeDelete = useCallback((edgeId) => {
    // Get source and target from the editing edge
    const edge = editingEdge
    if (!edge) return
    
    handleEdgeRemove(edge.source, edge.target)
    setEditingEdge(null)
  }, [handleEdgeRemove, editingEdge])

  // Close edge editor
  const handleEdgeEditorClose = useCallback(() => {
    setEditingEdge(null)
  }, [])

  // Track layout changes from FlowCanvas
  // Track layout changes from FlowCanvas (keep refs in sync during drag)
  const handleLayoutChange = useCallback((flowNodes, flowEdges) => {
    currentFlowNodesRef.current = flowNodes
    currentFlowEdgesRef.current = flowEdges
  }, [])

  // Handle immediate layout save (called on node drag stop)
  const handleLayoutSave = useCallback((flowNodes, flowEdges) => {
    // Ensure refs are up to date
    currentFlowNodesRef.current = flowNodes
    currentFlowEdgesRef.current = flowEdges
    
    // Save immediately without history
    const newYaml = getYamlWithLayout(yamlContent)
    if (newYaml !== yamlContent) {
      updateYaml(newYaml, true)
      console.log('[Layout] Saved positions on drag stop')
    }
  }, [yamlContent, getYamlWithLayout, updateYaml])

  // Handle node deletion from FlowCanvas - update YAML immediately
  // Accepts an array of node IDs to delete (supports multi-select)
  const handleNodeDelete = useCallback((nodeIds) => {
    // Normalize to array if single ID passed
    const idsToDelete = Array.isArray(nodeIds) ? nodeIds : [nodeIds]
    console.log(`[NODE DELETE] Removing nodes: ${idsToDelete.join(', ')}`)
    
    // Parse current YAML and remove all nodes at once
    try {
      const parsed = yaml.load(yamlContent) || {}
      
      if (parsed.nodes && Array.isArray(parsed.nodes)) {
        parsed.nodes = parsed.nodes.filter(n => !idsToDelete.includes(n.name))
      }
      
      // Also clean up flow edges that reference any deleted nodes
      if (parsed.flow && Array.isArray(parsed.flow)) {
        parsed.flow = parsed.flow.filter(edge => {
          if (idsToDelete.includes(edge.from)) return false
          if (idsToDelete.includes(edge.to)) return false
          // Handle conditional edges
          if (edge.edges && Array.isArray(edge.edges)) {
            edge.edges = edge.edges.filter(e => !idsToDelete.includes(e.to))
            return edge.edges.length > 0 || edge.to
          }
          return true
        })
      }
      
      // Clean up layout for all deleted nodes
      if (parsed.layout && parsed.layout.nodes) {
        idsToDelete.forEach(nodeId => {
          delete parsed.layout.nodes[nodeId]
        })
      }
      
      const newYaml = yaml.dump(orderYamlKeys(parsed), { 
        indent: 2, 
        lineWidth: -1,
        noRefs: true,
        sortKeys: false
      })
      
      updateYaml(newYaml)
      
      // Also update local nodes/edges state to keep in sync
      setNodes(prev => prev.filter(n => !idsToDelete.includes(n.id)))
      setEdges(prev => prev.filter(e => !idsToDelete.includes(e.source) && !idsToDelete.includes(e.target)))
      
    } catch (err) {
      console.error('Failed to delete nodes from YAML:', err)
    }
  }, [yamlContent, updateYaml])

  // Handle node duplication (copy/paste)
  // Accepts an array of node data to duplicate with new IDs
  const handleDuplicateNodes = useCallback((nodesToDuplicate) => {
    if (!nodesToDuplicate || nodesToDuplicate.length === 0) return
    
    console.log(`[DUPLICATE] Duplicating ${nodesToDuplicate.length} node(s)`)
    
    try {
      const parsed = yaml.load(yamlContent) || {}
      if (!parsed.nodes) parsed.nodes = []
      if (!parsed.layout) parsed.layout = { nodes: {} }
      if (!parsed.layout.nodes) parsed.layout.nodes = {}
      
      // Get existing node names to avoid conflicts
      const existingNames = new Set(parsed.nodes.map(n => n.name))
      
      // Track new node names for selection after paste
      const newNodeNames = []
      
      nodesToDuplicate.forEach((sourceNode, index) => {
        // Find the original node data in YAML
        const originalNode = parsed.nodes.find(n => n.name === sourceNode.id)
        if (!originalNode) {
          console.log(`[DUPLICATE] Original node not found: ${sourceNode.id}`)
          return
        }
        
        // Generate unique name
        let copyNum = 1
        let newName = `${originalNode.name}_copy`
        while (existingNames.has(newName)) {
          copyNum++
          newName = `${originalNode.name}_copy${copyNum}`
        }
        existingNames.add(newName)
        newNodeNames.push(newName)
        
        // Clone the node with new name
        const newNode = { ...originalNode, name: newName }
        parsed.nodes.push(newNode)
        
        // Set position (offset from original or from source position)
        const sourcePos = sourceNode.position || parsed.layout.nodes[sourceNode.id]
        if (sourcePos) {
          parsed.layout.nodes[newName] = {
            x: Math.round(sourcePos.x + 50 + index * 20),
            y: Math.round(sourcePos.y + 50 + index * 20)
          }
        }
      })
      
      const newYaml = yaml.dump(orderYamlKeys(parsed), { 
        indent: 2, 
        lineWidth: -1,
        noRefs: true,
        sortKeys: false
      })
      
      updateYaml(newYaml)
      console.log(`[DUPLICATE] Created ${newNodeNames.length} new node(s): ${newNodeNames.join(', ')}`)
      
    } catch (err) {
      console.error('Failed to duplicate nodes:', err)
    }
  }, [yamlContent, updateYaml])

  // Save layout and sync to disk (used before running and periodically)
  const saveLayoutAndSync = useCallback(async () => {
    if (!selectedAgent || !yamlContent) return
    // Skip saving for store flows (read-only)
    if (selectedAgent.source === 'store') return
    
    try {
      const parsed = yaml.load(yamlContent) || {}
      const layout = extractLayout(currentFlowNodesRef.current, currentFlowEdgesRef.current)
      parsed.layout = layout
      const updatedYaml = yaml.dump(orderYamlKeys(parsed), { 
        indent: 2,
        lineWidth: -1, 
        noRefs: true, 
        sortKeys: false 
      })
      await saveAgent(selectedAgent.id, updatedYaml)
      setYamlContent(updatedYaml)
    } catch (err) {
      console.error('Layout save failed:', err)
    }
  }, [selectedAgent, yamlContent])

  // Handle auto layout - remove stored positions to let ELK recalculate
  const handleAutoLayout = useCallback(() => {
    try {
      const parsed = yaml.load(yamlContent) || {}
      
      // Remove the entire layout section
      if (parsed.layout) {
        delete parsed.layout
      }
      
      const newYaml = yaml.dump(orderYamlKeys(parsed), { 
        indent: 2, 
        lineWidth: -1,
        noRefs: true,
        sortKeys: false
      })
      
      updateYaml(newYaml)
      
      // Also save to disk
      if (selectedAgent && selectedAgent.source !== 'store') {
        saveAgent(selectedAgent.id, newYaml).catch(err => {
          console.error('Failed to save after auto layout:', err)
        })
      }
    } catch (err) {
      console.error('Failed to apply auto layout:', err)
    }
  }, [yamlContent, updateYaml, selectedAgent])

  const handleStartRun = useCallback(async () => {
    // Save layout before running
    await saveLayoutAndSync()
    
    setChatMessages([{ type: 'system', content: 'Execution started...' }])
    setRunningNodeId(null)
    const newSessionId = `session-${Date.now()}`
    setSessionId(newSessionId)
    connectToChat(newSessionId)
  }, [connectToChat, saveLayoutAndSync])

  // Delete agent
  const handleDeleteAgent = useCallback((agent) => {
    setDeleteTarget(agent)
  }, [])

  // Toast notification helper
  const showToast = useCallback((message, type = 'success') => {
    setToast({ message, type })
    setTimeout(() => setToast(null), 4000) // Auto-dismiss after 4 seconds
  }, [])

  // Copy store agent to local
  const handleCopyToLocal = useCallback(async (agent) => {
    try {
      const res = await fetch(`/api/agents/${encodeURIComponent(agent.id)}/copy-to-local`, {
        method: 'POST'
      })
      if (!res.ok) {
        const errorText = await res.text()
        throw new Error(errorText || 'Failed to copy agent')
      }
      const data = await res.json()
      showToast(`Flow copied to local: ${data.newName}`, 'success')
      // Refresh agent list
      loadAgents()
    } catch (err) {
      console.error('Failed to copy agent:', err)
      showToast('Failed to copy: ' + err.message, 'error')
    }
  }, [loadAgents, showToast])

  const confirmDelete = useCallback(async () => {
    if (!deleteTarget) return
    
    try {
      if (deleteTarget.source === 'store') {
        // Uninstall store flow - parse the store:tap:name ID format
        const parts = deleteTarget.id.split(':')
        if (parts.length === 3) {
          const [, tapName, flowName] = parts
          const res = await fetch(`/api/flow-store/${encodeURIComponent(tapName)}/${encodeURIComponent(flowName)}`, {
            method: 'DELETE'
          })
          if (!res.ok) {
            const errorText = await res.text()
            throw new Error(errorText || 'Failed to uninstall flow')
          }
        } else {
          throw new Error('Invalid store flow ID format')
        }
      } else {
        // Delete local agent
        await deleteAgent(deleteTarget.id)
      }
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
  }, [deleteTarget, selectedAgent, loadAgents])

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
          currentView={view}
          onNavigate={setView}
        />

        {/* Main Content Area */}
        <div className="flex flex-1 overflow-hidden">
          {/* Sidebar - Show only in Canvas View */}
          {view === 'canvas' && (
            <Sidebar
              agents={agents}
              selectedAgent={selectedAgent}
              onAgentSelect={handleAgentSelect}
              onCreateNew={handleCreateNew}
              onDeleteAgent={handleDeleteAgent}
              onCopyToLocal={handleCopyToLocal}
              isLoading={isLoadingAgents}
            />
          )}

        {/* Main Content */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {view === 'home' ? (
            <HomePage
              onCreateAgent={handleCreateNew}
              onOpenSettings={() => navigate(buildPath('settings', { section: 'general' }))}
              onOpenMCP={() => navigate(buildPath('settings', { section: 'mcp' }))}
              onOpenRepositories={() => navigate(buildPath('settings', { section: 'taps' }))}
              onOpenFlowStore={() => navigate(buildPath('settings', { section: 'flows' }))}
              onBrowseFlows={() => setView('canvas')}
              defaultProvider={defaultProvider}
              defaultModel={defaultModel}
              theme={theme}
            />
          ) : !selectedAgent ? (
             <div className="flex-1 flex items-center justify-center p-8 text-center" style={{ color: 'var(--text-muted)' }}>
               Select a flow from the sidebar to continue
             </div>
          ) : (

          <>
          <Header
            agentName={selectedAgent?.name || 'Select Agent'}
            agentSource={selectedAgent?.source}
            agentTapName={selectedAgent?.tapName}
            showYaml={showYaml}
            onToggleYaml={() => setShowYaml(!showYaml)}
            isRunning={isRunning}
            onRun={handleRun}
            onStop={handleStopRun}
            onExit={handleExitRun}
            theme={theme}
            canUndo={historyIndex > 0}
            canRedo={historyIndex < yamlHistory.length - 1}
            onUndo={handleUndo}
            onRedo={handleRedo}
            readOnly={selectedAgent?.source === 'store'}
            onCopyToLocal={() => handleCopyToLocal(selectedAgent)}
          />

          {/* MCP Dependencies Warning Panel */}
          {mcpDependencies && !mcpDependencies.all_installed && (
            <MCPDependenciesPanel
              dependencies={mcpDependencies}
              onDismiss={() => setMcpDependencies(null)}
              isInstalling={installingDep}
              onInstall={async (dep) => {
                // Handle inline dependencies (embedded config in YAML)
                if (dep.source === 'inline') {
                  if (!dep.config || (!dep.config.command && !dep.config.url)) {
                    setToast({ message: `Cannot install ${dep.server}: Missing configuration. Please add it manually in Settings > MCP.`, type: 'error' })
                    return
                  }
                  
                  setInstallingDep(dep.server)
                  try {
                    // For inline, show modal with the embedded config for env var input
                    if (dep.config.env && Object.keys(dep.config.env).length > 0) {
                      setInstallModalServer({
                        name: dep.server,
                        description: `Inline MCP server required by this flow`,
                        config: dep.config,
                        _depContext: dep,
                        _isInline: true
                      })
                      setInstallingDep(null)
                      return
                    }
                    
                    // No env vars needed, install directly
                    await installInlineMcpServer(dep.server, dep.config)
                    
                    // Refresh tools and check dependencies
                    const toolsData = await fetchTools()
                    setAvailableTools(toolsData.tools || [])
                    
                    const parsed = yaml.load(yamlContent)
                    if (parsed?.mcp_dependencies) {
                      const newStatus = await checkMcpDependencies(parsed.mcp_dependencies)
                      setMcpDependencies(newStatus)
                    }
                    
                    setToast({ message: `Successfully installed ${dep.server}`, type: 'success' })
                  } catch (err) {
                    console.error('Failed to install inline server:', err)
                    setToast({ message: `Failed to install ${dep.server}: ${err.message}`, type: 'error' })
                  } finally {
                    setInstallingDep(null)
                  }
                  return
                }
                
                // Support both store and tap sources (both use the same install API)
                if (!dep.store_id || (dep.source !== 'store' && dep.source !== 'tap')) {
                  // For unknown sources
                  console.log('Unsupported install source:', dep)
                  setToast({ message: `Cannot install ${dep.server}: Unknown source type`, type: 'error' })
                  return
                }
                
                setInstallingDep(dep.server)
                try {
                  // Fetch server details first to check for env vars
                  const serverDetails = await getMcpStoreServer(dep.store_id)
                  
                  // If server requires env vars, show modal
                  if (serverDetails.config && serverDetails.config.env && Object.keys(serverDetails.config.env).length > 0) {
                    setInstallModalServer({
                      ...serverDetails,
                      _depContext: dep
                    })
                    setInstallingDep(null)
                    return
                  }
                  
                  // No env vars needed, proceed with install
                  await installMcpServer(dep.store_id)
                  
                  // Refresh tools and check dependencies
                  const toolsData = await fetchTools()
                  setAvailableTools(toolsData.tools || [])
                  
                  const parsed = yaml.load(yamlContent)
                  if (parsed?.mcp_dependencies) {
                    const newStatus = await checkMcpDependencies(parsed.mcp_dependencies)
                    setMcpDependencies(newStatus)
                  }
                  
                  setToast({ message: `Successfully installed ${dep.server}`, type: 'success' })
                } catch (err) {
                  console.error('Failed to install:', err)
                  setToast({ message: `Failed to install ${dep.server}: ${err.message}`, type: 'error' })
                } finally {
                  setInstallingDep(null)
                }
              }}
            />
          )}

          <InstallMcpModal 
            isOpen={!!installModalServer}
            server={installModalServer}
            onClose={() => setInstallModalServer(null)}
            onInstall={async (env) => {
              const dep = installModalServer._depContext
              const isInline = installModalServer._isInline
              try {
                if (isInline) {
                  // For inline servers, merge env vars with the config
                  const configWithEnv = {
                    ...installModalServer.config,
                    env: { ...installModalServer.config.env, ...env }
                  }
                  await installInlineMcpServer(dep.server, configWithEnv)
                } else {
                  // For store/tap servers
                  await installMcpServer(dep.store_id, env)
                }
                
                // Refresh tools and check dependencies
                const toolsData = await fetchTools()
                setAvailableTools(toolsData.tools || [])
                
                const parsed = yaml.load(yamlContent)
                if (parsed?.mcp_dependencies) {
                  const newStatus = await checkMcpDependencies(parsed.mcp_dependencies)
                  setMcpDependencies(newStatus)
                }
                
                setToast({ message: `Successfully installed ${dep.server}`, type: 'success' })
              } catch (err) {
                console.error('Failed to install with env:', err)
                setToast({ message: `Failed to install ${dep.server}: ${err.message}`, type: 'error' })
                throw err // InstallMcpModal will handle error display
              }
            }}
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
                readOnly={selectedAgent?.source === 'store'}
                theme={theme}
                onNodeSelect={handleNodeSelect}
                onNodeDoubleClick={handleNodeDoubleClick}
                onEdgeSelect={handleEdgeSelect}
                selectedNodeId={selectedNodeId}
                runningNodeId={runningNodeId}
                onAddNode={handleAddNode}
                onConnect={handleConnect}
                onEdgeRemove={handleEdgeRemove}
                onLayoutChange={handleLayoutChange}
                onLayoutSave={handleLayoutSave}
                onNodeDelete={handleNodeDelete}
                onAutoLayout={handleAutoLayout}
                onCreateConnectedNode={handleCreateConnectedNode}
                onDuplicateNodes={handleDuplicateNodes}
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
                  autoApprove={autoApprove}
                  onToggleAutoApprove={setAutoApprove}
                />
              </div>
            )}
          </div>

          {/* Bottom Panel - Node Editor OR Edge Editor OR YAML Drawer */}
          {!isRunning && (editingNode || editingEdge || showYaml) && (
            <div className="h-1/2" style={{ borderTop: '1px solid var(--border-color)' }}>
              {editingNode ? (
                <NodeEditor
                  node={editingNode}
                  onSave={handleNodeSave}
                  onClose={handleNodeEditorClose}
                  theme={theme}
                  availableTools={availableTools}
                  availableVariables={availableVariables}
                  readOnly={selectedAgent?.source === 'store'}
                  onAIAssist={(node, nodeName, nodeData) => {
                    setAIChatContext('node_config')
                    setAIFocusedNode({ name: nodeName, type: node.data?.nodeType || node.type, data: nodeData })
                    setShowAIChat(true)
                  }}
                />
              ) : editingEdge ? (
                <EdgeEditor
                  edge={editingEdge}
                  sourceNode={nodes.find(n => n.id === editingEdge.source)}
                  targetNode={nodes.find(n => n.id === editingEdge.target)}
                  onSave={handleEdgeSave}
                  onDelete={handleEdgeDelete}
                  onClose={handleEdgeEditorClose}
                  theme={theme}
                  availableVariables={availableVariables}
                  readOnly={selectedAgent?.source === 'store'}
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
        isStoreFlow={deleteTarget?.source === 'store'}
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
        tools={availableTools}
        onToolsRefresh={loadTools}
        onApplyYaml={(newYaml) => {
          // Apply the new YAML
          setYamlContent(newYaml)
          // Push new state to history (for undo)
          pushToHistory(newYaml)
          // Auto-save after applying (skip for store flows)
          if (selectedAgent && selectedAgent.source !== 'store') {
            saveAgent(selectedAgent.id, newYaml).then(() => {
              console.log('Auto-saved after AI changes')
            }).catch(err => {
              console.error('Failed to auto-save:', err)
            })
          }
        }}
      />

      {/* AI Chat Toggle Button - hidden for store flows (read-only) */}
      {selectedAgent && !isRunning && selectedAgent.source !== 'store' && (
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
          onSettingsSaved={loadSettings}
        />
      )}

      {/* Toast Notification */}
      {toast && (
        <div 
          className="fixed bottom-6 right-6 z-[100] animate-slide-up"
          style={{ animation: 'slide-up 0.3s ease-out' }}
        >
          <div 
            className={`flex items-center gap-3 px-5 py-3 rounded-xl shadow-2xl backdrop-blur-sm ${
              toast.type === 'error' 
                ? 'bg-red-500/90 text-white' 
                : 'bg-gradient-to-r from-emerald-500/90 to-teal-500/90 text-white'
            }`}
            style={{ minWidth: '280px' }}
          >
            {toast.type === 'error' ? (
              <svg className="w-5 h-5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            ) : (
              <svg className="w-5 h-5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            )}
            <span className="text-sm font-medium">{toast.message}</span>
            <button 
              onClick={() => setToast(null)}
              className="ml-auto p-1 hover:bg-white/20 rounded-lg transition-colors"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>
      )}
    </ReactFlowProvider>
  )
}

export default App
