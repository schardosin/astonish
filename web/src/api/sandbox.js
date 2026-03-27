// Sandbox API client functions for the Studio UI.

// --- Setup Wizard (existing) ---

/** Fetch sandbox platform detection status. */
export async function fetchSandboxStatus() {
  const res = await fetch('/api/sandbox/status')
  if (!res.ok) throw new Error(`Failed to fetch sandbox status: ${res.statusText}`)
  return res.json()
}

/** Fetch optional tools available for sandbox base template. */
export async function fetchOptionalTools() {
  const res = await fetch('/api/sandbox/optional-tools')
  if (!res.ok) throw new Error(`Failed to fetch optional tools: ${res.statusText}`)
  return res.json()
}

/**
 * Initialize the sandbox base template with selected tools.
 * Returns an SSE stream with progress events.
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
        buffer = lines.pop()

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

// --- Settings: Container & Template Management ---

/** Fetch extended sandbox details (Incus version, storage, counts). */
export async function fetchSandboxDetails() {
  const res = await fetch('/api/sandbox/details')
  if (!res.ok) throw new Error(`Failed to fetch sandbox details: ${res.statusText}`)
  return res.json()
}

/** Fetch all session containers. */
export async function fetchContainers() {
  const res = await fetch('/api/sandbox/containers')
  if (!res.ok) throw new Error(`Failed to fetch containers: ${res.statusText}`)
  return res.json()
}

/** Destroy a session container by session ID or container name. */
export async function deleteContainer(id) {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Prune orphaned containers. */
export async function pruneOrphans() {
  const res = await fetch('/api/sandbox/prune', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Fetch all registered templates. */
export async function fetchTemplates() {
  const res = await fetch('/api/sandbox/templates')
  if (!res.ok) throw new Error(`Failed to fetch templates: ${res.statusText}`)
  return res.json()
}

/** Fetch detailed info about a single template. */
export async function fetchTemplateInfo(name) {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}`)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Create a new template from @base. */
export async function createTemplate(name, description) {
  const res = await fetch('/api/sandbox/templates', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, description }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Delete a template. */
export async function deleteTemplate(name) {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Snapshot a template. */
export async function snapshotTemplate(name) {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}/snapshot`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Promote a template to replace @base. */
export async function promoteTemplate(name) {
  const res = await fetch(`/api/sandbox/templates/${encodeURIComponent(name)}/promote`, { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Refresh all templates with the current astonish binary. */
export async function refreshTemplates() {
  const res = await fetch('/api/sandbox/refresh', { method: 'POST' })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

// --- Port Exposure ---

/** Expose a port on a container through the reverse proxy. */
export async function exposePort(containerId, port) {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ port, base_domain: window.location.hostname }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}

/** Remove a port from the reverse proxy. */
export async function unexposePort(containerId, port) {
  const res = await fetch(`/api/sandbox/containers/${encodeURIComponent(containerId)}/expose/${port}`, {
    method: 'DELETE',
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || res.statusText)
  }
  return res.json()
}
