import { useState, useMemo } from 'react'
import { ChevronRight, ChevronDown, Loader, Check, Wrench, ListTodo, RotateCcw } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { SubTaskExecutionMessage, SubTaskEvent } from './chatTypes'

// Task status derived from events
interface TaskState {
  name: string
  description: string
  events: SubTaskEvent[]
  status: 'pending' | 'running' | 'complete' | 'failed'
  retrying?: boolean
  duration?: string
  error?: string
}

// Collapsible task plan panel showing real-time progress of delegate_tasks sub-agents.
// Displays a structured task breakdown with per-task tool calls and text output,
// similar to how Perplexity Computer shows its research steps.
export default function TaskPlanPanel({ data }: { data: SubTaskExecutionMessage }) {
  const [expanded, setExpanded] = useState(true)
  const [expandedTasks, setExpandedTasks] = useState<Record<string, boolean>>({})
  const [expandedContent, setExpandedContent] = useState<Set<string>>(new Set())

  // Derive task states from the event stream
  const taskStates = useMemo(() => {
    // Start with the task plan from delegation_start
    const taskMap: Record<string, TaskState> = {}
    const taskOrder: string[] = []

    // Initialize from the tasks array (populated on delegation_start)
    for (const t of data.tasks) {
      if (!taskMap[t.name]) {
        taskMap[t.name] = {
          name: t.name,
          description: t.description,
          events: [],
          status: 'pending',
        }
        taskOrder.push(t.name)
      }
    }

    // Process events to update task states
    for (const evt of data.events) {
      if (evt.type === 'delegation_start' || evt.type === 'delegation_complete') continue

      const taskName = evt.task_name || '_unknown'
      if (!taskMap[taskName]) {
        // Task appeared without delegation_start (edge case)
        taskMap[taskName] = {
          name: taskName,
          description: '',
          events: [],
          status: 'pending',
        }
        taskOrder.push(taskName)
      }

      const task = taskMap[taskName]
      task.events.push(evt)

      if (evt.type === 'task_start') {
        task.status = 'running'
        task.retrying = false
      } else if (evt.type === 'task_retry') {
        task.status = 'running'
        task.retrying = true
        task.error = undefined
        task.duration = undefined
      } else if (evt.type === 'task_complete') {
        task.status = 'complete'
        task.retrying = false
        task.duration = evt.duration
      } else if (evt.type === 'task_failed') {
        task.status = 'failed'
        task.retrying = false
        task.duration = evt.duration
        task.error = evt.error
      }
    }

    return taskOrder.map(name => taskMap[name])
  }, [data.events, data.tasks])

  const completedCount = taskStates.filter(t => t.status === 'complete').length
  const totalCount = taskStates.length

  const toggleTask = (name: string) => {
    setExpandedTasks(prev => ({ ...prev, [name]: !prev[name] }))
  }

  const toggleContent = (eventKey: string) => {
    setExpandedContent(prev => {
      const next = new Set(prev)
      if (next.has(eventKey)) next.delete(eventKey)
      else next.add(eventKey)
      return next
    })
  }

  const statusIcon = (status: string) => {
    if (status === 'running') return <Loader size={12} className="animate-spin text-amber-400" />
    if (status === 'complete') return <Check size={12} className="text-green-400" />
    if (status === 'failed') return <span className="text-red-400 text-xs font-bold">!</span>
    return <span className="text-gray-500 text-xs">&#x2022;</span>
  }

  const TRUNCATE_THRESHOLD = 800

  const renderCardContent = (cardData: unknown, eventKey: string) => {
    if (cardData == null || cardData === undefined) return null
    const text = typeof cardData === 'string' ? cardData : JSON.stringify(cardData, null, 2)
    if (!text) return null

    const isTruncatable = text.length > TRUNCATE_THRESHOLD
    const isFullyExpanded = expandedContent.has(eventKey)
    const displayText = (isTruncatable && !isFullyExpanded) ? text.slice(0, TRUNCATE_THRESHOLD) : text

    return (
      <div className="px-3 pb-2">
        <div className="relative">
          <pre
            className="text-xs whitespace-pre-wrap break-words font-mono p-2 rounded"
            style={{
              background: 'rgba(0,0,0,0.3)',
              color: 'var(--text-secondary)',
              maxHeight: isFullyExpanded ? 'none' : '200px',
              overflowY: 'hidden',
            }}
          >
            {displayText}
          </pre>
          {isTruncatable && !isFullyExpanded && (
            <div
              className="absolute bottom-0 left-0 right-0 h-10 flex items-end justify-center rounded-b"
              style={{ background: 'linear-gradient(transparent, rgba(0,0,0,0.5))' }}
            >
              <button
                onClick={() => toggleContent(eventKey)}
                className="text-[10px] text-amber-400 hover:text-amber-300 px-2 py-0.5 mb-1 rounded bg-black/50 cursor-pointer"
              >
                Show more ({Math.ceil(text.length / 1000)}k chars)
              </button>
            </div>
          )}
          {isTruncatable && isFullyExpanded && (
            <div className="flex justify-center mt-1">
              <button
                onClick={() => toggleContent(eventKey)}
                className="text-[10px] text-amber-400 hover:text-amber-300 px-2 py-0.5 rounded bg-black/30 cursor-pointer"
              >
                Show less
              </button>
            </div>
          )}
        </div>
      </div>
    )
  }

  // Render a tool call/result card
  const renderToolCard = (evt: SubTaskEvent, eventKey: string) => {
    const isCall = evt.type === 'task_tool_call'
    const name = evt.tool_name || 'unknown'
    const cardData = isCall ? (evt.tool_args || null) : (evt.tool_result || null)

    return (
      <div
        key={eventKey}
        className="my-1.5 rounded-lg overflow-hidden"
        style={{ border: '1px solid var(--border-color)', background: 'rgba(255,255,255,0.03)' }}
      >
        <div className="flex items-center gap-2 px-3 py-1.5">
          <Wrench size={12} className={isCall ? 'text-purple-400' : 'text-green-400'} />
          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
            {isCall ? 'Tool Call' : 'Tool Result'}: <code className="bg-purple-500/15 text-purple-300 px-1 py-0.5 rounded text-[11px]">{name}</code>
          </span>
        </div>
        {renderCardContent(cardData, eventKey)}
      </div>
    )
  }

  // Render task text output
  const renderTaskText = (evt: SubTaskEvent, eventKey: string) => {
    const textContent = evt.text || ''
    if (!textContent) return null

    return (
      <div key={eventKey} className="my-1.5">
        <div
          className="p-3 rounded-lg text-sm"
          style={{
            background: 'rgba(255,255,255,0.05)',
            border: '1px solid var(--border-color)',
          }}
        >
          <div className="markdown-body text-xs" style={{ color: 'var(--text-primary)' }}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{textContent}</ReactMarkdown>
          </div>
        </div>
      </div>
    )
  }

  // Render a single event within a task
  const renderTaskEvent = (evt: SubTaskEvent, taskIdx: number, evtIdx: number) => {
    const eventKey = `subtask-${taskIdx}-${evtIdx}`
    if (evt.type === 'task_start' || evt.type === 'task_complete' || evt.type === 'task_failed') {
      return null // Status shown in header
    }
    if (evt.type === 'task_tool_call' || evt.type === 'task_tool_result') {
      return renderToolCard(evt, eventKey)
    }
    if (evt.type === 'task_text') {
      return renderTaskText(evt, eventKey)
    }
    return null
  }

  return (
    <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 overflow-hidden text-sm">
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-amber-400 hover:bg-amber-500/10 transition-colors cursor-pointer"
      >
        {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        {data.status === 'running' && <Loader size={14} className="animate-spin" />}
        {(data.status === 'complete' || data.status === 'partial') && <Check size={14} className="text-green-400" />}
        {data.status === 'error' && <span className="text-red-400 font-bold">!</span>}
        <ListTodo size={14} />
        <span className="font-medium">Task Plan</span>
        <span className="text-amber-400/60 text-xs ml-auto">
          {completedCount}/{totalCount} task{totalCount !== 1 ? 's' : ''}
        </span>
      </button>

      {/* Task list */}
      {expanded && (
        <div className="border-t border-amber-500/20 px-2 py-1">
          {taskStates.map((task, taskIdx) => (
            <div key={task.name} className="my-1">
              {/* Task header */}
              <button
                onClick={() => toggleTask(task.name)}
                className="w-full flex items-center gap-2 px-2 py-1.5 rounded hover:bg-amber-500/10 transition-colors cursor-pointer"
              >
                {expandedTasks[task.name] ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                {statusIcon(task.status)}
                <span className="text-gray-200 font-medium text-xs">{task.name}</span>
                {task.description && (
                  <span className="text-gray-500 text-xs truncate max-w-[300px]">{task.description}</span>
                )}
                {task.status === 'running' && (
                  <span className="text-amber-400/60 text-xs ml-auto flex items-center gap-1">
                    {task.retrying && <RotateCcw size={10} />}
                    {task.retrying ? 'retrying...' : 'running'}
                  </span>
                )}
                {task.status === 'complete' && task.duration && (
                  <span className="text-gray-500 text-xs ml-auto">{task.duration}</span>
                )}
                {task.status === 'failed' && (
                  <span className="text-red-400 text-xs ml-auto truncate max-w-[200px]">{task.error || 'failed'}</span>
                )}
              </button>

              {/* Task contents — collapsed by default, click to expand */}
              {expandedTasks[task.name] && (
                <div className="ml-4 pl-2 border-l border-amber-500/15 pb-1">
                  {task.events.length === 0 && task.status === 'pending' && (
                    <div className="text-xs text-gray-500 px-2 py-1">Waiting to start...</div>
                  )}
                  {task.events.map((evt, evtIdx) => renderTaskEvent(evt, taskIdx, evtIdx))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
