import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { Send, Plus, Trash2, MessageSquare, ChevronRight, ChevronDown, Loader, Square, Copy, Check, Code, RotateCcw, Wrench, Clock, Search, Users, Info, FileText, Globe, ListChecks, AppWindow } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { markdownComponents } from './chat/markdownComponents'
import { fetchSessions, fetchSessionHistory, deleteSession, connectChat, stopChat, fetchSessionStatus, connectChatStream } from '../api/studioChat'
import type { ChatSession } from '../api/studioChat'
import { startFleetSession, connectFleetStream, sendFleetMessage, stopFleetSession, fetchFleetSessions } from '../api/fleetChat'
import type { FleetSession } from '../api/fleetChat'
import HomePage from './HomePage'
import type { FleetMessageItem, ChatMsg, FleetInfo, FleetStateInfo, DeferredPrompt, FleetExecutionMessage, FleetEvent, AgentMessage, ToolCallMessage, ToolResultMessage, BrowserHandoffMessage, SubTaskExecutionMessage, SubTaskEvent, SubTaskInfo, PlanMessage, PlanStepInfo, SessionArtifact, ArtifactMessage, AppPreviewMessage, AppSavedMessage, DistillPreviewMessage, DistillSavedMessage } from './chat/chatTypes'
import { getAgentColor } from './chat/chatTypes'
import FleetStartDialog from './chat/FleetStartDialog'
import FleetTemplatePicker from './chat/FleetTemplatePicker'
import FleetExecutionPanel from './chat/FleetExecutionPanel'
import TaskPlanPanel from './chat/TaskPlanPanel'
import PlanPanel from './chat/PlanPanel'
import FilePanel from './chat/FilePanel'
import TodoPanel from './chat/TodoPanel'
import AppsPanel from './chat/AppsPanel'
import UsagePopover from './chat/UsagePopover'
import type { TokenUsage } from './chat/UsagePopover'
import BrowserView from './chat/BrowserView'
import ArtifactCard from './chat/ArtifactCard'
import DistillPreviewCard from './chat/DistillPreviewCard'
import AppPreviewCard from './chat/AppPreviewCard'
import AppCodeIndicator from './chat/AppCodeIndicator'
import ResultCard from './chat/ResultCard'

// Extended ChatSession with optional fleet fields coming from the sidebar
interface SidebarSession extends ChatSession {
  fleetKey?: string
  fleetName?: string
}

// Web search tool name patterns (matches tool results from search MCP servers)
const WEB_SEARCH_PATTERNS = ['search', 'web_search', 'web-search', 'web_fetch', 'scrape', 'crawl', 'extract', 'browse']

