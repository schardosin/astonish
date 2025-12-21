import { useState, useRef, useEffect } from 'react'
import { MessageSquare, Send, X, Sparkles, Loader2, Check, Eye, Maximize2, Minimize2, Download, Package, Settings } from 'lucide-react'

// API function to chat with AI
async function sendChatMessage(message, context, currentYaml, selectedNodes, history) {
  const response = await fetch('/api/ai/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      message,
      context,
      currentYaml,
      selectedNodes,
      history,
    }),
  })
  return response.json()
}

// API function to search for tools in the store using AI semantic search
async function searchToolsInStore(requirement) {
  const response = await fetch('/api/ai/tool-search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requirement }),
  })
  return response.json()
}

// API function to install a tool from the store
async function installToolFromStore(toolId, serverName, env = {}) {
  const response = await fetch(`/api/mcp-store/${encodeURIComponent(toolId)}/install`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverName, env }),
  })
  return response.json()
}

// Detect if AI response offers to help find tools (stores pending requirement)
// Returns the tool requirement if AI is offering help, null otherwise
function detectToolOfferFromAI(text) {
  const lower = text.toLowerCase()
  
  // Check if AI is offering to help find/install tools
  const offerPatterns = [
    'would you like me to help you find',
    'would you like me to help you install',
    'would you like me to find and install',
    'shall i search for',
    'shall i help you find',
    'want me to search',
    'want me to find',
  ]
  
  // Check if AI mentions missing tools AND offers help
  const missingPatterns = [
    'tools that are not currently installed',
    'tools that are not installed',
    'would need the following tools',
    'would need a tool for',
    'need a tool for',
    'not currently installed',
    'i would need',
  ]
  
  const isMissingMentioned = missingPatterns.some(p => lower.includes(p))
  const isOfferingHelp = offerPatterns.some(p => lower.includes(p))
  
  if (isMissingMentioned) {
    // Extract the tool requirement description
    const toolMatches = []
    
    // Pattern 1: Bullet points (- tool: for reason)
    const bulletPattern = /[-•*]\s*\*?\*?([^:\n*]{5,100})\*?\*?(?::\s*(?:for|to)\s+([^\n]+))?/gi
    let match
    while ((match = bulletPattern.exec(text)) !== null) {
      let desc = (match[1] + (match[2] ? ' ' + match[2] : '')).trim()
      const excludePatterns = ['would you', 'let me', 'i can', 'you can', 'please', 'strictly following']
      if (!excludePatterns.some(p => desc.toLowerCase().includes(p)) && desc.length > 5) {
        toolMatches.push(desc)
      }
    }
    
    // Pattern 2: Inline mentions like "I would need a tool for X"
    if (toolMatches.length === 0) {
      const inlinePatterns = [
        /(?:would need|need)\s+(?:a\s+)?tool\s+for\s+([^.(]+)/gi,
        /(?:would need|need)\s+(?:a\s+)?([^.]+?)\s+tool/gi,
        /tool\s+(?:for\s+)?(?:general\s+)?([^.(,]+?)(?:\s+that|\s+is|\.|,)/gi,
        /`([a-z-]+(?:-[a-z]+)*)`\s*(?:or similar|tool)?/gi,  // backtick tool names like `tavily-search`
      ]
      
      for (const pattern of inlinePatterns) {
        let inlineMatch
        while ((inlineMatch = pattern.exec(text)) !== null) {
          let desc = inlineMatch[1].trim()
          // Clean up and validate
          if (desc.length > 3 && desc.length < 100 && !desc.includes('available tools')) {
            toolMatches.push(desc)
          }
        }
      }
    }
    
    if (toolMatches.length > 0) {
      // Deduplicate and join
      const unique = [...new Set(toolMatches.map(t => t.toLowerCase()))]
      return {
        requirement: unique.join('; '),
        isAskingUser: isOfferingHelp
      }
    }
  }
  
  return null
}

// Detect if user is confirming they want help finding tools
// Now also detects late requests like "search for it" even without pendingToolRequirement
function isUserConfirmingToolSearch(text) {
  const lower = text.toLowerCase().trim()
  const confirmPatterns = [
    'yes', 'yep', 'yeah', 'sure', 'ok', 'okay', 'please', 
    'go ahead', 'do it', 'yes please', 'find them', 'search for them',
    'install them', 'help me find', 'yes, help',
    // Late requests - user changed their mind
    'search for it', 'find it', 'check the store', 'check store', 
    'look for it', 'search the store', 'search store', 'install it',
    'changed my mind', 'i changed my mind', 'do it now', 'let\'s do it'
  ]
  return confirmPatterns.some(p => lower.startsWith(p) || lower === p || lower.includes(p))
}

// Component for a tool install card with optional env var configuration
function ToolInstallCard({ tool, installingTool, onInstall }) {
  const [envValues, setEnvValues] = useState({})
  const [showConfig, setShowConfig] = useState(false)
  
  // Check if tool requires configuration
  const hasEnvVars = tool.envVars && Object.keys(tool.envVars).length > 0
  const needsConfig = hasEnvVars || tool.requiresApiKey
  
  // Format env var name for display (e.g., TAVILY_API_KEY -> "Tavily API Key")
  const formatEnvName = (envName) => {
    return envName
      .replace(/_/g, ' ')
      .toLowerCase()
      .replace(/\b\w/g, c => c.toUpperCase())
  }
  
  const handleInstallClick = () => {
    if (needsConfig && !showConfig) {
      // First click: show config inputs
      setShowConfig(true)
    } else {
      // Install with collected env values
      onInstall({ ...tool, collectedEnv: envValues })
    }
  }
  
  const handleEnvChange = (key, value) => {
    setEnvValues(prev => ({ ...prev, [key]: value }))
  }
  
  const isInstalling = installingTool === tool.id
  
  // Check if all required env vars are filled
  const allEnvFilled = !hasEnvVars || Object.keys(tool.envVars).every(key => envValues[key]?.trim())
  
  return (
    <div className="bg-[var(--bg-primary)]/50 rounded-lg p-3 space-y-2">
      {/* Tool info header */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex-1 min-w-0">
          <div className="font-medium text-sm text-[var(--text-primary)]">
            {tool.name}
            {needsConfig && (
              <span className="ml-2 text-xs text-yellow-400">⚙️ Config required</span>
            )}
          </div>
          <div className="text-xs text-[var(--text-secondary)] mt-0.5">
            {tool.description}
          </div>
          <div className="text-xs text-purple-400 mt-0.5">
            Source: {tool.source}
          </div>
        </div>
        
        {!showConfig && (
          <button
            onClick={handleInstallClick}
            disabled={isInstalling}
            className="flex items-center gap-1 px-3 py-1.5 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 text-white text-xs font-medium rounded-lg transition-colors whitespace-nowrap"
          >
            {isInstalling ? (
              <>
                <Loader2 size={12} className="animate-spin" />
                Installing...
              </>
            ) : (
              <>
                <Download size={12} />
                Install
              </>
            )}
          </button>
        )}
      </div>
      
      {/* Config inputs - shown when tool needs config and user clicked Configure */}
      {showConfig && hasEnvVars && (
        <div className="border-t border-white/10 pt-2 space-y-2">
          <div className="text-xs text-[var(--text-muted)]">
            Enter the required configuration:
          </div>
          {Object.entries(tool.envVars).map(([key, placeholder]) => (
            <div key={key} className="flex flex-col gap-1">
              <label className="text-xs text-[var(--text-secondary)]">
                {formatEnvName(key)}
              </label>
              <input
                type={key.toLowerCase().includes('key') || key.toLowerCase().includes('token') || key.toLowerCase().includes('password') || key.toLowerCase().includes('secret') ? 'password' : 'text'}
                value={envValues[key] || ''}
                onChange={(e) => handleEnvChange(key, e.target.value)}
                placeholder={placeholder || `Enter ${formatEnvName(key)}`}
                className="px-2 py-1.5 bg-[var(--bg-secondary)] border border-[var(--border-color)] rounded text-xs text-[var(--text-primary)] placeholder:text-[var(--text-muted)]"
              />
            </div>
          ))}
          
          <div className="flex gap-2 pt-1">
            <button
              onClick={() => setShowConfig(false)}
              className="flex-1 px-3 py-1.5 bg-[var(--bg-secondary)] hover:bg-[var(--bg-tertiary)] text-[var(--text-secondary)] text-xs font-medium rounded-lg transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleInstallClick}
              disabled={isInstalling || !allEnvFilled}
              className="flex-1 flex items-center justify-center gap-1 px-3 py-1.5 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 text-white text-xs font-medium rounded-lg transition-colors"
            >
              {isInstalling ? (
                <>
                  <Loader2 size={12} className="animate-spin" />
                  Installing...
                </>
              ) : (
                <>
                  <Download size={12} />
                  Install
                </>
              )}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// Component for the store results panel with expandable list
function StoreResultsPanel({ storeResults, installingTool, onInstall }) {
  const [isExpanded, setIsExpanded] = useState(false)
  const INITIAL_COUNT = 5
  
  const displayedTools = isExpanded ? storeResults : storeResults.slice(0, INITIAL_COUNT)
  const hasMore = storeResults.length > INITIAL_COUNT
  
  return (
    <div className="bg-gradient-to-r from-purple-600/20 to-blue-600/20 border border-purple-500/30 rounded-lg p-3 space-y-3">
      <div className="flex items-center gap-2 text-sm font-medium text-purple-300">
        <Package size={16} />
        <span>Found {storeResults.length} matching tools in store:</span>
      </div>
      <div className="space-y-3">
        {displayedTools.map((tool) => (
          <ToolInstallCard 
            key={tool.id}
            tool={tool}
            installingTool={installingTool}
            onInstall={onInstall}
          />
        ))}
      </div>
      {hasMore && (
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="w-full text-xs text-purple-400 hover:text-purple-300 transition-colors py-1"
        >
          {isExpanded ? (
            <>▲ Show less</>
          ) : (
            <>▼ Show {storeResults.length - INITIAL_COUNT} more tools</>
          )}
        </button>
      )}
    </div>
  )
}

export default function AIChatPanel({ 
  isOpen, 
  onClose, 
  context = 'create_flow',
  currentYaml = '',
  selectedNodes = [],
  focusedNode = null,
  agentId = null,
  onApplyYaml,
}) {
  // Separate message histories: flow chat preserves, node refiner resets
  const [flowMessages, setFlowMessages] = useState([])
  const [nodeMessages, setNodeMessages] = useState([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [pendingYaml, setPendingYaml] = useState(null)
  const [isExpanded, setIsExpanded] = useState(false)
  const [storeResults, setStoreResults] = useState(null) // Store search results
  const [installingTool, setInstallingTool] = useState(null) // Tool being installed
  const [pendingToolRequirement, setPendingToolRequirement] = useState(null) // Stores requirement when AI asks for confirmation
  const [originalRequest, setOriginalRequest] = useState(null) // Stores original user request for auto-continue after install
  const messagesEndRef = useRef(null)
  const inputRef = useRef(null)
  
  // Use the right message state based on context
  const isNodeContext = context === 'node_config' && focusedNode
  const messages = isNodeContext ? nodeMessages : flowMessages
  const setMessages = isNodeContext ? setNodeMessages : setFlowMessages

  // Scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, storeResults])

  // Focus input when panel opens
  useEffect(() => {
    if (isOpen) {
      inputRef.current?.focus()
    }
  }, [isOpen])

  // Reset flow chat when agent changes (context is different per agent)
  useEffect(() => {
    setFlowMessages([])
    setPendingYaml(null)
    setStoreResults(null)
  }, [agentId])

  // Reset node refiner when focused node changes (fresh start per node)
  useEffect(() => {
    if (focusedNode) {
      setNodeMessages([])
      setPendingYaml(null)
      setStoreResults(null)
    }
  }, [focusedNode])

  // Handle tool installation
  const handleInstallTool = async (tool) => {
    setInstallingTool(tool.id)
    try {
      // Pass collected env vars if any
      const envVars = tool.collectedEnv || {}
      const result = await installToolFromStore(tool.id, tool.name.toLowerCase().replace(/\s+/g, '-'), envVars)
      if (result.status === 'ok') {
        setMessages(prev => [...prev, { 
          role: 'system', 
          content: `✓ Installed ${tool.name}! ${result.toolsLoaded} tools loaded.`
        }])
        setStoreResults(null) // Clear results after successful install
        
        // Auto-continue: retry the original request now that tool is installed
        if (originalRequest) {
          setMessages(prev => [...prev, { 
            role: 'system', 
            content: '↻ Retrying your original request with the new tool...'
          }])
          
          // Small delay to let the message appear, then retry
          setTimeout(async () => {
            setIsLoading(true)
            try {
              const history = messages.map(m => ({ role: m.role, content: m.content }))
              const response = await sendChatMessage(
                originalRequest,
                context,
                currentYaml,
                selectedNodes,
                history
              )
              
              if (!response.error && response.proposedYaml && onApplyYaml) {
                setMessages(prev => [...prev, { 
                  role: 'assistant', 
                  content: response.message,
                  proposedYaml: response.proposedYaml,
                }])
                onApplyYaml(response.proposedYaml)
                setMessages(prev => [...prev, { 
                  role: 'system', 
                  content: '✓ Flow created! Use Undo (⌘Z) to revert if needed.' 
                }])
              } else if (response.message) {
                setMessages(prev => [...prev, { 
                  role: 'assistant', 
                  content: response.message,
                }])
              }
            } catch (err) {
              console.error('Auto-continue failed:', err)
            } finally {
              setIsLoading(false)
              setOriginalRequest(null)
            }
          }, 500)
        }
      } else {
        setMessages(prev => [...prev, { 
          role: 'system', 
          content: `⚠️ Installation issue: ${result.toolError || 'Unknown error'}`,
          isError: true
        }])
      }
    } catch (err) {
      setMessages(prev => [...prev, { 
        role: 'system', 
        content: `Error installing tool: ${err.message}`,
        isError: true
      }])
    } finally {
      setInstallingTool(null)
    }
  }

  const handleSend = async () => {
    if (!input.trim() || isLoading) return

    const userMessage = input.trim()
    setInput('')
    setMessages(prev => [...prev, { role: 'user', content: userMessage }])
    setIsLoading(true)
    setStoreResults(null) // Clear previous store results

    // Check if user is confirming a tool search (either with pending or late confirmation)
    if (isUserConfirmingToolSearch(userMessage)) {
      let requirement = pendingToolRequirement
      
      // If no pending requirement, try to extract from recent AI messages
      if (!requirement) {
        // Look through recent messages for tool offers
        for (let i = messages.length - 1; i >= 0 && i >= messages.length - 10; i--) {
          const msg = messages[i]
          if (msg.role === 'assistant') {
            const toolOffer = detectToolOfferFromAI(msg.content)
            if (toolOffer) {
              requirement = toolOffer.requirement
              // Also restore originalRequest from the first user message in conversation
              if (!originalRequest && messages.length > 0) {
                const firstUserMsg = messages.find(m => m.role === 'user')
                if (firstUserMsg) {
                  setOriginalRequest(firstUserMsg.content)
                }
              }
              break
            }
          }
        }
      }
      
      if (requirement) {
        // User confirmed - search for tools now
        try {
          const searchResult = await searchToolsInStore(requirement)
          if (searchResult.results && searchResult.results.length > 0) {
            setStoreResults(searchResult.results.filter(r => r.installable))
            setMessages(prev => [...prev, { 
              role: 'system', 
              content: `Found ${searchResult.results.length} matching tools! See options below.`
            }])
          } else {
            setMessages(prev => [...prev, { 
              role: 'system', 
              content: 'No matching tools found in the store.'
            }])
          }
        } catch (err) {
          console.error('Store search failed:', err)
          setMessages(prev => [...prev, { 
            role: 'system', 
            content: 'Failed to search the store.',
            isError: true
          }])
        } finally {
          setPendingToolRequirement(null) // Clear pending
          setIsLoading(false)
        }
        return // Don't send to AI, we handled it
      }
    }

    // Clear pending if user sends something else
    if (pendingToolRequirement) {
      setPendingToolRequirement(null)
    }

    try {
      // Build history for API (exclude current message)
      const history = messages.map(m => ({ role: m.role, content: m.content }))
      
      const response = await sendChatMessage(
        userMessage,
        context,
        currentYaml,
        selectedNodes,
        history
      )

      if (response.error) {
        setMessages(prev => [...prev, { 
          role: 'assistant', 
          content: `Error: ${response.error}`,
          isError: true 
        }])
      } else {
        setMessages(prev => [...prev, { 
          role: 'assistant', 
          content: response.message,
          proposedYaml: response.proposedYaml,
          action: response.action,
        }])
        
        // Check if AI mentioned missing tools
        const toolOffer = detectToolOfferFromAI(response.message)
        if (toolOffer) {
          // Store the original request so we can retry after tool installation
          setOriginalRequest(userMessage)
          
          if (toolOffer.isAskingUser) {
            // AI is asking user for confirmation - store the requirement for later
            setPendingToolRequirement(toolOffer.requirement)
            // Don't search yet, wait for user to say "yes"
          } else {
            // AI mentioned missing tools but didn't ask - auto-search
            try {
              const searchResult = await searchToolsInStore(toolOffer.requirement)
              if (searchResult.results && searchResult.results.length > 0) {
                setStoreResults(searchResult.results.filter(r => r.installable))
              }
            } catch (err) {
              console.error('Store search failed:', err)
            }
          }
        }
        
        // Auto-apply YAML changes when received
        if (response.proposedYaml && onApplyYaml) {
          onApplyYaml(response.proposedYaml)
          setMessages(prev => [...prev, { 
            role: 'system', 
            content: '✓ Changes applied! Use Undo (⌘Z) to revert if needed.' 
          }])
        }
      }
    } catch (err) {
      setMessages(prev => [...prev, { 
        role: 'assistant', 
        content: `Network error: ${err.message}`,
        isError: true 
      }])
    } finally {
      setIsLoading(false)
    }
  }

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  const getContextTitle = () => {
    switch (context) {
      case 'create_flow': return 'Create Flow'
      case 'modify_flow': return 'Modify Flow'
      case 'modify_nodes': return 'Modify Nodes'
      case 'node_config': return 'Node Assistant'
      case 'multi_node': return 'Multi-Node Assistant'
      default: return 'AI Assistant'
    }
  }

  const getPlaceholder = () => {
    switch (context) {
      case 'create_flow': return 'Describe the flow you want to create...'
      case 'modify_flow': return 'Describe changes to make to this flow...'
      case 'modify_nodes': return 'What changes do you want to make?'
      case 'node_config': return 'How can I help with this node?'
      case 'multi_node': return 'What would you like to do with these nodes?'
      default: return 'Ask me anything about your flow...'
    }
  }

  if (!isOpen) return null

  return (
    <div className={`fixed bottom-4 right-4 bg-[var(--bg-secondary)] border border-[var(--border-color)] rounded-lg shadow-2xl flex flex-col z-50 transition-all duration-200 ${isExpanded ? 'w-[600px] h-[80vh]' : 'w-96 h-[500px]'}`}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-[var(--border-color)] bg-gradient-to-r from-purple-600/20 to-blue-600/20">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <Sparkles size={18} className="text-purple-400 flex-shrink-0" />
          <span className="font-semibold text-[var(--text-primary)] truncate">{getContextTitle()}</span>
          {/* Show focused node badge */}
          {focusedNode && (
            <span className="ml-2 px-2 py-0.5 text-xs rounded bg-purple-600/30 text-purple-300 truncate">
              {focusedNode.name} ({focusedNode.type})
            </span>
          )}
          {/* Show selection badge */}
          {context === 'multi_node' && selectedNodes.length > 0 && (
            <span className="ml-2 px-2 py-0.5 text-xs rounded bg-blue-600/30 text-blue-300">
              {selectedNodes.length} nodes
            </span>
          )}
        </div>
        <div className="flex items-center gap-1 flex-shrink-0">
          <button 
            onClick={() => setIsExpanded(!isExpanded)}
            className="p-1 hover:bg-white/10 rounded transition-colors"
            title={isExpanded ? 'Collapse' : 'Expand'}
          >
            {isExpanded ? (
              <Minimize2 size={16} className="text-[var(--text-secondary)]" />
            ) : (
              <Maximize2 size={16} className="text-[var(--text-secondary)]" />
            )}
          </button>
          <button 
            onClick={onClose}
            className="p-1 hover:bg-white/10 rounded transition-colors"
          >
            <X size={18} className="text-[var(--text-secondary)]" />
          </button>
        </div>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.length === 0 && (
          <div className="text-center py-4">
            <Sparkles size={32} className="mx-auto mb-3 text-purple-400 opacity-50" />
            
            {context === 'node_config' && focusedNode ? (
              <>
                <p className="text-[var(--text-primary)] font-medium mb-1">Node Refiner</p>
                <p className="text-[var(--text-secondary)] text-sm mb-4">
                  Let me help you improve <span className="text-purple-400 font-medium">{focusedNode.name}</span>
                </p>
                
                {/* Node-specific suggestions */}
                <div className="text-left space-y-2">
                  <p className="text-xs text-[var(--text-muted)] mb-2">Try asking:</p>
                  {focusedNode.type === 'llm' && [
                    'Improve the system prompt',
                    'Make the prompt more concise',
                    'Add user_message to show output',
                    'Suggest tools for this task',
                  ].map((example, idx) => (
                    <button
                      key={idx}
                      onClick={() => setInput(example)}
                      className="block w-full text-left px-3 py-2 text-xs bg-[var(--bg-primary)] hover:bg-purple-600/20 rounded-lg transition-colors text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    >
                      → {example}
                    </button>
                  ))}
                  {focusedNode.type === 'input' && [
                    'Make the prompt clearer',
                    'Add options for choices',
                    'Improve user experience',
                  ].map((example, idx) => (
                    <button
                      key={idx}
                      onClick={() => setInput(example)}
                      className="block w-full text-left px-3 py-2 text-xs bg-[var(--bg-primary)] hover:bg-purple-600/20 rounded-lg transition-colors text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    >
                      → {example}
                    </button>
                  ))}
                  {(focusedNode.type !== 'llm' && focusedNode.type !== 'input') && [
                    'How can I improve this node?',
                    'What should I configure here?',
                    'Show me best practices',
                  ].map((example, idx) => (
                    <button
                      key={idx}
                      onClick={() => setInput(example)}
                      className="block w-full text-left px-3 py-2 text-xs bg-[var(--bg-primary)] hover:bg-purple-600/20 rounded-lg transition-colors text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    >
                      → {example}
                    </button>
                  ))}
                </div>
              </>
            ) : (
              <>
                <p className="text-[var(--text-secondary)] text-sm mb-4">
                  {context === 'modify_flow' 
                    ? 'I can help you modify and improve this flow'
                    : 'I can help you design and build flows'}
                </p>
                
                {/* Quick Examples based on context */}
                <div className="text-left space-y-2">
                  <p className="text-xs text-[var(--text-muted)] mb-2">Try an example:</p>
                  {context === 'modify_flow' ? [
                    'Add a new node to save the result',
                    'Insert an input step before processing',
                    'Add error handling to the flow',
                    'Add a confirmation step at the end',
                  ].map((example, idx) => (
                    <button
                      key={idx}
                      onClick={() => setInput(example)}
                      className="block w-full text-left px-3 py-2 text-xs bg-[var(--bg-primary)] hover:bg-purple-600/20 rounded-lg transition-colors text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    >
                      → {example}
                    </button>
                  )) : [
                    'Create a simple Q&A chatbot',
                    'Build a web search summarizer',
                    'Make a file reader and analyzer',
                    'Create a multi-step reasoning flow',
                  ].map((example, idx) => (
                    <button
                      key={idx}
                      onClick={() => setInput(example)}
                      className="block w-full text-left px-3 py-2 text-xs bg-[var(--bg-primary)] hover:bg-purple-600/20 rounded-lg transition-colors text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    >
                      → {example}
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
        
        {messages.map((msg, idx) => (
          <div 
            key={idx} 
            className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
          >
            <div 
              className={`max-w-[85%] px-3 py-2 rounded-lg text-sm ${
                msg.role === 'user' 
                  ? 'bg-purple-600 text-white' 
                  : msg.role === 'system'
                  ? 'bg-green-600/20 text-green-400 text-center w-full'
                  : msg.isError
                  ? 'bg-red-600/20 text-red-400'
                  : 'bg-[var(--bg-primary)] text-[var(--text-primary)]'
              }`}
            >
              <div className="whitespace-pre-wrap break-words">
                {/* Render message content, removing YAML blocks for cleaner display */}
                {msg.content.split('```yaml')[0].trim() || msg.content}
              </div>
              
              {/* Show YAML indicator if present */}
              {msg.proposedYaml && (
                <div className="mt-2 pt-2 border-t border-white/10 text-xs flex items-center gap-1 text-purple-300">
                  <Check size={12} />
                  YAML generated - use buttons below to preview/apply
                </div>
              )}
            </div>
          </div>
        ))}
        
        {isLoading && (
          <div className="flex justify-start">
            <div className="bg-[var(--bg-primary)] px-3 py-2 rounded-lg">
              <Loader2 size={16} className="animate-spin text-purple-400" />
            </div>
          </div>
        )}
        
        {/* Store Results Panel - shown when tools are found */}
        {storeResults && storeResults.length > 0 && (
          <StoreResultsPanel 
            storeResults={storeResults}
            installingTool={installingTool}
            onInstall={handleInstallTool}
          />
        )}
        
        <div ref={messagesEndRef} />
      </div>

      {/* Input */}
      <div className="p-3 border-t border-[var(--border-color)]">
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={getPlaceholder()}
            rows={isExpanded ? 6 : 1}
            className={`flex-1 px-3 py-2 bg-[var(--bg-primary)] border border-[var(--border-color)] rounded-lg text-sm text-[var(--text-primary)] placeholder:text-[var(--text-secondary)] focus:outline-none focus:ring-2 focus:ring-purple-500/50 ${isExpanded ? 'resize-y min-h-[120px]' : 'resize-none'}`}
            disabled={isLoading}
          />
          <button
            onClick={handleSend}
            disabled={!input.trim() || isLoading}
            className="px-3 py-2 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg transition-colors"
          >
            <Send size={16} className="text-white" />
          </button>
        </div>
      </div>
    </div>
  )
}
