import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { Send, Plus, Trash2, MessageSquare, ChevronRight, ChevronDown, Loader, Square, Copy, Check, Code, RotateCcw, Wrench, Clock, Search, Users } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { fetchSessions, fetchSessionHistory, deleteSession, connectChat, stopChat } from '../api/studioChat'
import type { ChatSession } from '../api/studioChat'
import { startFleetSession, connectFleetStream, sendFleetMessage, stopFleetSession, fetchFleetSessions } from '../api/fleetChat'
import type { FleetSession } from '../api/fleetChat'
import HomePage from './HomePage'
import type { FleetMessageItem, ChatMsg, FleetInfo, FleetStateInfo, DeferredPrompt, FleetExecutionMessage, FleetEvent, AgentMessage, ToolCallMessage, ToolResultMessage } from './chat/chatTypes'
import { getAgentColor } from './chat/chatTypes'
import FleetStartDialog from './chat/FleetStartDialog'
import FleetTemplatePicker from './chat/FleetTemplatePicker'
import FleetExecutionPanel from './chat/FleetExecutionPanel'

// Extended ChatSession with optional fleet fields coming from the sidebar
interface SidebarSession extends ChatSession {
  fleetKey?: string
  fleetName?: string
}

export default function StudioChat({ theme, initialSessionId, pendingChatMessage, onPendingChatMessageConsumed, onSessionChange }: { theme: string; initialSessionId?: string | null; pendingChatMessage?: string | null; onPendingChatMessageConsumed?: () => void; onSessionChange?: (sessionId: string | null) => void }) {
  // Session state
  const [sessions, setSessions] = useState<SidebarSession[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(initialSessionId || null)
  const [isLoadingSessions, setIsLoadingSessions] = useState(true)
  const [isLoadingHistory, setIsLoadingHistory] = useState(false)
  const [sessionFilter, setSessionFilter] = useState('')

  // Chat state
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)

  // Fleet state
  const [isFleetMode, setIsFleetMode] = useState(false)
  const [fleetSessionId, setFleetSessionId] = useState<string | null>(null)
  const [fleetInfo, setFleetInfo] = useState<FleetInfo | null>(null) // { fleet_key, fleet_name, agents }
  const [fleetState, setFleetState] = useState<FleetStateInfo | null>(null) // { state, active_agent }
  const [showFleetDialog, setShowFleetDialog] = useState(false)
  const [fleetDialogMessage, setFleetDialogMessage] = useState('') // pre-populated from /fleet command
  const [showTemplatePicker, setShowTemplatePicker] = useState(false) // /fleet-plan without template key
  const [pendingFleetPlanPrompt, setPendingFleetPlanPrompt] = useState<DeferredPrompt | null>(null) // deferred plan creation message
  const [pendingDrillPrompt, setPendingDrillPrompt] = useState<DeferredPrompt | null>(null) // deferred drill creation message
  const [activeWizardContext, setActiveWizardContext] = useState<string | null>(null) // persisted wizard system prompt for multi-turn sessions

  // Slash command popup
  const [showSlashPopup, setShowSlashPopup] = useState(false)
  const [slashFilter, setSlashFilter] = useState('')
  const [slashIndex, setSlashIndex] = useState(0)

  // UI state
  const [expandedTools, setExpandedTools] = useState<Set<number>>(new Set())
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null)
  const [rawViewIndices, setRawViewIndices] = useState<Set<number>>(new Set())
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  // Refs
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const streamingTextRef = useRef('')

  const slashCommands = useMemo(() => [
    { cmd: '/help', desc: 'Show available commands' },
    { cmd: '/status', desc: 'Show provider, model, and tools info' },
    { cmd: '/new', desc: 'Start a fresh conversation' },
    { cmd: '/compact', desc: 'Show context window usage' },
    { cmd: '/distill', desc: 'Distill last task into a flow' },
    { cmd: '/fleet', desc: 'Start a fleet-based task with specialized agents' },
    { cmd: '/fleet-plan', desc: 'Create a reusable fleet plan' },
    { cmd: '/drill', desc: 'Create a drill suite with guided wizard' },
    { cmd: '/drill-add', desc: 'Add new drills to an existing suite' },
  ], [])

  // Wrapper to keep URL in sync with active session
  const changeSession = useCallback((sessionId: string | null, { userInitiated = false } = {}) => {
    setActiveSessionId(sessionId)
    if (userInitiated) {
      setActiveWizardContext(null) // only clear wizard context on explicit user navigation
    }
    if (onSessionChange) onSessionChange(sessionId)
  }, [onSessionChange])

  const connectToFleetStream = useCallback((sessionId: string) => {
    const controller = connectFleetStream({
      sessionId,
      onEvent: (eventType, data) => {
        switch (eventType) {
          case 'fleet_session':
            setFleetInfo({ fleet_key: data.fleet_key as string, fleet_name: data.fleet_name as string, agents: data.agents })
            break

          case 'fleet_message':
            setMessages((prev: ChatMsg[]) => {
              // Deduplicate by message ID
              if (data.id && prev.some(m => (m as FleetMessageItem).id === data.id)) {
                return prev
              }
              // Skip human messages from the stream since we add them optimistically.
              // Match by sender + text to detect the duplicate.
              if (data.sender === 'customer' && prev.some(m => (m as FleetMessageItem).sender === 'customer' && (m as FleetMessageItem).text === data.text && !(m as FleetMessageItem).id)) {
                // Replace the optimistic message (no id) with the server version (has id)
                return prev.map(m =>
                  (m as FleetMessageItem).sender === 'customer' && (m as FleetMessageItem).text === data.text && !(m as FleetMessageItem).id
                    ? { ...m, id: data.id, timestamp: (data.timestamp as number) || (m as FleetMessageItem).timestamp } as ChatMsg
                    : m
                )
              }
              return [...prev, { type: 'fleet_message', ...data, timestamp: (data.timestamp as number) || Date.now() } as ChatMsg]
            })
            break

          case 'fleet_state':
            setFleetState({ state: data.state as string, active_agent: data.active_agent as string })
            break

          case 'fleet_done':
            setIsStreaming(false)
            break

          case 'error':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: (data.error as string) || 'Unknown error' }])
            break

          default:
            break
        }
      },
      onError: (err) => {
        console.error('Fleet stream error:', err)
        setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: err.message }])
        setIsStreaming(false)
      },
      onDone: () => {
        setIsStreaming(false)
      },
    })

    abortRef.current = controller
    return controller
  }, [])

  // Load sessions on mount (and initial session if URL specifies one)
  // Also check for active fleet sessions that we should reconnect to.
  useEffect(() => {
    loadSessions()

    const init = async () => {
      if (initialSessionId) {
        // Check if this is an active fleet session
        try {
          const data = await fetchFleetSessions()
          const activeFleet = (data.sessions || []).find((s: FleetSession) => s.id === initialSessionId)
          if (activeFleet) {
            setIsFleetMode(true)
            setFleetSessionId(initialSessionId)
            setFleetState({ state: activeFleet.state, active_agent: activeFleet.active_agent })
            setMessages([])
            setIsStreaming(true)
            changeSession(initialSessionId)
            connectToFleetStream(initialSessionId)
            return
          }
        } catch {
          // fetchFleetSessions may fail if fleet system not initialized; that's ok
        }
        // Not a fleet session (or fleet no longer active), load as regular
        loadSessionHistory(initialSessionId)
      }
    }
    init()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

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
    } catch (err: any) {
      console.error('Failed to load sessions:', err)
      setSessions([])
    } finally {
      setIsLoadingSessions(false)
    }
  }

  const loadSessionHistory = async (sessionId: string) => {
    try {
      setIsLoadingHistory(true)
      const data = await fetchSessionHistory(sessionId)
      // If the response includes fleet messages, convert them to the fleet_message format
      const dataAny = data as Record<string, any>
      if (dataAny.fleetMessages && dataAny.fleetMessages.length > 0) {
        const fleetMsgs: ChatMsg[] = dataAny.fleetMessages.map((m: any) => ({
          type: 'fleet_message' as const,
          id: m.id,
          sender: m.sender,
          text: m.text,
          mentions: m.mentions,
          timestamp: m.timestamp ? new Date(m.timestamp).getTime() : Date.now(),
          metadata: m.metadata,
        }))
        setMessages(fleetMsgs)
      } else {
        setMessages((data.messages || []) as unknown as ChatMsg[])
      }
    } catch (err: any) {
      console.error('Failed to load session history:', err)
      setMessages([])
    } finally {
      setIsLoadingHistory(false)
    }
  }

  const handleSelectSession = useCallback(async (sessionId: string) => {
    // Cancel any active stream
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    setIsStreaming(false)

    // Check if this is a fleet session (from sidebar data)
    const session = sessions.find(s => s.id === sessionId)
    if (session && session.fleetKey) {
      // Check if this fleet session is still active in the registry
      try {
        const data = await fetchFleetSessions()
        const activeFleet = (data.sessions || []).find((s: FleetSession) => s.id === sessionId)
        if (activeFleet) {
          // Reconnect to the active fleet session
          setIsFleetMode(true)
           setFleetSessionId(sessionId)
          setFleetState({ state: activeFleet.state, active_agent: activeFleet.active_agent })
          setMessages([])
          setIsStreaming(true)
          changeSession(sessionId, { userInitiated: true })
          connectToFleetStream(sessionId)
          return
        }
      } catch (err: any) {
        console.error('Failed to check fleet session status:', err)
      }
      // Fleet session is no longer active; enter fleet mode as read-only history
      setIsFleetMode(true)
      setFleetSessionId(sessionId)
      setFleetInfo({ fleet_key: session.fleetKey, fleet_name: session.fleetName || '' })
      setFleetState({ state: 'stopped', active_agent: '' })
      changeSession(sessionId, { userInitiated: true })
      await loadSessionHistory(sessionId)
      return
    } else {
      // Exit fleet mode if switching to a regular session
      if (isFleetMode) {
        setIsFleetMode(false)
        setFleetSessionId(null)
        setFleetInfo(null)
        setFleetState(null)
      }
    }

    changeSession(sessionId, { userInitiated: true })
    await loadSessionHistory(sessionId)
  }, [sessions, isFleetMode, connectToFleetStream, changeSession])

  const handleNewSession = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    // If in fleet mode, just disconnect the SSE stream (don't stop the fleet session)
    if (isFleetMode) {
      setIsFleetMode(false)
      setFleetSessionId(null)
      setFleetInfo(null)
      setFleetState(null)
    }
    setIsStreaming(false)
    changeSession(null, { userInitiated: true })
    setMessages([])
    if (inputRef.current) inputRef.current.focus()
  }, [isFleetMode, changeSession])

  const handleDeleteSession = useCallback(async (e: React.MouseEvent, sessionId: string) => {
    e.stopPropagation()
    try {
      // If this is an active fleet session, stop it first
      const session = sessions.find(s => s.id === sessionId)
      if (session && session.fleetKey) {
        try {
          await stopFleetSession(sessionId)
        } catch {
          // Fleet session may already be stopped
        }
      }
      await deleteSession(sessionId)
      setSessions(prev => prev.filter(s => s.id !== sessionId))
      if (activeSessionId === sessionId) {
        if (isFleetMode) {
          setIsFleetMode(false)
          setFleetSessionId(null)
          setFleetInfo(null)
          setFleetState(null)
          setIsStreaming(false)
          if (abortRef.current) {
            abortRef.current.abort()
            abortRef.current = null
          }
        }
        changeSession(null, { userInitiated: true })
        setMessages([])
      }
    } catch (err: any) {
      console.error('Failed to delete session:', err)
    }
  }, [activeSessionId, sessions, isFleetMode])

  const handleStop = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    if (isFleetMode && fleetSessionId) {
      stopFleetSession(fleetSessionId)
    } else if (activeSessionId) {
      stopChat(activeSessionId)
    }
    setIsStreaming(false)
  }, [activeSessionId, isFleetMode, fleetSessionId])

  // Start a fleet session
  const handleFleetStart = useCallback(async (fleetKey: string | null, initialMessage: string, planKey: string) => {
    setShowFleetDialog(false)
    setFleetDialogMessage('')
    setIsFleetMode(true)
    setMessages([])
    setIsStreaming(true)

    // Add the initial human message to the UI if provided
    if (initialMessage) {
      setMessages([{ type: 'fleet_message', sender: 'customer', text: initialMessage, timestamp: Date.now() }])
    }

    try {
      // Create the fleet session (returns JSON with session info)
      const sessionInfo = await startFleetSession({ fleetKey: fleetKey || undefined, planKey, message: initialMessage })
      setFleetSessionId(sessionInfo.session_id)
      setFleetInfo({ fleet_key: sessionInfo.fleet_key, fleet_name: sessionInfo.fleet_name, agents: sessionInfo.agents })
      changeSession(sessionInfo.session_id)

      // Refresh sidebar to show the new fleet session
      loadSessions()

      // Connect to the SSE stream for real-time events
      connectToFleetStream(sessionInfo.session_id)
    } catch (err: any) {
      console.error('Failed to start fleet session:', err)
      setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: 'Failed to start fleet: ' + err.message }])
      setIsStreaming(false)
      setIsFleetMode(false)
    }
  }, [connectToFleetStream, changeSession])

  // Send a human message to the fleet session
  const sendFleetHumanMessage = useCallback(async (text: string) => {
    if (!text.trim() || !fleetSessionId) return
    // Add human message to UI immediately
    setMessages((prev: ChatMsg[]) => [...prev, { type: 'fleet_message', sender: 'customer', text, timestamp: Date.now() }])
    setInput('')
    if (inputRef.current) inputRef.current.style.height = 'auto'
    try {
      await sendFleetMessage(fleetSessionId, text)
    } catch (err: any) {
      console.error('Failed to send fleet message:', err)
      setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: 'Failed to send message: ' + err.message }])
    }
  }, [fleetSessionId])

  // Exit fleet mode
  const handleExitFleet = useCallback(() => {
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    if (fleetSessionId) {
      stopFleetSession(fleetSessionId)
    }
    setIsFleetMode(false)
    setFleetSessionId(null)
    setFleetInfo(null)
    setFleetState(null)
    setIsStreaming(false)
    changeSession(null, { userInitiated: true })
    setMessages([])
    loadSessions()
  }, [fleetSessionId, changeSession])

  const sendMessage = useCallback((text: string, options: { systemContext?: string } = {}) => {
    if (!text.trim()) return
    const userMsg = text.trim()

    // Add user message to chat (unless it's a slash command)
    if (!userMsg.startsWith('/')) {
      setMessages((prev: ChatMsg[]) => [...prev, { type: 'user', content: userMsg }])
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
      systemContext: options.systemContext || activeWizardContext || undefined,
      onEvent: (eventType, data) => {
        switch (eventType) {
          case 'session':
            if (data.sessionId) {
              changeSession(data.sessionId as string)
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
              setMessages((prev: ChatMsg[]) => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
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
              setMessages((prev: ChatMsg[]) => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: finalText }]
                }
                return prev
              })
            }
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'tool_call', toolName: data.name, toolArgs: data.args }])
            break

          case 'tool_result':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'tool_result', toolName: data.name, toolResult: data.result }])
            // Clear wizard context once the fleet plan or drill suite has been saved
            if (data.name === 'save_fleet_plan' || data.name === 'save_drill') {
              setActiveWizardContext(null)
            }
            break

          case 'image':
            if (data.data && data.mimeType) {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'image', data: data.data, mimeType: data.mimeType }])
            }
            break

          case 'flow_output':
            // Flow output delivered directly — bypass LLM, render as markdown.
            // Finalize any pending streaming text first.
            if (streamingTextRef.current) {
              const finalText = streamingTextRef.current
              streamingTextRef.current = ''
              setMessages((prev: ChatMsg[]) => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: finalText }]
                }
                return prev
              })
            }
            if (data.content) {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'agent', content: data.content as string }])
            }
            break

          case 'new_session':
            if (data.sessionId) {
              changeSession(data.sessionId as string)
              setMessages([])
              streamingTextRef.current = ''
              loadSessions()
            }
            break

          case 'session_title':
            // Update the session title in the sidebar
            if (data.title) {
              setSessions(prev =>
                prev.map(s => s.id === activeSessionId ? { ...s, title: data.title as string } : s)
              )
            }
            break

          case 'distill_preview':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'system',
              content: `**Distill Preview**\n\n${data.description}\n\nSession: \`${data.sessionId}\``,
            }])
            break

          case 'approval':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'approval',
              toolName: data.tool,
              options: data.options,
            }])
            break

          case 'auto_approved':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'auto_approved',
              toolName: data.tool,
            }])
            break

          case 'thinking':
            // Show as a transient indicator (replace previous thinking)
            setMessages((prev: ChatMsg[]) => {
              const filtered = prev.filter(m => m.type !== 'thinking')
              return [...filtered, { type: 'thinking', content: data.text }]
            })
            break

          case 'fleet_redirect':
            // /fleet [task] command opens the fleet dialog, optionally pre-populated
            setIsStreaming(false)
            setFleetDialogMessage((data.task as string) || '')
            setShowFleetDialog(true)
            break

          case 'fleet_plan_redirect':
            // /fleet-plan [hint] command: start plan creation in a fresh conversation.
            // If the backend found a plan_wizard in the template, use it as system context.
            // If no hint, show a template picker dialog so the user selects one first.
            setIsStreaming(false)
            {
              const hint = (data.hint as string) || ''
              const wizardSystemPrompt = (data.wizard_system_prompt as string) || ''

              if (wizardSystemPrompt) {
                // Template has a wizard: persist the system prompt so it's sent on every turn
                setActiveWizardContext(wizardSystemPrompt)
                setPendingFleetPlanPrompt({ message: `Create a fleet plan from the "${hint}" template.`, systemContext: wizardSystemPrompt })
              } else if (hint) {
                // No wizard in template: use generic prompt as system context, persist it too
                const genericSystemPrompt = `You are helping the user create a fleet plan based on the "${hint}" fleet template. The base_fleet_key is "${hint}". Guide them through:\n1. Plan identity (key, name, description)\n2. Communication channel type and settings\n3. Artifact destinations\n4. Credentials for external services\n5. Any agent behavior customizations\n\nBefore saving, call validate_fleet_plan with all config including credentials. Only call save_fleet_plan after validation passes. Include the same credentials in the save call.`
                setActiveWizardContext(genericSystemPrompt)
                setPendingFleetPlanPrompt({ message: `Create a fleet plan from the "${hint}" template.`, systemContext: genericSystemPrompt })
              } else {
                // No hint: show template picker so user selects one, then re-issue /fleet-plan <key>
                setShowTemplatePicker(true)
              }
            }
            break

          case 'drill_redirect':
            // /drill [hint] command: start drill suite creation wizard
            setIsStreaming(false)
            {
              const hint = (data.hint as string) || ''
              const wizardSystemPrompt = (data.wizard_system_prompt as string) || ''

              if (wizardSystemPrompt) {
                setActiveWizardContext(wizardSystemPrompt)
                const kickoff = hint
                  ? `I'd like to create a drill suite. Here's what I want to test: ${hint}`
                  : 'I\'d like to create a drill suite for my project.'
                setPendingDrillPrompt({ message: kickoff, systemContext: wizardSystemPrompt })
              }
            }
            break

          case 'drill_add_redirect':
            // /drill-add <suite> command: start drill-add wizard for existing suite
            setIsStreaming(false)
            {
              const suiteName = (data.suite_name as string) || ''
              const wizardSystemPrompt = (data.wizard_system_prompt as string) || ''

              if (wizardSystemPrompt) {
                setActiveWizardContext(wizardSystemPrompt)
                const kickoff = `I'd like to add new drills to the "${suiteName}" suite.`
                setPendingDrillPrompt({ message: kickoff, systemContext: wizardSystemPrompt })
              }
            }
            break

          case 'fleet_progress':
            // Accumulate fleet progress events into a structured fleet_execution message.
            // Each event is appended to the phases array; the UI renders a collapsible panel.
            setMessages((prev: ChatMsg[]) => {
              const existing = prev.find(m => m.type === 'fleet_execution') as FleetExecutionMessage | undefined
              const event: FleetEvent = {
                ...data,
                type: data.type as string,
                timestamp: Date.now(),
                // Preserve rich data fields from SSE payload
                args: data.args || null,
                result: data.result !== undefined ? data.result : null,
                text: (data.text as string) || '',
              }

              if (existing) {
                const updated: FleetExecutionMessage = { ...existing, events: [...existing.events, event] }
                // Update current phase/status
                if (data.type === 'phase_start' || data.type === 'conversation_start') {
                  updated.currentPhase = data.phase as string
                  updated.currentAgent = data.agent as string
                } else if (data.type === 'fleet_complete') {
                  updated.status = 'complete'
                  updated.currentPhase = null
                }
                return prev.map(m => m.type === 'fleet_execution' ? updated : m)
              }

              // First event: create the fleet_execution message
              return [...prev, {
                type: 'fleet_execution',
                events: [event],
                currentPhase: (data.type === 'phase_start' || data.type === 'conversation_start') ? data.phase as string : null,
                currentAgent: (data.type === 'phase_start' || data.type === 'conversation_start') ? data.agent as string : null,
                status: 'running',
              } as FleetExecutionMessage]
            })
            break

          case 'retry':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'retry',
              attempt: data.attempt,
              maxRetries: data.maxRetries,
              reason: data.reason,
            }])
            break

          case 'error':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: (data.error as string) || (data.message as string) || 'Unknown error' }])
            break

          case 'error_info':
            setMessages((prev: ChatMsg[]) => [...prev, {
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
              setMessages((prev: ChatMsg[]) => {
                const last = prev[prev.length - 1]
                if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
                  return [...prev.slice(0, -1), { type: 'agent', content: finalText }]
                }
                return prev
              })
            }
            // Remove transient thinking messages (fleet_execution is kept as persistent)
            setMessages((prev: ChatMsg[]) => prev.filter(m => m.type !== 'thinking'))
            break

          default:
            break
        }
      },
      onError: (err) => {
        console.error('Chat stream error:', err)
        setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: err.message }])
        setIsStreaming(false)
      },
      onDone: () => {
        setIsStreaming(false)
        // Refresh sessions to pick up title updates
        setTimeout(() => loadSessions(), 1000)
      },
    })

    abortRef.current = controller
  }, [activeSessionId, activeWizardContext])

  // Process deferred fleet plan prompt (set by fleet_plan_redirect SSE event)
  useEffect(() => {
    if (pendingFleetPlanPrompt && !isStreaming) {
      const { message, systemContext } = pendingFleetPlanPrompt
      setPendingFleetPlanPrompt(null)
      sendMessage(message, { systemContext })
    }
  }, [pendingFleetPlanPrompt, isStreaming, sendMessage])

  // Process deferred drill prompt (set by drill_redirect SSE event)
  useEffect(() => {
    if (pendingDrillPrompt && !isStreaming) {
      const { message, systemContext } = pendingDrillPrompt
      setPendingDrillPrompt(null)
      sendMessage(message, { systemContext })
    }
  }, [pendingDrillPrompt, isStreaming, sendMessage])

  // Process pending chat message passed from another view (e.g., Fleet UI "Create Plan with AI Guide")
  useEffect(() => {
    if (pendingChatMessage && !isStreaming) {
      sendMessage(pendingChatMessage)
      if (onPendingChatMessageConsumed) {
        onPendingChatMessageConsumed()
      }
    }
  }, [pendingChatMessage, isStreaming, sendMessage, onPendingChatMessageConsumed])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (isStreaming && !isFleetMode) return

    // In fleet mode, send as human message to the fleet
    if (isFleetMode && input.trim()) {
      sendFleetHumanMessage(input)
      return
    }

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
  const autoResize = useCallback((el: HTMLTextAreaElement) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 200) + 'px'
  }, [])

  // Handle input changes for slash command popup
  const handleInputChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
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

  const handleSlashSelect = (cmd: string) => {
    setShowSlashPopup(false)
    setSlashIndex(0)
    setInput('')
    if (inputRef.current) {
      inputRef.current.style.height = 'auto'
    }
    sendMessage(cmd)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
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

  const toggleToolExpand = (index: number) => {
    setExpandedTools(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  const toggleRawView = (index: number) => {
    setRawViewIndices(prev => {
      const next = new Set(prev)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  const copyToClipboard = async (content: string, index: number) => {
    await navigator.clipboard.writeText(content)
    setCopiedIndex(index)
    setTimeout(() => setCopiedIndex(null), 2000)
  }

  const filteredSessions = useMemo(() => {
    if (!sessionFilter) return sessions
    const q = sessionFilter.toLowerCase()
    return sessions.filter(s => (s.title || s.id).toLowerCase().includes(q))
  }, [sessions, sessionFilter])

  const formatTimeAgo = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
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
  const renderToolCard = (msg: ChatMsg, index: number) => {
    const toolMsg = msg as ToolCallMessage | ToolResultMessage
    const isExpanded = expandedTools.has(index)
    const isCall = msg.type === 'tool_call'
    const name = (toolMsg as any).toolName || 'unknown'
    const data = isCall ? (toolMsg as ToolCallMessage).toolArgs : (toolMsg as ToolResultMessage).toolResult

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
        {isExpanded && !!data && (
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
        ) as React.ReactNode}
      </div>
    )
  }

  return (
    <>
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
                onClick={() => { setFleetDialogMessage(''); setShowFleetDialog(true) }}
                className="p-1.5 rounded-lg hover:bg-cyan-500/15 transition-colors"
                title="Start fleet session"
                style={{ color: 'var(--text-secondary)' }}
              >
                <Users size={16} className="text-cyan-400" />
              </button>
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
                      <div className="flex items-center gap-1.5">
                        {session.fleetKey && (
                          <Users size={12} className="text-cyan-400 flex-shrink-0" />
                        )}
                        <p
                          className="text-sm font-medium truncate"
                          style={{ color: activeSessionId === session.id ? 'var(--accent)' : 'var(--text-primary)' }}
                        >
                          {session.title || 'Untitled'}
                        </p>
                      </div>
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
        {/* Fleet session header */}
        {isFleetMode && fleetInfo && (
          <div className="flex items-center justify-between px-4 py-2" style={{ borderBottom: '1px solid var(--border-color)', background: 'rgba(6, 182, 212, 0.05)' }}>
            <div className="flex items-center gap-3">
              <Users size={16} className="text-cyan-400" />
              <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>{fleetInfo.fleet_name}</span>
              {fleetState && (
                <span className="flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full" style={{
                  background: fleetState.state === 'waiting_for_customer' ? 'rgba(234, 179, 8, 0.15)' : fleetState.state === 'processing' ? 'rgba(6, 182, 212, 0.15)' : 'rgba(107, 114, 128, 0.15)',
                  color: fleetState.state === 'waiting_for_customer' ? '#facc15' : fleetState.state === 'processing' ? '#22d3ee' : '#9ca3af',
                }}>
                  {fleetState.state === 'processing' && <Loader size={10} className="animate-spin" />}
                  {fleetState.state === 'waiting_for_customer' && '? '}
                  {fleetState.active_agent ? `@${fleetState.active_agent}` : fleetState.state}
                </span>
              )}
            </div>
            <button
              onClick={handleExitFleet}
              className="text-xs px-2 py-1 rounded hover:bg-red-500/10 text-red-400 transition-colors"
            >
              Exit Fleet
            </button>
          </div>
        )}
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
                      <span>&#x2715;</span> {String(msg.title)}
                    </div>
                    {!!msg.reason && <p className="text-sm text-gray-300 pl-4">{String(msg.reason)}</p>}
                    {!!msg.suggestion && (
                      <div className="pl-4">
                        <p className="text-sm font-medium text-yellow-400">Suggestion:</p>
                        <p className="text-sm text-yellow-300/80">{String(msg.suggestion)}</p>
                      </div>
                    )}
                    {!!msg.originalError && (
                      <details className="pl-4">
                        <summary className="text-xs text-gray-500 cursor-pointer hover:text-gray-400">Raw Error</summary>
                        <pre className="text-xs text-gray-500 mt-1 whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
                          {String(msg.originalError)}
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
                      Approve tool: <code className="bg-yellow-500/20 px-1.5 rounded">{String(msg.toolName)}</code>
                    </p>
                    {!!msg.options && (msg.options as unknown[]).length > 0 && (
                      <div className="flex gap-2 flex-wrap">
                        {(msg.options as unknown[]).map((opt, i) => (
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
                      <span>&#10003;</span> Auto-approved: <code className="bg-green-500/20 px-1.5 py-0.5 rounded font-mono text-xs">{msg.toolName as string}</code>
                    </span>
                  </div>
                )
              }

              if (msg.type === 'thinking') {
                return (
                  <div key={index} className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm w-fit bg-yellow-500/10 text-yellow-400 border border-yellow-500/20">
                    <Loader size={14} className="animate-spin" />
                    <span>{(msg.content as string) || 'Thinking...'}</span>
                  </div>
                )
              }

              if (msg.type === 'fleet_execution') {
                return <FleetExecutionPanel key={index} data={msg as FleetExecutionMessage} />
              }

              if (msg.type === 'fleet_message') {
                const fMsg = msg as FleetMessageItem
                const isHuman = fMsg.sender === 'customer'
                const isSystem = fMsg.sender === 'system'
                const color = getAgentColor(fMsg.sender)

                if (isHuman) {
                  return (
                    <div key={index} className="flex justify-end">
                      <div className="space-y-1 max-w-[80%]">
                        <div className="text-xs font-medium text-right" style={{ color: 'var(--text-muted)' }}>You</div>
                        <div className="chat-bubble-user p-3 rounded-lg">
                          <p className="text-sm whitespace-pre-wrap">{fMsg.text}</p>
                        </div>
                      </div>
                    </div>
                  )
                }

                return (
                  <div key={index} className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span
                        className="text-xs font-bold px-1.5 py-0.5 rounded"
                        style={{ background: color.bg, color: color.text, border: `1px solid ${color.border}` }}
                      >
                        @{fMsg.sender}
                      </span>
                      {isSystem && <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>system</span>}
                      {fMsg.mentions && fMsg.mentions.length > 0 && (
                        <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          &rarr; {fMsg.mentions.map(m => `@${m}`).join(', ')}
                        </span>
                      )}
                    </div>
                    <div
                      className="p-4 rounded-lg max-w-[90%]"
                      style={{
                        background: color.bg,
                        border: `1px solid ${color.border}`,
                      }}
                    >
                      <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{fMsg.text}</ReactMarkdown>
                      </div>
                    </div>
                  </div>
                )
              }

              if (msg.type === 'retry') {
                return (
                  <div key={index} className="flex items-center gap-2 px-3 py-2 my-1 text-sm">
                    <RotateCcw size={14} className="text-orange-400" />
                    <span className="text-orange-400 font-medium">Retry {msg.attempt as string}/{msg.maxRetries as string}:</span>
                    <span className="text-gray-400">{msg.reason as string}</span>
                  </div>
                )
              }

              return null
            })
          )}

          {/* Streaming indicator */}
          {isStreaming && !isFleetMode && messages.length > 0 && messages[messages.length - 1]?.type !== 'thinking' && messages[messages.length - 1]?.type !== 'fleet_execution' && (
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
                    } else if (isFleetMode && input.trim()) {
                      sendFleetHumanMessage(input)
                    } else if (!isStreaming && input.trim()) {
                      // Reuse slash validation from handleSubmit
                      if (input.startsWith('/') && !input.includes(' ')) return
                      sendMessage(input)
                    }
                    return
                  }
                  handleKeyDown(e)
                }}
                disabled={isStreaming && !isFleetMode}
                placeholder={
                  isFleetMode
                    ? fleetState?.state === 'waiting_for_customer'
                      ? `${fleetState.active_agent || 'An agent'} is waiting for your response...`
                      : fleetState?.state === 'processing'
                        ? `${fleetState.active_agent || 'Agent'} is working... You can still type.`
                        : 'Type a message to the team...'
                    : isStreaming
                      ? 'Agent is responding...'
                      : 'Type a message or / for commands...'
                }
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
              disabled={(isStreaming && !isFleetMode) || !input.trim()}
              className="px-4 py-2.5 bg-[#805AD5] hover:bg-[#6B46C1] text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Send size={18} />
            </button>
          </form>
        </div>
      </div>
    </div>

    {/* Fleet start dialog */}
    {showFleetDialog && (
      <FleetStartDialog
        onStart={handleFleetStart}
        onCancel={() => { setFleetDialogMessage(''); setShowFleetDialog(false) }}
        defaultMessage={fleetDialogMessage}
      />
    )}

    {/* Fleet template picker for bare /fleet-plan command */}
    {showTemplatePicker && (
      <FleetTemplatePicker
        onSelect={(templateKey) => {
          setShowTemplatePicker(false)
          sendMessage(`/fleet-plan ${templateKey}`)
        }}
        onCancel={() => setShowTemplatePicker(false)}
      />
    )}
    </>
  )
}
