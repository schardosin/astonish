// Sandbox API client functions for the Studio Setup Wizard.

/**
 * Fetch sandbox platform detection status.
 * @returns {Promise<{platform: string, reason: string, sandboxEnabled: boolean, incusAvailable: boolean, baseTemplateExists: boolean}>}
 */
export async function fetchSandboxStatus() {
  const res = await fetch('/api/sandbox/status')
  if (!res.ok) throw new Error(`Failed to fetch sandbox status: ${res.statusText}`)
  return res.json()
}

/**
 * Fetch optional tools available for sandbox base template.
 * @returns {Promise<{tools: Array<{id: string, name: string, description: string, url: string, recommended: boolean, requiresNesting: boolean}>}>}
 */
export async function fetchOptionalTools() {
  const res = await fetch('/api/sandbox/optional-tools')
  if (!res.ok) throw new Error(`Failed to fetch optional tools: ${res.statusText}`)
  return res.json()
}

/**
 * Initialize the sandbox base template with selected tools.
 * Returns an SSE stream with progress events.
 *
 * @param {Object} params
 * @param {Object<string, boolean>} params.installTools - Map of tool IDs to install flags
 * @param {function(string)} params.onProgress - Called with each progress message
 * @param {function()} params.onDone - Called when initialization completes
 * @param {function(string)} params.onError - Called with error message on failure
 * @returns {{ abort: function }} Controller to cancel the request
 */
export function initSandbox({ installTools, onProgress, onDone, onError }) {
  const controller = new AbortController()

  fetch('/api/sandbox/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ installTools }),
    signal: controller.signal,
  })
    .then(async (res) => {
      if (!res.ok) {
        const text = await res.text()
        onError(text || res.statusText)
        return
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() // keep incomplete line in buffer

        let currentEvent = ''
        for (const line of lines) {
          if (line.startsWith('event: ')) {
            currentEvent = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const dataStr = line.slice(6)
            try {
              const data = JSON.parse(dataStr)
              if (currentEvent === 'progress') {
                onProgress(data.message || '')
              } else if (currentEvent === 'done') {
                onDone()
              } else if (currentEvent === 'error') {
                onError(data.error || 'Unknown error')
              }
            } catch {
              // ignore parse errors for incomplete data
            }
            currentEvent = ''
          }
        }
      }
    })
    .catch((err) => {
      if (err.name !== 'AbortError') {
        onError(err.message || 'Connection failed')
      }
    })

  return { abort: () => controller.abort() }
}
