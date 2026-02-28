/**
 * API client for Studio Chat
 */

const API_BASE = '/api/studio'

/**
 * Fetch all chat sessions (sorted by most recent)
 * @returns {Promise<Array<{id: string, title: string, createdAt: string, updatedAt: string, messageCount: number}>>}
 */
export async function fetchSessions() {
  const response = await fetch(`${API_BASE}/sessions`)
  if (!response.ok) {
    throw new Error(`Failed to fetch sessions: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Fetch a session's history (metadata + messages)
 * @param {string} id - Session ID
 * @returns {Promise<{id: string, title: string, messages: Array}>}
 */
export async function fetchSessionHistory(id) {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`)
  if (!response.ok) {
    throw new Error(`Failed to fetch session: ${response.statusText}`)
  }
  return response.json()
}

/**
 * Delete a chat session
 * @param {string} id - Session ID
 */
export async function deleteSession(id) {
  const response = await fetch(`${API_BASE}/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    throw new Error(`Failed to delete session: ${response.statusText}`)
  }
}

/**
 * Connect to the chat SSE stream. Returns an AbortController so the caller can cancel.
 * @param {object} params
 * @param {string} params.sessionId - Session ID (empty for new session)
 * @param {string} params.message - User message
 * @param {boolean} params.autoApprove - Auto-approve tool calls
 * @param {function} params.onEvent - Callback for each SSE event: (eventType, data) => void
 * @param {function} params.onError - Callback for errors: (error) => void
 * @param {function} params.onDone - Callback when stream completes
 * @returns {AbortController}
 */
export function connectChat({ sessionId, message, autoApprove, onEvent, onError, onDone }) {
  const controller = new AbortController()

  const run = async () => {
    try {
      const response = await fetch(`${API_BASE}/chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          sessionId: sessionId || '',
          message: message || '',
          autoApprove: !!autoApprove,
        }),
      })

      if (!response.ok) {
        const text = await response.text()
        throw new Error(text || `HTTP ${response.status}`)
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const blocks = buffer.split('\n\n')
        buffer = blocks.pop() // keep incomplete block

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
      if (err.name === 'AbortError') {
        if (onDone) onDone()
      } else {
        if (onError) onError(err)
      }
    }
  }

  run()
  return controller
}

/**
 * Stop an active chat stream on the server
 * @param {string} sessionId
 */
export async function stopChat(sessionId) {
  try {
    await fetch(`${API_BASE}/sessions/${encodeURIComponent(sessionId)}/stop`, {
      method: 'POST',
    })
  } catch (err) {
    console.warn('Failed to stop chat:', err)
  }
}
