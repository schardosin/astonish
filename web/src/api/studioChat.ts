/**
 * API client for Studio Chat
 */

const API_BASE = '/api/studio'

// --- Types ---

export interface ChatSession {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  messageCount: number
}

export interface SessionHistory {
  id: string
  title: string
  messages: ChatMessage[]
}

export interface ChatMessage {
  role: string
  content: string
  tool_calls?: ToolCall[]
  tool_results?: ToolResult[]
  [key: string]: unknown
}

export interface ToolCall {
  id: string
  name: string
  args: Record<string, unknown>
}

export interface ToolResult {
  tool_call_id: string
  content: string
}

export type SSEEventCallback = (eventType: string, data: Record<string, unknown>) => void
export type ErrorCallback = (error: Error) => void
export type DoneCallback = () => void

export interface ConnectChatParams {
  sessionId?: string
  message?: string
  systemContext?: string
  autoApprove?: boolean
  onEvent: SSEEventCallback
  onError?: ErrorCallback
  onDone?: DoneCallback
}

// --- API Functions ---

export async function fetchSessions(): Promise<ChatSession[]> {
  const response = await fetch(`${API_BASE}/sessions`)
  if (!response.ok) {
    throw new Error(`Failed to fetch sessions: ${response.statusText}`)
  }
  return response.json()
}

export async function fetchSessionHistory(id: string): Promise<SessionHistory> {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch session: ${response.statusText}`)
  }
  return response.json()
}

export async function deleteSession(id: string): Promise<void> {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete session: ${response.statusText}`)
  }
}

export function connectChat({ sessionId, message, systemContext, autoApprove, onEvent, onError, onDone }: ConnectChatParams): AbortController {
  const controller = new AbortController()

  const run = async () => {
    try {
      const body: Record<string, unknown> = {
        sessionId: sessionId || '',
        message: message || '',
        autoApprove: !!autoApprove,
      }
      if (systemContext) {
        body.systemContext = systemContext
      }
      const response = await fetch(`${API_BASE}/chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify(body),
      })

      if (!response.ok) {
        const text = await response.text()
        throw new Error(text || `HTTP ${response.status}`)
      }

      const reader = response.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split('\n\n')
        buffer = blocks.pop()!

        for (const block of blocks) {
          if (!block.trim()) continue
          const lines = block.split('\n')
          let eventType = 'message'
          let dataStr = ''

          for (const line of lines) {
            if (line.startsWith('event: ')) {
              eventType = line.slice(7).trim()
            } else if (line.startsWith('data: ')) {
              dataStr = line.slice(6)
            }
          }

          if (dataStr) {
            try {
              const data = JSON.parse(dataStr)
              onEvent(eventType, data)
            } catch (e) {
              console.error('Failed to parse SSE data:', e, dataStr)
            }
          }
        }
      }

      if (onDone) onDone()
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        if (onDone) onDone()
      } else {
        if (onError) onError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  run()
  return controller
}

export async function stopChat(sessionId: string): Promise<void> {
  try {
    await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stop`, {
      method: 'POST',
    })
  } catch (err) {
    console.warn('Failed to stop chat:', err)
  }
}
