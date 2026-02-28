import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { Send, Plus, Trash2, MessageSquare, ChevronRight, ChevronDown, Loader, Square, Copy, Check, Code, RotateCcw, Wrench, Clock, Search } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { fetchSessions, fetchSessionHistory, deleteSession, connectChat, stopChat } from '../api/studioChat'
import HomePage from './HomePage'

export default function StudioChat({ theme, initialSessionId, onSessionChange }) {
  // Session state
  const [sessions, setSessions] = useState([])
  const [activeSessionId, setActiveSessionId] = useState(initialSessionId || null)
  const [isLoadingSessions, setIsLoadingSessions] = useState(true)
  const [isLoadingHistory, setIsLoadingHistory] = useState(false)
  const [sessionFilter, setSessionFilter] = useState('')

  // Chat state
  const [messages, setMessages] = useState([])
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)

  // Slash command popup
  const [showSlashPopup, setShowSlashPopup] = useState(false)
  const [slashFilter, setSlashFilter] = useState('')
  const [slashIndex, setSlashIndex] = useState(0)

  // UI state
  const [expandedTools, setExpandedTools] = useState(new Set())
  const [copiedIndex, setCopiedIndex] = useState(null)
  const [rawViewIndices, setRawViewIndices] = useState(new Set())
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  // Refs
  const scrollRef = useRef(null)
  const inputRef = useRef(null)
  const abortRef = useRef(null)
  const streamingTextRef = useRef('')

  const slashCommands = useMemo(() => [
    { cmd: '/help', desc: 'Show available commands' },
    { cmd: '/status', desc: 'Show provider, model, and tools info' },
    { cmd: '/new', desc: 'Start a fresh conversation' },
    { cmd: '/compact', desc: 'Show context window usage' },
    { cmd: '/distill', desc: 'Distill last task into a flow' },
  ], [])

  // Wrapper to keep URL in sync with active session
  const changeSession = useCallback((sessionId) => {
    setActiveSessionId(sessionId)
    if (onSessionChange) onSessionChange(sessionId)
  }, [onSessionChange])

  // Load sessions on mount (and initial session if URL specifies one)
  useEffect(() => {
    loadSessions()
    if (initialSessionId) {
      loadSessionHistory(initialSessionId)
    }
  }, [])

  // Auto-scroll on new messages
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages])

  // Focus input when not streaming
  useEffect(() => {
    if (!isStreaming && inputRef.current) {
      inputRef.current.focus()
    }
  }, [isStreaming, activeSessionId])

  const loadSessions = async () => {
    try {
      setIsLoadingSessions(true)
      const data = await fetchSessions()
      setSessions(Array.isArray(data) ? data : [])
    } catch (err) {
      console.error('Failed to load sessions:', err)
      setSessions([])
    } finally {
      setIsLoadingSessions(false)
    }
  }

  const loadSessionHistory = async (sessionId) => {
    try {
      setIsLoadingHistory(true)
      const data = await fetchSessionHistory(sessionId)
      setMessages(data.messages || [])
    } catch (err) {
      console.error('Failed to load session history:', err)
      setMessages([])
    } finally {
      setIsLoadingHistory(false)
    }
  }

  const handleSelectSession = useCallback(async (sessionId) => {
    // Cancel any active stream
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    setIsStreaming(false)
    changeSession(sessionId)
    await loadSessionHistory(sessionId)
  }, [])

  const handleNewSession = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    setIsStreaming(false)
    changeSession(null)
    setMessages([])
    if (inputRef.current) inputRef.current.focus()
  }, [])

  const handleDeleteSession = useCallback(async (e, sessionId) => {
    e.stopPropagation()
    try {
      await deleteSession(sessionId)
      setSessions(prev => prev.filter(s => s.id !== sessionId))
      if (activeSessionId === sessionId) {
        changeSession(null)
        setMessages([])
      }
    } catch (err) {
      console.error('Failed to delete session:', err)
    }
  }, [activeSessionId])

  const handleStop = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    if (activeSessionId) {
      stopChat(activeSessionId)
    }
    setIsStreaming(false)
  }, [activeSessionId])

  const sendMessage = useCallback((text) => {
    if (!text.trim()) return
    const userMsg = text.trim()

    // Add user message to chat (unless it's a slash command)
    if (!userMsg.startsWith('/')) {
      setMessages(prev => [...prev, { type: 'user', content: userMsg }])
    }

    setInput('')
    if (inputRef.current) {
      inputRef.current.style.height = 'auto'
    }
    setIsStreaming(true)
    streamingTextRef.current = ''

    const controller = connectChat({
      sessionId: activeSessionId || '',
      message: userMsg,
      onEvent: (eventType, data) => {
        switch (eventType) {
          case 'session':
            if (data.sessionId) {
              changeSession(data.sessionId)
              // Refresh session list to include new session
              if (data.isNew) {
                setTimeout(() => loadSessions(), 500)
              }
            }
            break

          case 'text':
            if (data.text) {
              streamingTextRef.current += data.text
              const currentText = streamingTextRef.current
              setMessages(prev => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && last._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: currentText, _streaming: true }]
                }
                return [...prev, { type: 'agent', content: currentText, _streaming: true }]
              })
            }
            break

          case 'tool_call':
            // Finalize any streaming text before tool call
            if (streamingTextRef.current) {
              const finalText = streamingTextRef.current
              streamingTextRef.current = ''
              setMessages(prev => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && last._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: finalText }]
                }
                return prev
              })
            }
            setMessages(prev => [...prev, { type: 'tool_call', toolName: data.name, toolArgs: data.args }])
            break

          case 'tool_result':
            setMessages(prev => [...prev, { type: 'tool_result', toolName: data.name, toolResult: data.result }])
            break

          case 'image':
            if (data.data && data.mimeType) {
              setMessages(prev => [...prev, { type: 'image', data: data.data, mimeType: data.mimeType }])
            }
            break

          case 'system':
            setMessages(prev => [...prev, { type: 'system', content: data.content }])
            break

          case 'new_session':
            if (data.sessionId) {
              changeSession(data.sessionId)
              setMessages([])
              streamingTextRef.current = ''
              loadSessions()
            }
            break

          case 'session_title':
            // Update the session title in the sidebar
            if (data.title) {
              setSessions(prev =>
                prev.map(s => s.id === activeSessionId ? { ...s, title: data.title } : s)
              )
            }
            break

          case 'distill_preview':
            setMessages(prev => [...prev, {
              type: 'system',
              content: `**Distill Preview**\n\n${data.description}\n\nSession: \`${data.sessionId}\``,
            }])
            break

          case 'approval':
            setMessages(prev => [...prev, {
              type: 'approval',
              toolName: data.tool,
              options: data.options,
            }])
            break

          case 'auto_approved':
            setMessages(prev => [...prev, {
              type: 'auto_approved',
              toolName: data.tool,
            }])
            break

          case 'thinking':
            // Show as a transient indicator (replace previous thinking)
            setMessages(prev => {
              const filtered = prev.filter(m => m.type !== 'thinking')
              return [...filtered, { type: 'thinking', content: data.text }]
            })
            break

          case 'retry':
            setMessages(prev => [...prev, {
              type: 'retry',
              attempt: data.attempt,
              maxRetries: data.maxRetries,
              reason: data.reason,
            }])
            break

          case 'error':
            setMessages(prev => [...prev, { type: 'error', content: data.error || data.message || 'Unknown error' }])
            break

          case 'error_info':
            setMessages(prev => [...prev, {
              type: 'error_info',
              title: data.title,
              reason: data.reason,
              suggestion: data.suggestion,
              originalError: data.originalError,
            }])
            break

          case 'done':
            // Finalize streaming text
            if (streamingTextRef.current) {
              const finalText = streamingTextRef.current
              streamingTextRef.current = ''
              setMessages(prev => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && last._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: finalText }]
                }
                return prev
              })
            }
            // Remove transient thinking messages
            setMessages(prev => prev.filter(m => m.type !== 'thinking'))
            break

          default:
            break
        }
      },
      onError: (err) => {
        console.error('Chat stream error:', err)
        setMessages(prev => [...prev, { type: 'error', content: err.message }])
        setIsStreaming(false)
      },
      onDone: () => {
        setIsStreaming(false)
        // Refresh sessions to pick up title updates
        setTimeout(() => loadSessions(), 1000)
      },
    })

    abortRef.current = controller
  }, [activeSessionId])

  const handleSubmit = (e) => {
    e.preventDefault()
    if (isStreaming) return

    // If slash popup is open, send the highlighted or only matching command
    if (showSlashPopup && filteredSlashCommands.length > 0) {
      const selected = filteredSlashCommands[slashIndex] || filteredSlashCommands[0]
      handleSlashSelect(selected.cmd)
      return
    }

    // If input starts with / but popup is closed (no matches), ignore
    if (input.startsWith('/') && !input.includes(' ')) {
      return
    }

    if (!input.trim()) return
    sendMessage(input)
  }

  // Auto-resize textarea to fit content
  const autoResize = useCallback((el) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }, [])

  // Handle input changes for slash command popup
  const handleInputChange = (e) => {
    const val = e.target.value
    setInput(val)
    autoResize(e.target)

    if (val === '/') {
      setShowSlashPopup(true)
      setSlashFilter('')
      setSlashIndex(0)
    } else if (val.startsWith('/') && !val.includes(' ')) {
      setShowSlashPopup(true)
      setSlashFilter(val.slice(1).toLowerCase())
      setSlashIndex(0)
    } else {
      setShowSlashPopup(false)
    }
  }

  const handleSlashSelect = (cmd) => {
    setShowSlashPopup(false)
    setSlashIndex(0)
    setInput('')
    if (inputRef.current) {
      inputRef.current.style.height = 'auto'
    }
    sendMessage(cmd)
  }

  const handleKeyDown = (e) => {
    if (!showSlashPopup) return

    if (e.key === 'Escape') {
      setShowSlashPopup(false)
      e.preventDefault()
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSlashIndex(prev => (prev + 1) % filteredSlashCommands.length)
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSlashIndex(prev => (prev - 1 + filteredSlashCommands.length) % filteredSlashCommands.length)
      return
    }

    if (e.key === 'Tab') {
      e.preventDefault()
      if (filteredSlashCommands.length > 0) {
        const selected = filteredSlashCommands[slashIndex] || filteredSlashCommands[0]
        handleSlashSelect(selected.cmd)
      }
      return
    }
  }

  const filteredSlashCommands = useMemo(() => {
    if (!slashFilter) return slashCommands
    return slashCommands.filter(c => c.cmd.slice(1).startsWith(slashFilter))
  }, [slashFilter, slashCommands])

  const toggleToolExpand = (index) => {
    setExpandedTools(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  const toggleRawView = (index) => {
    setRawViewIndices(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  const copyToClipboard = async (content, index) => {
    await navigator.clipboard.writeText(content)
    setCopiedIndex(index)
    setTimeout(() => setCopiedIndex(null), 2000)
  }

  const filteredSessions = useMemo(() => {
    if (!sessionFilter) return sessions
    const q = sessionFilter.toLowerCase()
    return sessions.filter(s => (s.title || s.id).toLowerCase().includes(q))
  }, [sessions, sessionFilter])

  const formatTimeAgo = (dateStr) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now - date
    const mins = Math.floor(diffMs / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    if (days < 7) return `${days}d ago`
    return date.toLocaleDateString()
  }

  // Render a single tool call as a collapsible card
  const renderToolCard = (msg, index) => {
    const isExpanded = expandedTools.has(index)
    const isCall = msg.type === 'tool_call'
    const name = msg.toolName || 'unknown'
    const data = isCall ? msg.toolArgs : msg.toolResult

    return (
      <div
        key={index}
        className="my-2 rounded-lg overflow-hidden"
        style={{
          border: '1px solid var(--border-color)',
          background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
        }}
      >
        <button
          onClick={() => toggleToolExpand(index)}
          className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-purple-500/5 transition-colors"
        >
          {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <Wrench size={14} className="text-purple-400" />
          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
            {isCall ? 'Tool Call' : 'Tool Result'}: <code className="bg-purple-500/15 px-1.5 py-0.5 rounded text-purple-300">{name}</code>
          </span>
        </button>
        {isExpanded && data && (
          <div className="px-3 pb-3">
            <pre
              className="text-xs whitespace-pre-wrap break-words font-mono p-2 rounded"
              style={{
                background: theme === 'dark' ? 'rgba(0,0,0,0.3)' : 'rgba(0,0,0,0.05)',
                color: 'var(--text-secondary)',
                maxHeight: '300px',
                overflowY: 'auto',
              }}
            >
              {typeof data === 'string' ? data : JSON.stringify(data, null, 2)}
            </pre>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-1 overflow-hidden" style={{ background: 'var(--bg-primary)' }}>
      {/* Session Sidebar */}
      {!sidebarCollapsed ? (
        <div
          className="flex flex-col"
          style={{
            width: '280px',
            minWidth: '280px',
            borderRight: '1px solid var(--border-color)',
            background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
          }}
        >
          {/* Sidebar Header */}
          <div className="flex items-center justify-between px-4 py-3" style={{ borderBottom: '1px solid var(--border-color)' }}>
            <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>Conversations</span>
            <div className="flex items-center gap-1">
              <button
                onClick={handleNewSession}
                className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
                title="New conversation"
                style={{ color: 'var(--text-secondary)' }}
              >
                <Plus size={16} />
              </button>
              <button
                onClick={() => setSidebarCollapsed(true)}
                className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
                title="Hide sidebar"
                style={{ color: 'var(--text-secondary)' }}
              >
                <ChevronRight size={16} className="rotate-180" />
              </button>
            </div>
          </div>

          {/* Search */}
          <div className="px-3 py-2">
            <div className="relative">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }} />
              <input
                type="text"
                value={sessionFilter}
                onChange={(e) => setSessionFilter(e.target.value)}
                placeholder="Search conversations..."
                className="w-full pl-8 pr-3 py-1.5 text-xs rounded-lg focus:outline-none focus:ring-1 focus:ring-purple-500"
                style={{
                  background: 'var(--bg-tertiary)',
                  color: 'var(--text-primary)',
                  border: '1px solid var(--border-color)',
                }}
              />
            </div>
          </div>

          {/* Session List */}
          <div className="flex-1 overflow-y-auto">
            {isLoadingSessions ? (
              <div className="flex items-center justify-center py-8">
                <Loader size={18} className="animate-spin text-purple-400" />
              </div>
            ) : filteredSessions.length === 0 ? (
              <div className="px-4 py-8 text-center">
                <MessageSquare size={24} className="mx-auto mb-2" style={{ color: 'var(--text-muted)' }} />
                <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                  {sessionFilter ? 'No matching conversations' : 'No conversations yet'}
                </p>
              </div>
            ) : (
              filteredSessions.map(session => (
                <button
                  key={session.id}
                  onClick={() => handleSelectSession(session.id)}
                  className={`w-full text-left px-4 py-3 transition-colors group ${
                    activeSessionId === session.id ? 'bg-purple-500/15' : 'hover:bg-purple-500/5'
                  }`}
                  style={{ borderBottom: '1px solid var(--border-color)' }}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <p
                        className="text-sm font-medium truncate"
                        style={{ color: activeSessionId === session.id ? 'var(--accent)' : 'var(--text-primary)' }}
                      >
                        {session.title || 'Untitled'}
                      </p>
                      <div className="flex items-center gap-2 mt-1">
                        <Clock size={10} style={{ color: 'var(--text-muted)' }} />
                        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                          {formatTimeAgo(session.updatedAt)}
                        </span>
                        <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
                          {session.messageCount} msg{session.messageCount !== 1 ? 's' : ''}
                        </span>
                      </div>
                    </div>
                    <button
                      onClick={(e) => handleDeleteSession(e, session.id)}
                      className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 transition-all"
                      title="Delete conversation"
                    >
                      <Trash2 size={12} className="text-red-400" />
                    </button>
                  </div>
                </button>
              ))
            )}
          </div>
        </div>
      ) : (
        <div
          className="flex flex-col items-center py-3 gap-3"
          style={{
            borderRight: '1px solid var(--border-color)',
            background: theme === 'dark' ? 'rgba(15, 23, 42, 0.5)' : 'var(--bg-secondary)',
          }}
        >
          <button
            onClick={() => setSidebarCollapsed(false)}
            className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
            title="Show sidebar"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronRight size={16} />
          </button>
          <button
            onClick={handleNewSession}
            className="p-1.5 rounded-lg hover:bg-purple-500/15 transition-colors"
            title="New conversation"
            style={{ color: 'var(--text-secondary)' }}
          >
            <Plus size={16} />
          </button>
        </div>
      )}

      {/* Chat Area */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Messages Area */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-4">
          {isLoadingHistory ? (
            <div className="flex items-center justify-center py-16">
              <Loader size={24} className="animate-spin text-purple-400" />
            </div>
          ) : messages.length === 0 ? (
            <HomePage />
          ) : (
            messages.map((msg, index) => {
              if (msg.type === 'user') {
                return (
                  <div key={index} className="flex justify-end">
                    <div className="space-y-1 max-w-[80%]">
                      <div className="text-xs font-medium text-right" style={{ color: 'var(--text-muted)' }}>You</div>
                      <div className="chat-bubble-user p-3 rounded-lg">
                        <p className="text-sm whitespace-pre-wrap">{msg.content}</p>
                      </div>
                    </div>
                  </div>
                )
              }

              if (msg.type === 'agent') {
                return (
                  <div key={index} className="space-y-1">
                    <div className="flex items-center justify-between">
                      <div className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>Agent</div>
                      <div className="flex gap-1">
                        <button
                          onClick={() => toggleRawView(index)}
                          className="p-1 rounded hover:bg-white/10 transition-colors"
                          title={rawViewIndices.has(index) ? 'Show formatted' : 'Show raw markdown'}
                        >
                          <Code size={14} className={rawViewIndices.has(index) ? 'text-purple-400' : 'text-gray-500'} />
                        </button>
                        <button
                          onClick={() => copyToClipboard(msg.content, index)}
                          className="p-1 rounded hover:bg-white/10 transition-colors"
                          title="Copy"
                        >
                          {copiedIndex === index ? (
                            <Check size={14} className="text-green-400" />
                          ) : (
                            <Copy size={14} className="text-gray-500" />
                          )}
                        </button>
                      </div>
                    </div>
                    <div
                      className="p-4 rounded-lg max-w-[90%]"
                      style={{
                        background: theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'white',
                        border: '1px solid var(--border-color)',
                      }}
                    >
                      {rawViewIndices.has(index) ? (
                        <pre className="text-sm whitespace-pre-wrap break-words font-mono" style={{ color: 'var(--text-primary)' }}>
                          {msg.content}
                        </pre>
                      ) : (
                        <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                        </div>
                      )}
                    </div>
                  </div>
                )
              }

              if (msg.type === 'tool_call' || msg.type === 'tool_result') {
                return renderToolCard(msg, index)
              }

              if (msg.type === 'image') {
                return (
                  <div key={index} className="my-2">
                    <img
                      src={`data:${msg.mimeType};base64,${msg.data}`}
                      alt="Screenshot"
                      className="rounded-lg max-w-full"
                      style={{
                        maxHeight: '500px',
                        border: '1px solid var(--border-color)',
                      }}
                    />
                  </div>
                )
              }

              if (msg.type === 'system') {
                return (
                  <div key={index} className="my-2">
                    <div
                      className="p-3 rounded-lg text-sm"
                      style={{
                        background: theme === 'dark' ? 'rgba(168, 85, 247, 0.08)' : 'rgba(168, 85, 247, 0.05)',
                        border: '1px solid rgba(168, 85, 247, 0.2)',
                      }}
                    >
                      <div className="markdown-body text-sm" style={{ color: 'var(--text-primary)' }}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                      </div>
                    </div>
                  </div>
                )
              }

              if (msg.type === 'error') {
                return (
                  <div key={index} className="p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                    Error: {msg.content}
                  </div>
                )
              }

              if (msg.type === 'error_info') {
                return (
                  <div key={index} className="my-3 p-4 rounded-lg bg-red-500/5 border border-red-500/20 space-y-3">
                    <div className="flex items-center gap-2 text-red-400 font-medium">
                      <span>&#x2715;</span> {msg.title}
                    </div>
                    {msg.reason && <p className="text-sm text-gray-300 pl-4">{msg.reason}</p>}
                    {msg.suggestion && (
                      <div className="pl-4">
                        <p className="text-sm font-medium text-yellow-400">Suggestion:</p>
                        <p className="text-sm text-yellow-300/80">{msg.suggestion}</p>
                      </div>
                    )}
                    {msg.originalError && (
                      <details className="pl-4">
                        <summary className="text-xs text-gray-500 cursor-pointer hover:text-gray-400">Raw Error</summary>
                        <pre className="text-xs text-gray-500 mt-1 whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
                          {msg.originalError}
                        </pre>
                      </details>
                    )}
                  </div>
                )
              }

              if (msg.type === 'approval') {
                return (
                  <div key={index} className="my-2 p-3 rounded-lg bg-yellow-500/10 border border-yellow-500/20">
                    <p className="text-sm font-medium text-yellow-400 mb-2">
                      Approve tool: <code className="bg-yellow-500/20 px-1.5 rounded">{msg.toolName}</code>
                    </p>
                    {msg.options && msg.options.length > 0 && (
                      <div className="flex gap-2 flex-wrap">
                        {msg.options.map((opt, i) => (
                          <button
                            key={i}
                            onClick={() => sendMessage(String(opt))}
                            className="px-3 py-1.5 text-xs bg-yellow-500/20 hover:bg-yellow-500/30 text-yellow-300 border border-yellow-500/30 rounded transition-colors"
                          >
                            {String(opt)}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                )
              }

              if (msg.type === 'auto_approved') {
                return (
                  <div key={index} className="flex items-center gap-2 px-3 py-2 my-1 text-sm">
                    <span className="flex items-center gap-1.5 px-2 py-1 rounded bg-green-500/10 border border-green-500/20 text-green-400">
                      <span>&#10003;</span> Auto-approved: <code className="bg-green-500/20 px-1.5 py-0.5 rounded font-mono text-xs">{msg.toolName}</code>
                    </span>
                  </div>
                )
              }

              if (msg.type === 'thinking') {
                return (
                  <div key={index} className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm w-fit bg-yellow-500/10 text-yellow-400 border border-yellow-500/20">
                    <Loader size={14} className="animate-spin" />
                    <span>{msg.content || 'Thinking...'}</span>
                  </div>
                )
              }

              if (msg.type === 'retry') {
                return (
                  <div key={index} className="flex items-center gap-2 px-3 py-2 my-1 text-sm">
                    <RotateCcw size={14} className="text-orange-400" />
                    <span className="text-orange-400 font-medium">Retry {msg.attempt}/{msg.maxRetries}:</span>
                    <span className="text-gray-400">{msg.reason}</span>
                  </div>
                )
              }

              return null
            })
          )}

          {/* Streaming indicator */}
          {isStreaming && messages.length > 0 && messages[messages.length - 1]?.type !== 'thinking' && (
            <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-purple-500/10 border border-purple-500/20 w-fit">
              <Loader size={14} className="text-purple-400 animate-spin" />
              <span className="text-xs text-purple-300">Processing...</span>
            </div>
          )}
        </div>

        {/* Input Area */}
        <div className="relative" style={{ borderTop: '1px solid var(--border-color)' }}>
          {/* Slash command popup */}
          {showSlashPopup && filteredSlashCommands.length > 0 && (
            <div
              className="absolute bottom-full left-4 right-4 mb-1 rounded-lg shadow-xl overflow-hidden"
              style={{
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border-color)',
                zIndex: 50,
              }}
            >
              {filteredSlashCommands.map(({ cmd, desc }, i) => (
                <button
                  key={cmd}
                  onClick={() => handleSlashSelect(cmd)}
                  className={`w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors ${
                    i === slashIndex ? 'bg-purple-500/15' : 'hover:bg-purple-500/10'
                  }`}
                >
                  <code className="text-sm font-mono text-purple-400">{cmd}</code>
                  <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{desc}</span>
                </button>
              ))}
            </div>
          )}

          <form onSubmit={handleSubmit} className="flex items-end gap-3 p-4">
            {isStreaming && (
              <button
                type="button"
                onClick={handleStop}
                className="px-3 py-2.5 bg-red-500 hover:bg-red-600 text-white rounded-lg transition-colors flex items-center gap-2"
                title="Stop"
              >
                <Square size={16} />
              </button>
            )}
            <div className="relative flex-1">
              <textarea
                ref={inputRef}
                value={input}
                onChange={handleInputChange}
                onKeyDown={(e) => {
                  // Enter without Shift submits the form
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    if (showSlashPopup && filteredSlashCommands.length > 0) {
                      const selected = filteredSlashCommands[slashIndex] || filteredSlashCommands[0]
                      handleSlashSelect(selected.cmd)
                    } else if (!isStreaming && input.trim()) {
                      // Reuse slash validation from handleSubmit
                      if (input.startsWith('/') && !input.includes(' ')) return
                      sendMessage(input)
                    }
                    return
                  }
                  handleKeyDown(e)
                }}
                disabled={isStreaming}
                placeholder={isStreaming ? 'Agent is responding...' : 'Type a message or / for commands...'}
                rows={1}
                className="w-full px-4 py-2.5 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 disabled:opacity-60 disabled:cursor-not-allowed transition-all text-sm resize-none overflow-hidden"
                style={{
                  background: 'var(--bg-tertiary)',
                  color: 'var(--text-primary)',
                  border: '1px solid var(--border-color)',
                  maxHeight: '200px',
                  overflowY: 'auto',
                }}
              />
            </div>
            <button
              type="submit"
              disabled={isStreaming || !input.trim()}
              className="px-4 py-2.5 bg-[#805AD5] hover:bg-[#6B46C1] text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Send size={18} />
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}
