// API functions for AI Chat panel

import type { InternetResult } from '../components/AIChatToolCards'

// API function to chat with AI
export async function sendChatMessage(message: string, context: string, currentYaml: string, selectedNodes: any[], history: Array<{role: string; content: string}>) {
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

// API function to chat with AI using streaming (Server-Sent Events)
export async function sendChatMessageStream(message: string, context: string, currentYaml: string, selectedNodes: any[], history: Array<{role: string; content: string}>, onEvent: (eventType: string, data: Record<string, any>) => void) {
  const response = await fetch('/api/ai/chat', {
    method: 'POST',
    headers: { 
      'Content-Type': 'application/json',
      'Accept': 'text/event-stream'
    },
    body: JSON.stringify({
      message,
      context,
      currentYaml,
      selectedNodes,
      history,
    }),
  })

  if (!response.ok) {
    const errText = await response.text()
    throw new Error(errText || response.statusText)
  }

  // Handle SSE stream
  const reader = response.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() as string // Last line might be incomplete

    let currentEvent: string | null = null
    
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        currentEvent = line.slice(7).trim()
      } else if (line.startsWith('data: ')) {
        const dataStr = line.slice(6)
        try {
          const data = JSON.parse(dataStr)
          if (onEvent && currentEvent) {
            onEvent(currentEvent, data)
          }
        } catch (e) {
          console.error('Failed to parse SSE JSON:', e)
        }
        currentEvent = null // Reset for next pair
      }
    }
  }
}

// API function to search for tools in the store using AI semantic search
export async function searchToolsInStore(requirement: string) {
  const response = await fetch('/api/ai/tool-search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requirement }),
  })
  return response.json()
}

// API function to search for MCP servers on the internet (uses AI knowledge)
export async function searchToolsOnInternet(requirement: string) {
  const response = await fetch('/api/ai/tool-search-internet', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requirement }),
  })
  return response.json()
}

// API function to classify user intent using LLM
export async function classifyIntent(message: string, tools: Array<{ name: string; [key: string]: any }> = []) {
  const response = await fetch('/api/ai/classify-intent', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ 
      message,
      tools: tools.map(t => t.name)
    }),
  })
  return response.json()
}

// API function to extract MCP server info from a URL (uses tavily-extract)
export async function extractMCPServerFromURL(url: string) {
  const response = await fetch('/api/ai/url-extract', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  return response.json()
}

// API function to install a tool from the store
export async function installToolFromStore(toolId: string, serverName: string, env: Record<string, string> = {}) {
  const response = await fetch(`/api/mcp-store/${encodeURIComponent(toolId)}/install`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverName, env }),
  })
  return response.json()
}

// API function to install MCP server from internet search result
export async function installInternetMCP(result: InternetResult, env: Record<string, string> = {}) {
  const response = await fetch('/api/mcp-internet-install', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      name: result.name,
      command: result.command,
      args: result.args || [],
      env: env,
    }),
  })
  return response.json()
}
