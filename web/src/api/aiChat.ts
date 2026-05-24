// API functions for AI Chat panel

import type { InternetResult } from '../components/AIChatToolCards'
import { teamFetch } from './teamContext'

// API function to chat with AI
export async function sendChatMessage(message: string, context: string, currentYaml: string, selectedNodes: any[], history: Array<{role: string; content: string}>) {
  const response = await teamFetch('/api/ai/chat', {
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
  const response = await teamFetch('/api/ai/chat', {
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
    
    // Split on double-newline (SSE event boundary) to ensure we only
    // process complete events. Large payloads (e.g., full flow YAML) may
    // arrive across multiple TCP chunks — buffering until \n\n guarantees
    // we only JSON.parse complete data lines.
    const blocks = buffer.split('\n\n')
    buffer = blocks.pop()! // Last block might be incomplete — keep it

    for (const block of blocks) {
      if (!block.trim()) continue

      let eventType: string | null = null
      let dataStr: string | null = null

      for (const line of block.split('\n')) {
        if (line.startsWith('event: ')) {
          eventType = line.slice(7).trim()
        } else if (line.startsWith('data: ')) {
          dataStr = line.slice(6)
        }
      }

      if (eventType && dataStr) {
        try {
          const data = JSON.parse(dataStr)
          if (onEvent) {
            onEvent(eventType, data)
          }
        } catch (e) {
          console.error('Failed to parse SSE JSON:', e)
        }
      }
    }
  }
}

// API function to search for tools in the store using AI semantic search
export async function searchToolsInStore(requirement: string) {
  const response = await teamFetch('/api/ai/tool-search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requirement }),
  })
  return response.json()
}

// API function to search for MCP servers on the internet (uses AI knowledge)
export async function searchToolsOnInternet(requirement: string) {
  const response = await teamFetch('/api/ai/tool-search-internet', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ requirement }),
  })
  return response.json()
}

// API function to classify user intent using LLM
export async function classifyIntent(message: string, tools: Array<{ name: string; [key: string]: any }> = []) {
  const response = await teamFetch('/api/ai/classify-intent', {
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
  const response = await teamFetch('/api/ai/url-extract', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  return response.json()
}

// API function to install a tool from the store
export async function installToolFromStore(toolId: string, serverName: string, env: Record<string, string> = {}) {
  const response = await teamFetch(`/api/mcp-store/${encodeURIComponent(toolId)}/install`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ serverName, env }),
  })
  return response.json()
}

// API function to install MCP server from internet search result
export async function installInternetMCP(result: InternetResult, env: Record<string, string> = {}) {
  const response = await teamFetch('/api/mcp-internet-install', {
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