// Extract URLs from a tool result value (deeply searches objects/arrays/strings)
function extractUrlsFromResult(result: unknown): string[] {
  const urls: string[] = []
  const urlRegex = /https?:\/\/[^\s"',\]}>]+/g

  function walk(val: unknown) {
    if (typeof val === 'string') {
      const matches = val.match(urlRegex)
      if (matches) urls.push(...matches)
    } else if (Array.isArray(val)) {
      val.forEach(walk)
    } else if (val && typeof val === 'object') {
      Object.values(val as Record<string, unknown>).forEach(walk)
    }
  }
  walk(result)

  // Deduplicate and filter out API/internal URLs
  return [...new Set(urls)].filter(u =>
    !u.includes('api.tavily') &&
    !u.includes('api.firecrawl') &&
    !u.includes('localhost') &&
    !u.includes('127.0.0.1')
  )
}

// Collect web source URLs from tool_result messages preceding an agent message
function collectSourceUrls(messages: ChatMsg[], agentIndex: number): string[] {
  const urls: string[] = []
  // Walk backwards from the agent message, collecting URLs from web search tool results
  for (let i = agentIndex - 1; i >= 0; i--) {
    const m = messages[i]
    // Stop at user messages or other agent messages (only look at the current "turn")
    if (m.type === 'user' || m.type === 'agent') break

    if (m.type === 'tool_result') {
      const tr = m as ToolResultMessage
      const toolName = String(tr.toolName || '').toLowerCase()
      const isWebTool = WEB_SEARCH_PATTERNS.some(p => toolName.includes(p))
      if (isWebTool && tr.toolResult) {
        urls.push(...extractUrlsFromResult(tr.toolResult))
      }
    }
  }
  return [...new Set(urls)]
}

// Inline citation pill component
function SourceCitations({ urls }: { urls: string[] }) {
  const [expanded, setExpanded] = useState(false)

  if (urls.length === 0) return null

  // Extract domain for display
  const getDomain = (url: string) => {
    try { return new URL(url).hostname.replace('www.', '') }
    catch { return url }
  }

  // Favicon URL via Google's favicon service
  const getFavicon = (url: string) => {
    try {
      const hostname = new URL(url).hostname
      return `https://www.google.com/s2/favicons?domain=${hostname}&sz=16`
    } catch { return '' }
  }

  return (
    <div className="mt-2">
      <button
        onClick={() => setExpanded(!expanded)}
        className="source-citation-pill flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs cursor-pointer transition-colors hover:opacity-80"
        style={{
          background: 'var(--bg-tertiary)',
          border: '1px solid var(--border-color)',
          color: 'var(--text-secondary)',
        }}
      >
        {/* Stacked favicons (show up to 3) */}
        <span className="flex items-center -space-x-1">
          {urls.slice(0, 3).map((url, i) => (
            <img
              key={i}
              src={getFavicon(url)}
              alt=""
              className="w-4 h-4 rounded-full border"
              style={{ borderColor: 'var(--bg-tertiary)' }}
              onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
            />
          ))}
        </span>
        <Globe size={12} style={{ color: 'var(--text-muted)' }} />
        <span>{urls.length} source{urls.length !== 1 ? 's' : ''}</span>
      </button>

      {expanded && (
        <div
          className="mt-1.5 p-2 rounded-lg space-y-1 text-xs"
          style={{
            background: 'var(--bg-tertiary)',
            border: '1px solid var(--border-color)',
          }}
        >
          {urls.map((url, i) => (
            <div key={i} className="flex items-center gap-2">
              <img
                src={getFavicon(url)}
                alt=""
                className="w-3.5 h-3.5 rounded shrink-0"
                onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
              />
              <a
                href={url}
                target="_blank"
                rel="noopener noreferrer"
                className="truncate hover:underline"
                style={{ color: 'var(--accent)' }}
              >
                {getDomain(url)}
              </a>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default function StudioChat({ theme, initialSessionId, pendingChatMessage, onPendingChatMessageConsumed, onSessionChange }: { theme: string; initialSessionId?: string | null; pendingChatMessage?: { message: string; systemContext?: string } | null; onPendingChatMessageConsumed?: () => void; onSessionChange?: (sessionId: string | null) => void }) {
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
  const [activeAppId, setActiveAppId] = useState<string | null>(null) // ID of app currently being refined

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
  const [expandedCodeIndices, setExpandedCodeIndices] = useState<Set<number>>(new Set())
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)

  // File panel state
  const [filePanelOpen, setFilePanelOpen] = useState(false)
  const [sessionArtifacts, setSessionArtifacts] = useState<SessionArtifact[]>([])
  const [filePanelInitialPath, setFilePanelInitialPath] = useState<string | null>(null)

  // Todo panel state
  const [todoPanelOpen, setTodoPanelOpen] = useState(false)

  // Apps panel state
  const [appsPanelOpen, setAppsPanelOpen] = useState(false)

  // Token usage state (from API-reported UsageMetadata)
  const [tokenUsage, setTokenUsage] = useState<TokenUsage>({ inputTokens: 0, outputTokens: 0, totalTokens: 0 })
  const [sessionStartTime, setSessionStartTime] = useState<number | null>(null)

  // Refs
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLTextAreaElement | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const streamingTextRef = useRef('')
  const isNearBottomRef = useRef(true)
  const [showScrollButton, setShowScrollButton] = useState(false)

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

  // Pre-compute which artifact paths should be embedded inside the ResultCard
  // (the final "Response" card) instead of rendered as inline ArtifactCards.
  // When the session has a final-result message AND artifacts, all turn artifacts
  // are shown embedded in the ResultCard, and their inline cards are suppressed.
  const embeddedArtifactPaths = useMemo(() => {
    const paths = new Set<string>()
    if (isStreaming || sessionArtifacts.length === 0) return paths
    // Check if there's a final result message (same logic as in the render)
    let hasFinalResult = false
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i]
      if (m.type === 'agent' && !(m as AgentMessage)._streaming && m.content.length > 500) {
        const hasLaterAgent = messages.slice(i + 1).some(x => x.type === 'agent')
        const hasToolBefore = messages.slice(0, i).some(x =>
          x.type === 'tool_call' || x.type === 'tool_result' ||
          x.type === 'subtask_execution' || x.type === 'fleet_execution'
        )
        if (!hasLaterAgent && hasToolBefore) hasFinalResult = true
        break
      }
    }
    if (!hasFinalResult) return paths
    // Collect all artifact paths from this session
    for (const a of sessionArtifacts) paths.add(a.path)
    return paths
  }, [messages, isStreaming, sessionArtifacts])

  // Wrapper to keep URL in sync with active session
  const changeSession = useCallback((sessionId: string | null, { userInitiated = false } = {}) => {
    setActiveSessionId(sessionId)
    setActiveAppId(null) // clear active app refinement state on session switch
    setAppsPanelOpen(false)
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
        // Check if this session has an active background chat runner
        try {
          const status = await fetchSessionStatus(initialSessionId)
          if (status.running) {
            changeSession(initialSessionId)
            reconnectToChatRunner(initialSessionId)
            return
          }
        } catch {
          // Status endpoint may fail; fall through to history
        }
        // Not a fleet session or active runner, load as regular history
        loadSessionHistory(initialSessionId)
      }
    }
    init()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-scroll on new messages — only if user is near the bottom
  useEffect(() => {
    if (scrollRef.current && isNearBottomRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
    // Show scroll button when not at bottom and there are messages
    if (!isNearBottomRef.current && messages.length > 0) {
      setShowScrollButton(true)
    }
  }, [messages])

  // Track scroll position to detect if user scrolled up
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const handleScroll = () => {
      const threshold = 80
      const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - threshold
      isNearBottomRef.current = atBottom
      if (atBottom) {
        setShowScrollButton(false)
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, []) // Re-attaches if scrollRef changes — but ref is stable

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      isNearBottomRef.current = true
      setShowScrollButton(false)
    }
  }, [])

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
      const dataAny = data as Record<string, any>

      // Extract artifacts from the session detail response
      if (dataAny.artifacts && Array.isArray(dataAny.artifacts)) {
        setSessionArtifacts(dataAny.artifacts as SessionArtifact[])
      } else {
        setSessionArtifacts([])
      }

      // Restore cumulative token usage from the persisted session data.
      // The backend sums UsageMetadata across all LLM responses in the transcript.
      if (dataAny.totalUsage) {
        setTokenUsage({
          inputTokens: dataAny.totalUsage.inputTokens || 0,
          outputTokens: dataAny.totalUsage.outputTokens || 0,
          totalTokens: dataAny.totalUsage.totalTokens || 0,
        })
      } else {
        setTokenUsage({ inputTokens: 0, outputTokens: 0, totalTokens: 0 })
      }

      // If the response includes fleet messages, convert them to the fleet_message format
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
        // Map API messages to frontend ChatMsg types.
        // The API uses StudioMessage with generic fields (content, toolName);
        // some message types need field remapping for the frontend types.
        const apiMessages = (data.messages || []) as Array<Record<string, any>>
        const mapped: ChatMsg[] = apiMessages.map(m => {
          if (m.type === 'artifact' && m.content) {
            return { type: 'artifact', path: m.content, toolName: m.toolName || 'write_file' } as ArtifactMessage
          }
          if (m.type === 'distill_preview') {
            return {
              type: 'distill_preview',
              yaml: m.yaml || '',
              flowName: m.flowName || '',
              description: m.description || '',
              tags: m.tags || [],
              explanation: m.explanation || '',
            } as DistillPreviewMessage
          }
          if (m.type === 'distill_saved') {
            return {
              type: 'distill_saved',
              filePath: m.filePath || '',
              runCommand: m.runCommand || '',
            } as DistillSavedMessage
          }
          if (m.type === 'app_preview') {
            return {
              type: 'app_preview',
              code: m.code || '',
              title: m.title || 'App Preview',
              description: m.description || '',
              version: m.version || 1,
              appId: m.appId || undefined,
            } as AppPreviewMessage
          }
          return m as unknown as ChatMsg
        })
        setMessages(mapped)
        // Restore active app state from session history
        const appPreviews = mapped.filter((m): m is AppPreviewMessage => m.type === 'app_preview')
        if (appPreviews.length > 0) {
          const lastApp = appPreviews[appPreviews.length - 1]
          if (lastApp.appId) setActiveAppId(lastApp.appId)
        }
      }
    } catch (err: any) {
      console.error('Failed to load session history:', err)
      setMessages([])
      setSessionArtifacts([])
    } finally {
      setIsLoadingHistory(false)
    }
  }

  // Reconnect to an active background chat runner. The runner streams all
  // buffered events (catch-up) followed by live events. This uses the same
  // event handler as sendMessage so all event types are handled identically.
  const reconnectToChatRunner = useCallback((sessionId: string) => {
    setMessages([])
    setSessionArtifacts([])
    setIsStreaming(true)
    setSessionStartTime(Date.now())
    streamingTextRef.current = ''

    const controller = connectChatStream({
      sessionId,
      onEvent: (eventType, data) => {
        // Reuse the exact same event handling as sendMessage's onEvent
        switch (eventType) {
          case 'session':
            if (data.sessionId) {
              changeSession(data.sessionId as string)
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
            if (data.name === 'browser_request_human' && data.result && typeof data.result === 'object' && (data.result as Record<string, unknown>).vnc_proxy_url) {
              const result = data.result as Record<string, unknown>
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'browser_handoff',
                vncProxyUrl: String(result.vnc_proxy_url || ''),
                pageUrl: String(result.page_url || ''),
                pageTitle: String(result.page_title || ''),
                reason: String(result.message || 'Human assistance needed'),
              } as BrowserHandoffMessage])
            } else {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'tool_result', toolName: data.name, toolResult: data.result }])
            }
            break

          case 'image':
            if (data.data && data.mimeType) {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'image', data: data.data, mimeType: data.mimeType }])
            }
            break

          case 'artifact':
            if (data.path) {
              const fileName = (data.path as string).split('/').pop() || 'file'
              const ext = fileName.includes('.') ? fileName.split('.').pop()?.toLowerCase() || '' : ''
              const fileType = ext === 'md' ? 'Markdown' : ext === 'py' ? 'Python' : ext === 'go' ? 'Go' : ext === 'js' ? 'JavaScript' : ext === 'ts' ? 'TypeScript' : ext || 'File'
              setSessionArtifacts(prev => {
                if (prev.some(a => a.path === data.path)) return prev
                return [...prev, { path: data.path as string, fileName, fileType, toolName: (data.tool_name as string) || 'write_file' }]
              })
              // Also add inline artifact message to the chat thread
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'artifact',
                path: data.path as string,
                toolName: (data.tool_name as string) || 'write_file',
              } as ArtifactMessage])
            }
            break

          case 'flow_output':
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

          case 'approval':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'approval', toolName: data.tool, options: data.options }])
            break

          case 'auto_approved':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'auto_approved', toolName: data.tool }])
            break

          case 'thinking':
            if (data.text) {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'thinking', content: data.text }])
            }
            break

          case 'retry':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'retry', attempt: data.attempt, maxRetries: data.maxRetries, reason: data.reason }])
            break

          case 'error':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'error', content: (data.error as string) || (data.message as string) || 'Unknown error' }])
            break

          case 'error_info':
            setMessages((prev: ChatMsg[]) => [...prev, { type: 'error_info', title: data.title, reason: data.reason, suggestion: data.suggestion, originalError: data.originalError }])
            break

          case 'distill_preview':
            // Finalize any streaming text first
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
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'distill_preview',
              yaml: data.yaml as string,
              flowName: data.flowName as string,
              description: data.description as string,
              tags: (data.tags as string[]) || [],
              explanation: data.explanation as string || '',
            } as DistillPreviewMessage])
            break

          case 'app_preview':
            // Finalize any streaming text first
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
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'app_preview',
              code: data.code as string,
              title: data.title as string || 'App Preview',
              description: data.description as string || '',
              version: (data.version as number) || 1,
              appId: data.appId as string || undefined,
            } as AppPreviewMessage])
            if (data.appId) setActiveAppId(data.appId as string)
            break

          case 'app_done':
          case 'app_saved':
            setActiveAppId(null)
            if (data.name) {
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'app_saved',
                name: data.name as string || '',
                path: data.path as string || '',
              } as AppSavedMessage])
            }
            window.dispatchEvent(new Event('astonish:apps-updated'))
            break

          case 'distill_saved':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'distill_saved',
              filePath: data.filePath as string,
              runCommand: data.runCommand as string,
            } as DistillSavedMessage])
            // Notify App to refresh the flows list
            window.dispatchEvent(new Event('astonish:flows-updated'))
            break

          case 'done':
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
            break

          case 'subtask_progress': {
            const stEventType = data.event_type as string

            // ── Plan events ──
            if (stEventType === 'plan_announced') {
              const steps: PlanStepInfo[] = ((data.plan_steps as Array<{ name: string; description: string }>) || []).map(s => ({
                name: s.name,
                description: s.description,
                status: 'pending' as const,
              }))
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'plan',
                goal: (data.plan_goal as string) || '',
                steps,
              } as PlanMessage])
              break
            }

            if (stEventType === 'plan_step_update') {
              const stepName = data.step_name as string
              const stepStatus = data.step_status as PlanStepInfo['status']
              setMessages((prev: ChatMsg[]) => {
                for (let i = prev.length - 1; i >= 0; i--) {
                  if (prev[i].type === 'plan') {
                    const plan = prev[i] as PlanMessage
                    const updatedSteps = plan.steps.map(s =>
                      s.name === stepName ? { ...s, status: stepStatus } : s
                    )
                    return prev.map((m, idx) => idx === i ? { ...plan, steps: updatedSteps } : m)
                  }
                }
                return prev
              })
              break
            }

            // ── Delegation events ──
            const stEvent: SubTaskEvent = {
              type: stEventType,
              task_name: data.task_name as string | undefined,
              status: data.status as string | undefined,
              duration: data.duration as string | undefined,
              error: data.error as string | undefined,
              tool_name: data.tool_name as string | undefined,
              tool_args: data.tool_args,
              tool_result: data.tool_result,
              text: data.text as string | undefined,
              tasks: data.tasks as SubTaskInfo[] | undefined,
              timestamp: Date.now(),
            }
            setMessages((prev: ChatMsg[]) => {
              let existingIdx = -1
              for (let i = prev.length - 1; i >= 0; i--) {
                if (prev[i].type === 'subtask_execution' && (prev[i] as SubTaskExecutionMessage).status === 'running') {
                  existingIdx = i
                  break
                }
              }
              const existing = existingIdx >= 0 ? prev[existingIdx] as SubTaskExecutionMessage : undefined
              if (existing) {
                const updated: SubTaskExecutionMessage = { ...existing, events: [...existing.events, stEvent] }
                if (stEventType === 'delegation_complete') {
                  updated.status = (data.status as string) || 'complete'
                }
                if (stEventType === 'delegation_start' && stEvent.tasks) {
                  updated.tasks = stEvent.tasks
                }
                return prev.map((m, i) => i === existingIdx ? updated : m)
              }
              const tasks: SubTaskInfo[] = (stEventType === 'delegation_start' && stEvent.tasks) ? stEvent.tasks : []
              return [...prev, {
                type: 'subtask_execution',
                events: [stEvent],
                tasks,
                status: 'running',
              } as SubTaskExecutionMessage]
            })
            break
          }

          case 'usage': {
            const input = (data.input_tokens as number) || 0
            const output = (data.output_tokens as number) || 0
            const total = (data.total_tokens as number) || 0
            setTokenUsage(prev => ({
              inputTokens: prev.inputTokens + input,
              outputTokens: prev.outputTokens + output,
              totalTokens: prev.totalTokens + total,
            }))
            break
          }

          default:
            break
        }
      },
      onError: (err) => {
        console.error('Chat reconnect stream error:', err)
        setIsStreaming(false)
      },
      onDone: () => {
        // Check for dangling partial streaming text (same as sendMessage)
        setMessages((prev: ChatMsg[]) => {
          const last = prev[prev.length - 1]
          if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
            const finalText = streamingTextRef.current || (last as AgentMessage).content
            streamingTextRef.current = ''
            const finalized = prev.map((m, i) =>
              i === prev.length - 1 ? { type: 'agent', content: finalText } as ChatMsg : m
            )
            return [...finalized, {
              type: 'error',
              content: 'The model stopped responding unexpectedly. You can send a follow-up message to continue.',
            } as ChatMsg]
          }
          return prev
        })
        setIsStreaming(false)
        setTimeout(() => loadSessions(), 1000)
      },
    })

    abortRef.current = controller
  }, [changeSession])

  const handleSelectSession = useCallback(async (sessionId: string) => {
    // Cancel any active stream
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    setIsStreaming(false)
    setFilePanelOpen(false)
    setSessionArtifacts([])
    setFilePanelInitialPath(null)
    setTodoPanelOpen(false)
    setTokenUsage({ inputTokens: 0, outputTokens: 0, totalTokens: 0 })
    setSessionStartTime(null)

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

    // Check if this session has an active background runner — if so, reconnect
    // to the live SSE stream instead of loading static history.
    try {
      const status = await fetchSessionStatus(sessionId)
      if (status.running) {
        reconnectToChatRunner(sessionId)
        return
      }
    } catch {
      // Status endpoint may fail if chat manager not initialized; fall through to history
    }

    await loadSessionHistory(sessionId)
  }, [sessions, isFleetMode, connectToFleetStream, changeSession, reconnectToChatRunner])

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
    setSessionArtifacts([])
    setFilePanelOpen(false)
    setFilePanelInitialPath(null)
    setTodoPanelOpen(false)
    setTokenUsage({ inputTokens: 0, outputTokens: 0, totalTokens: 0 })
    setSessionStartTime(null)
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
    // Disconnect the SSE viewer
    if (abortRef.current) {
      abortRef.current.abort()
      abortRef.current = null
    }
    // Stop the background runner (this is the actual kill switch)
    if (isFleetMode && fleetSessionId) {
      stopFleetSession(fleetSessionId)
    } else if (activeSessionId) {
      stopChat(activeSessionId) // calls POST /api/studio/sessions/{id}/stop which stops the background runner
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
    // Reset scroll position — user expects to see the conversation flow
    isNearBottomRef.current = true
    setShowScrollButton(false)
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

    // Reset scroll position — user expects to see the conversation flow
    isNearBottomRef.current = true
    setShowScrollButton(false)

    // Add user message to chat (unless it's a slash command or internal action)
    if (!userMsg.startsWith('/') && !userMsg.startsWith('__distill_') && !userMsg.startsWith('__app_')) {
      setMessages((prev: ChatMsg[]) => [...prev, { type: 'user', content: userMsg }])
    }

    setInput('')
    if (inputRef.current) {
      inputRef.current.style.height = 'auto'
    }
    setIsStreaming(true)
    setSessionStartTime(Date.now())
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
            // Detect browser handoff with VNC viewer
            if (data.name === 'browser_request_human' && data.result && typeof data.result === 'object' && (data.result as Record<string, unknown>).vnc_proxy_url) {
              const result = data.result as Record<string, unknown>
              const vncUrl = String(result.vnc_proxy_url || '')
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'browser_handoff',
                vncProxyUrl: vncUrl,
                pageUrl: String(result.page_url || ''),
                pageTitle: String(result.page_title || ''),
                reason: String(result.message || 'Human assistance needed'),
              } as BrowserHandoffMessage])
            } else {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'tool_result', toolName: data.name, toolResult: data.result }])
            }
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

          case 'artifact':
            if (data.path) {
              const fileName = (data.path as string).split('/').pop() || 'file'
              const ext = fileName.includes('.') ? fileName.split('.').pop()?.toLowerCase() || '' : ''
              const fileType = ext === 'md' ? 'Markdown' : ext === 'py' ? 'Python' : ext === 'go' ? 'Go' : ext === 'js' ? 'JavaScript' : ext === 'ts' ? 'TypeScript' : ext || 'File'
              setSessionArtifacts(prev => {
                if (prev.some(a => a.path === data.path)) return prev
                return [...prev, { path: data.path as string, fileName, fileType, toolName: (data.tool_name as string) || 'write_file' }]
              })
              // Also add inline artifact message to the chat thread
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'artifact',
                path: data.path as string,
                toolName: (data.tool_name as string) || 'write_file',
              } as ArtifactMessage])
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

          case 'system':
            if (data.content) {
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'system',
                content: data.content as string,
              }])
            }
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
            if (data.text) {
              setMessages((prev: ChatMsg[]) => [...prev, { type: 'thinking', content: data.text }])
            }
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

          case 'subtask_progress': {
            // Handle plan events and sub-task progress events.
            // Plan events create/update a PlanMessage; delegation events manage SubTaskExecutionMessage.
            const eventType = data.event_type as string

            // ── Plan events ──
            if (eventType === 'plan_announced') {
              const steps: PlanStepInfo[] = ((data.plan_steps as Array<{ name: string; description: string }>) || []).map(s => ({
                name: s.name,
                description: s.description,
                status: 'pending' as const,
              }))
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'plan',
                goal: (data.plan_goal as string) || '',
                steps,
              } as PlanMessage])
              break
            }

            if (eventType === 'plan_step_update') {
              const stepName = data.step_name as string
              const stepStatus = data.step_status as PlanStepInfo['status']
              setMessages((prev: ChatMsg[]) => {
                // Find the most recent plan message
                for (let i = prev.length - 1; i >= 0; i--) {
                  if (prev[i].type === 'plan') {
                    const plan = prev[i] as PlanMessage
                    const updatedSteps = plan.steps.map(s =>
                      s.name === stepName ? { ...s, status: stepStatus } : s
                    )
                    return prev.map((m, idx) => idx === i ? { ...plan, steps: updatedSteps } : m)
                  }
                }
                return prev
              })
              break
            }

            // ── Delegation events (existing subtask_execution handling) ──
            const event: SubTaskEvent = {
              type: eventType,
              task_name: data.task_name as string | undefined,
              status: data.status as string | undefined,
              duration: data.duration as string | undefined,
              error: data.error as string | undefined,
              tool_name: data.tool_name as string | undefined,
              tool_args: data.tool_args,
              tool_result: data.tool_result,
              text: data.text as string | undefined,
              tasks: data.tasks as SubTaskInfo[] | undefined,
              timestamp: Date.now(),
            }

            setMessages((prev: ChatMsg[]) => {
              // Find the latest subtask_execution message that is still running (search from end)
              let existingIdx = -1
              for (let i = prev.length - 1; i >= 0; i--) {
                if (prev[i].type === 'subtask_execution' && (prev[i] as SubTaskExecutionMessage).status === 'running') {
                  existingIdx = i
                  break
                }
              }
              const existing = existingIdx >= 0 ? prev[existingIdx] as SubTaskExecutionMessage : undefined

              if (existing) {
                const updated: SubTaskExecutionMessage = { ...existing, events: [...existing.events, event] }
                // Update status on delegation_complete
                if (eventType === 'delegation_complete') {
                  updated.status = (data.status as string) || 'complete'
                }
                // Update tasks list on delegation_start (if somehow received after creation)
                if (eventType === 'delegation_start' && event.tasks) {
                  updated.tasks = event.tasks
                }
                return prev.map((m, i) => i === existingIdx ? updated : m)
              }

              // First event: create the subtask_execution message
              const tasks: SubTaskInfo[] = (eventType === 'delegation_start' && event.tasks) ? event.tasks : []
              return [...prev, {
                type: 'subtask_execution',
                events: [event],
                tasks,
                status: 'running',
              } as SubTaskExecutionMessage]
            })
            break
          }

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

          case 'distill_preview':
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
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'distill_preview',
              yaml: data.yaml as string,
              flowName: data.flowName as string,
              description: data.description as string,
              tags: (data.tags as string[]) || [],
              explanation: data.explanation as string || '',
            } as DistillPreviewMessage])
            break

          case 'app_preview':
            // Finalize any streaming text first
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
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'app_preview',
              code: data.code as string,
              title: data.title as string || 'App Preview',
              description: data.description as string || '',
              version: (data.version as number) || 1,
              appId: data.appId as string || undefined,
            } as AppPreviewMessage])
            if (data.appId) setActiveAppId(data.appId as string)
            break

          case 'app_done':
          case 'app_saved':
            setActiveAppId(null)
            if (data.name) {
              setMessages((prev: ChatMsg[]) => [...prev, {
                type: 'app_saved',
                name: data.name as string || '',
                path: data.path as string || '',
              } as AppSavedMessage])
            }
            window.dispatchEvent(new Event('astonish:apps-updated'))
            break

          case 'distill_saved':
            setMessages((prev: ChatMsg[]) => [...prev, {
              type: 'distill_saved',
              filePath: data.filePath as string,
              runCommand: data.runCommand as string,
            } as DistillSavedMessage])
            // Notify App to refresh the flows list
            window.dispatchEvent(new Event('astonish:flows-updated'))
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
            break

          case 'usage': {
            const input = (data.input_tokens as number) || 0
            const output = (data.output_tokens as number) || 0
            const total = (data.total_tokens as number) || 0
            setTokenUsage(prev => ({
              inputTokens: prev.inputTokens + input,
              outputTokens: prev.outputTokens + output,
              totalTokens: prev.totalTokens + total,
            }))
            break
          }

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
        // Check for dangling partial streaming text — if the stream ended
        // without a proper 'done' event (e.g., server crash, network drop),
        // finalize whatever text was received and show an error.
        setMessages((prev: ChatMsg[]) => {
          const last = prev[prev.length - 1]
          if (last && last.type === 'agent' && (last as AgentMessage)._streaming) {
            const finalText = streamingTextRef.current || (last as AgentMessage).content
            streamingTextRef.current = ''
            const finalized = prev.map((m, i) =>
              i === prev.length - 1 ? { type: 'agent', content: finalText } as ChatMsg : m
            )
            return [...finalized, {
              type: 'error',
              content: 'The model stopped responding unexpectedly. You can send a follow-up message to continue.',
            } as ChatMsg]
          }
          return prev
        })
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

  // Process pending chat message passed from another view (e.g., Fleet UI "Create Plan with AI Guide", Apps "Improve with AI")
  useEffect(() => {
    if (pendingChatMessage && !isStreaming) {
      // If there's an active session and the pending message needs a fresh session
      // (has systemContext, e.g. app refinement), clear the session first.
      // sendMessage with activeSessionId=null will create a new session.
      if (activeSessionId && pendingChatMessage.systemContext) {
        changeSession(null)
        return // re-runs on next render when activeSessionId is null
      }
      sendMessage(pendingChatMessage.message, { systemContext: pendingChatMessage.systemContext })
      if (onPendingChatMessageConsumed) {
        onPendingChatMessageConsumed()
      }
    }
  }, [pendingChatMessage, isStreaming, sendMessage, onPendingChatMessageConsumed, activeSessionId, changeSession])

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
      <div className="flex-1 flex flex-col overflow-hidden min-w-0">
        {/* Toolbar bar — always visible */}
        <div
          className="flex items-center justify-end gap-1.5 px-3 py-1.5 shrink-0"
          style={{ borderBottom: '1px solid var(--border-color)' }}
        >
          {/* Todo button — shows plan steps in side panel */}
          <button
            onClick={() => { setTodoPanelOpen(!todoPanelOpen); if (!todoPanelOpen) { setFilePanelOpen(false); setAppsPanelOpen(false) } }}
            className="flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors"
            style={{
              background: todoPanelOpen ? 'var(--accent-bg, rgba(59, 130, 246, 0.15))' : 'transparent',
              color: todoPanelOpen ? 'var(--accent-color, #60a5fa)' : 'var(--text-secondary)',
              border: todoPanelOpen ? '1px solid var(--accent-border, rgba(59, 130, 246, 0.3))' : '1px solid transparent',
            }}
            title="Todo / Plan"
          >
            <ListChecks size={13} />
            <span>Todo</span>
            {messages.some(m => m.type === 'plan') && (
              <span className="px-1 py-0 rounded text-[10px] font-medium" style={{
                background: 'var(--accent-bg, rgba(59, 130, 246, 0.15))',
                color: 'var(--accent-color, #60a5fa)',
              }}>
                {(() => {
                  const plans = messages.filter(m => m.type === 'plan') as PlanMessage[]
                  const plan = plans[plans.length - 1]
                  const done = plan.steps.filter(s => s.status === 'complete').length
                  return `${done}/${plan.steps.length}`
                })()}
              </span>
            )}
          </button>

          {/* Files button — shown when session has artifacts */}
          {sessionArtifacts.length > 0 && (
            <button
              onClick={() => { setFilePanelOpen(!filePanelOpen); setFilePanelInitialPath(null); if (!filePanelOpen) { setTodoPanelOpen(false); setAppsPanelOpen(false) } }}
              className="flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors"
              style={{
                background: filePanelOpen ? 'var(--accent-bg, rgba(59, 130, 246, 0.15))' : 'transparent',
                color: filePanelOpen ? 'var(--accent-color, #60a5fa)' : 'var(--text-secondary)',
                border: filePanelOpen ? '1px solid var(--accent-border, rgba(59, 130, 246, 0.3))' : '1px solid transparent',
              }}
              title={`${sessionArtifacts.length} file(s) generated`}
            >
              <FileText size={13} />
              <span>Files</span>
              <span className="px-1 py-0 rounded text-[10px] font-medium" style={{
                background: 'var(--accent-bg, rgba(59, 130, 246, 0.15))',
                color: 'var(--accent-color, #60a5fa)',
              }}>
                {sessionArtifacts.length}
              </span>
            </button>
          )}

          {/* Apps button — shown when session has app previews */}
          {messages.some(m => m.type === 'app_preview') && (
            <button
              onClick={() => { setAppsPanelOpen(!appsPanelOpen); if (!appsPanelOpen) { setTodoPanelOpen(false); setFilePanelOpen(false) } }}
              className="flex items-center gap-1.5 px-2 py-1 rounded text-xs transition-colors"
              style={{
                background: appsPanelOpen ? 'var(--accent-bg, rgba(59, 130, 246, 0.15))' : 'transparent',
                color: appsPanelOpen ? 'var(--accent-color, #60a5fa)' : 'var(--text-secondary)',
                border: appsPanelOpen ? '1px solid var(--accent-border, rgba(59, 130, 246, 0.3))' : '1px solid transparent',
              }}
              title="Generated apps"
            >
              <AppWindow size={13} />
              <span>Apps</span>
              <span className="px-1 py-0 rounded text-[10px] font-medium" style={{
                background: 'var(--accent-bg, rgba(59, 130, 246, 0.15))',
                color: 'var(--accent-color, #60a5fa)',
              }}>
                {(() => {
                  const appPreviews = messages.filter(m => m.type === 'app_preview') as AppPreviewMessage[]
                  const uniqueApps = new Set(appPreviews.map(a => a.appId || a.title))
                  return uniqueApps.size
                })()}
              </span>
            </button>
          )}

          {/* Usage popover — shows token counts */}
          <UsagePopover
            usage={tokenUsage}
            isStreaming={isStreaming}
            sessionStartTime={sessionStartTime}
          />
        </div>
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
        {/* Messages Area (with scroll-to-bottom button) */}
        <div className="flex-1 relative overflow-hidden">
        <div ref={scrollRef} className="absolute inset-0 overflow-y-auto p-4 space-y-4">
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
                // Detect if this is a "final result" — long final agent message after tool activity
                const isFinalResult = !isStreaming &&
                  !(msg as AgentMessage)._streaming &&
                  msg.content.length > 500 &&
                  // App code fences must always go through the fence-splitting path
                  !msg.content.includes('```astonish-app') &&
                  // Must be the last agent message in the list
                  !messages.slice(index + 1).some(m => m.type === 'agent') &&
                  // Must have tool activity somewhere before it
                  messages.slice(0, index).some(m =>
                    m.type === 'tool_call' || m.type === 'tool_result' ||
                    m.type === 'subtask_execution' || m.type === 'fleet_execution'
                  )

                // Collect web source URLs for citation pill
                const sourceUrls = !(msg as AgentMessage)._streaming ? collectSourceUrls(messages, index) : []

                if (isFinalResult) {
                  return (
                    <div key={index} className="space-y-1">
                      <div className="text-xs font-medium" style={{ color: 'var(--text-muted)' }}>Agent</div>
                      <ResultCard
                        content={msg.content}
                        showRaw={rawViewIndices.has(index)}
                        onToggleRaw={() => toggleRawView(index)}
                        artifacts={sessionArtifacts.length > 0 ? sessionArtifacts : undefined}
                        sessionId={activeSessionId}
                        onOpenFileInPanel={(path) => {
                          setFilePanelInitialPath(path)
                          setFilePanelOpen(true)
                        }}
                      />
                      <SourceCitations urls={sourceUrls} />
                    </div>
                  )
                }

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
                      ) : (() => {
                        // Split out astonish-app code fences so they render as a stable
                        // AppCodeIndicator outside the ReactMarkdown tree. This avoids
                        // the jank of ReactMarkdown recreating the component on every
                        // streaming text update.
                        const appFenceRe = /```astonish-app\s*\n([\s\S]*?)(?:\n```|$)/
                        const match = msg.content.match(appFenceRe)
                        if (!match) {
                          return (
                            <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                              <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{msg.content}</ReactMarkdown>
                            </div>
                          )
                        }
                        const fenceStart = match.index!
                        const fenceEnd = fenceStart + match[0].length
                        const before = msg.content.slice(0, fenceStart).trimEnd()
                        const appCode = match[1]
                        const after = msg.content.slice(fenceEnd).trimStart()
                        return (
                          <>
                            {before && (
                              <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{before}</ReactMarkdown>
                              </div>
                            )}
                            <AppCodeIndicator
                              streaming={!!(msg as AgentMessage)._streaming}
                              code={appCode}
                              expanded={expandedCodeIndices.has(index)}
                              onToggle={() => setExpandedCodeIndices(prev => {
                                const next = new Set(prev)
                                if (next.has(index)) next.delete(index)
                                else next.add(index)
                                return next
                              })}
                            />
                            {after && (
                              <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{after}</ReactMarkdown>
                              </div>
                            )}
                          </>
                        )
                      })()}
                    </div>
                    {sourceUrls.length > 0 && <SourceCitations urls={sourceUrls} />}
                  </div>
                )
              }

              if (msg.type === 'tool_call' || msg.type === 'tool_result') {
                return renderToolCard(msg, index)
              }

              if (msg.type === 'browser_handoff') {
                const handoff = msg as BrowserHandoffMessage
                return (
                  <BrowserView
                    key={index}
                    data={handoff}
                    theme={theme}
                    onDone={() => {
                      // Replace with a completed marker
                      setMessages((prev: ChatMsg[]) => prev.map((m, i) =>
                        i === index ? { ...m, type: 'browser_handoff' } as BrowserHandoffMessage : m
                      ))
                    }}
                  />
                )
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
                const text = (msg.content as string) || ''
                if (!text) return null
                return (
                  <div key={index} className="thinking-note">
                    {text}
                  </div>
                )
              }

              if (msg.type === 'fleet_execution') {
                return <FleetExecutionPanel key={index} data={msg as FleetExecutionMessage} />
              }

              if (msg.type === 'plan') {
                return <PlanPanel key={index} data={msg as PlanMessage} />
              }

              if (msg.type === 'subtask_execution') {
                return <TaskPlanPanel key={index} data={msg as SubTaskExecutionMessage} />
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
                        <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{fMsg.text}</ReactMarkdown>
                      </div>
                    </div>
                  </div>
                )
              }

              if (msg.type === 'system') {
                return (
                  <div key={index} className="my-2 p-4 rounded-lg" style={{
                    background: 'rgba(99, 102, 241, 0.08)',
                    border: '1px solid rgba(99, 102, 241, 0.2)',
                  }}>
                    <div className="flex items-center gap-2 mb-2">
                      <Info size={14} style={{ color: 'rgba(129, 140, 248, 0.9)' }} />
                      <span className="text-xs font-medium" style={{ color: 'rgba(129, 140, 248, 0.9)' }}>System</span>
                    </div>
                    <div style={{ color: 'var(--text-primary)' }} className="markdown-body text-sm">
                      <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{msg.content as string}</ReactMarkdown>
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

              if (msg.type === 'artifact') {
                const artifactMsg = msg as ArtifactMessage
                // Suppress inline card when this artifact is embedded in the ResultCard
                if (embeddedArtifactPaths.has(artifactMsg.path)) return null
                return (
                  <div key={index}>
                    <ArtifactCard
                      data={artifactMsg}
                      sessionId={activeSessionId}
                      onOpenInPanel={(path) => {
                        setFilePanelInitialPath(path)
                        setFilePanelOpen(true)
                      }}
                    />
                  </div>
                )
              }

              if (msg.type === 'app_preview') {
                const appMsg = msg as AppPreviewMessage
                // Collect all app_preview versions for this app (by appId, fallback to title)
                const allVersions = messages.filter(
                  (m): m is AppPreviewMessage => m.type === 'app_preview' && (
                    appMsg.appId
                      ? (m as AppPreviewMessage).appId === appMsg.appId
                      : (m as AppPreviewMessage).title === appMsg.title
                  )
                )
                // Only render the card on the LAST version's message to avoid duplicates
                const lastVersion = allVersions[allVersions.length - 1]
                if (lastVersion && lastVersion !== appMsg) {
                  return null // Skip earlier versions — they'll be shown via version nav
                }
                const versionIdx = allVersions.length - 1 // Default to latest
                // Check if this app is actively being refined
                const isActive = activeAppId != null && (appMsg.appId === activeAppId)
                return (
                  <div key={index}>
                    <AppPreviewCard
                      data={appMsg}
                      versions={allVersions.length > 1 ? allVersions : undefined}
                      versionIndex={versionIdx}
                      isActive={isActive}
                      onSave={isActive ? (name: string) => sendMessage(`__app_save__:${name}`) : undefined}
                      sessionId={activeSessionId}
                    />
                  </div>
                )
              }

              if (msg.type === 'distill_preview') {
                const previewMsg = msg as DistillPreviewMessage
                // A preview card is active (shows action buttons) when:
                // 1. It's the last distill_preview in the message list
                // 2. There's no distill_saved or cancel after it (review not concluded)
                const isLastPreview = (() => {
                  for (let j = index + 1; j < messages.length; j++) {
                    const m = messages[j]
                    if (m.type === 'distill_preview' || m.type === 'distill_saved') return false
                  }
                  return true
                })()
                return (
                  <div key={index}>
                    <DistillPreviewCard
                      data={previewMsg}
                      isActive={isLastPreview}
                      onSave={() => sendMessage('__distill_save__')}
                      onRequestChanges={() => {
                        if (inputRef.current) {
                          inputRef.current.focus()
                          inputRef.current.placeholder = 'Describe what you want to change in the flow...'
                        }
                      }}
                      onCancel={() => sendMessage('__distill_cancel__')}
                    />
                  </div>
                )
              }

              if (msg.type === 'distill_saved') {
                const savedMsg = msg as DistillSavedMessage
                return (
                  <div key={index} className="my-3 rounded-xl overflow-hidden w-full max-w-2xl"
                    style={{ border: '1px solid var(--border-color)', background: 'var(--bg-secondary)', boxShadow: 'var(--shadow-soft)' }}>
                    <div className="px-4 py-3">
                      <div className="flex items-center gap-2 mb-2">
                        <Check size={16} style={{ color: 'var(--success)' }} />
                        <span className="text-sm font-semibold" style={{ color: 'var(--success)' }}>Flow Saved</span>
                      </div>
                      <div className="text-xs mb-2" style={{ color: 'var(--text-secondary)' }}>
                        Saved to: <code className="px-1.5 py-0.5 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--accent)' }}>{savedMsg.filePath}</code>
                      </div>
                      <div className="text-xs mb-2" style={{ color: 'var(--text-muted)' }}>Run with:</div>
                      <div className="flex items-center gap-2">
                        <pre className="flex-1 p-2 rounded-lg text-[11px] overflow-x-auto" style={{ background: 'var(--bg-tertiary)', color: 'var(--text-secondary)' }}>
                          <code>{savedMsg.runCommand}</code>
                        </pre>
                        <button
                          onClick={() => navigator.clipboard.writeText(savedMsg.runCommand)}
                          className="flex-shrink-0 p-1.5 rounded transition-colors cursor-pointer"
                          style={{ color: 'var(--accent)' }}
                          title="Copy command"
                        >
                          <Copy size={13} />
                        </button>
                      </div>
                    </div>
                  </div>
                )
              }

              if (msg.type === 'app_saved') {
                const savedMsg = msg as AppSavedMessage
                return (
                  <div key={index} className="my-3 rounded-xl overflow-hidden w-full max-w-2xl"
                    style={{ border: '1px solid var(--border-color)', background: 'var(--bg-secondary)', boxShadow: 'var(--shadow-soft)' }}>
                    <div className="px-4 py-3">
                      <div className="flex items-center gap-2 mb-2">
                        <Check size={16} style={{ color: 'var(--success)' }} />
                        <span className="text-sm font-semibold" style={{ color: 'var(--success)' }}>App Saved</span>
                      </div>
                      <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                        Saved as <code className="px-1.5 py-0.5 rounded" style={{ background: 'var(--bg-tertiary)', color: 'var(--accent)' }}>{savedMsg.name}</code> — view it in the Apps tab.
                      </div>
                    </div>
                  </div>
                )
              }

              return null
            })
          )}

          {/* Streaming indicator */}
          {isStreaming && !isFleetMode && messages.length > 0 && messages[messages.length - 1]?.type !== 'fleet_execution' && (
            <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-purple-500/10 border border-purple-500/20 w-fit">
              <Loader size={14} className="text-purple-400 animate-spin" />
              <span className="text-xs text-purple-300">Processing...</span>
            </div>
          )}
        </div>
        {/* Scroll to bottom button */}
        {showScrollButton && (
          <button
            onClick={scrollToBottom}
            className="absolute bottom-3 right-5 z-10 p-2 rounded-full shadow-lg transition-all hover:scale-105 cursor-pointer"
            style={{
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border-color)',
              color: 'var(--text-secondary)',
            }}
            title="Scroll to bottom"
          >
            <ChevronDown size={16} />
          </button>
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

      {/* File Panel — right side split panel for viewing artifacts */}
      {filePanelOpen && sessionArtifacts.length > 0 && (
        <FilePanel
          artifacts={sessionArtifacts}
          initialPath={filePanelInitialPath}
          sessionId={activeSessionId}
          onClose={() => setFilePanelOpen(false)}
        />
      )}

      {/* Todo Panel — right side split panel for plan steps */}
      {todoPanelOpen && (
        <TodoPanel
          messages={messages}
          onClose={() => setTodoPanelOpen(false)}
        />
      )}

      {/* Apps Panel — right side split panel for generated apps */}
      {appsPanelOpen && (
        <AppsPanel
          messages={messages}
          activeAppId={activeAppId}
          onClose={() => setAppsPanelOpen(false)}
        />
      )}
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
