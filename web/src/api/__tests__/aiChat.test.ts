import { describe, it, expect, vi, afterEach } from 'vitest'
import { sendChatMessageStream } from '../aiChat'

// Helper: encode SSE blocks into a ReadableStream with controlled chunking.
function makeSSEStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk))
      }
      controller.close()
    },
  })
}

function mockFetchWithStream(stream: ReadableStream<Uint8Array>) {
  return vi.fn().mockResolvedValue({
    ok: true,
    body: stream,
  })
}

describe('sendChatMessageStream', () => {
  const originalFetch = globalThis.fetch

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  const defaultArgs = {
    message: 'test',
    context: 'create_flow',
    currentYaml: '',
    selectedNodes: [] as any[],
    history: [] as Array<{ role: string; content: string }>,
  }

  it('parses a single small SSE event in one chunk', async () => {
    const stream = makeSSEStream([
      'event: complete\ndata: {"message":"done","action":"info"}\n\n',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    expect(events).toHaveLength(1)
    expect(events[0]).toEqual({
      type: 'complete',
      data: { message: 'done', action: 'info' },
    })
  })

  it('handles multiple events in a single chunk', async () => {
    const stream = makeSSEStream([
      'event: chunk\ndata: {"content":"Hello "}\n\nevent: chunk\ndata: {"content":"world"}\n\nevent: complete\ndata: {"message":"Hello world","action":"info"}\n\n',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    expect(events).toHaveLength(3)
    expect(events[0]).toEqual({ type: 'chunk', data: { content: 'Hello ' } })
    expect(events[1]).toEqual({ type: 'chunk', data: { content: 'world' } })
    expect(events[2]).toEqual({ type: 'complete', data: { message: 'Hello world', action: 'info' } })
  })

  it('reconstructs an event split across two chunks (mid-data line)', async () => {
    // Split in the middle of the data line
    const stream = makeSSEStream([
      'event: complete\nda',
      'ta: {"message":"ok"}\n\n',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    expect(events).toHaveLength(1)
    expect(events[0]).toEqual({ type: 'complete', data: { message: 'ok' } })
  })

  it('handles a large payload (~80KB) split across many chunks — THE BUG REGRESSION TEST', async () => {
    // Generate a large YAML string (~80KB) simulating a real flow YAML in proposedYaml
    const largeYaml = 'name: test_flow\nnodes:\n' + Array.from({ length: 2000 }, (_, i) =>
      `  - name: step_${i}\n    type: agent\n    model: gemini-2.0-flash\n    prompt: "Do task ${i} which involves processing data and returning results"\n`
    ).join('')

    const completePayload = JSON.stringify({
      message: 'Here is your modified flow.',
      proposedYaml: largeYaml,
      action: 'apply_yaml',
    })

    // Verify payload is actually large (>50KB)
    expect(completePayload.length).toBeGreaterThan(50000)

    // Build the full SSE event as it would come from the server
    const fullSSE = `event: complete\ndata: ${completePayload}\n\n`

    // Split into chunks of ~10KB each (simulating TCP fragmentation)
    const chunkSize = 10000
    const chunks: string[] = []
    for (let i = 0; i < fullSSE.length; i += chunkSize) {
      chunks.push(fullSSE.slice(i, i + chunkSize))
    }
    expect(chunks.length).toBeGreaterThan(5) // Should be 8-10 chunks

    const stream = makeSSEStream(chunks)
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    // The critical assertion: we should get exactly ONE complete event
    // with the FULL payload intact (not silently dropped)
    expect(events).toHaveLength(1)
    expect(events[0].type).toBe('complete')
    expect(events[0].data.action).toBe('apply_yaml')
    expect(events[0].data.message).toBe('Here is your modified flow.')
    expect(events[0].data.proposedYaml).toBe(largeYaml)
    expect(events[0].data.proposedYaml.length).toBe(largeYaml.length)
  })

  it('handles streaming chunks followed by a large complete event', async () => {
    // Simulates real-world: several small chunk events then one large complete
    const largeYaml = 'name: flow\n' + 'step: x\n'.repeat(5000)
    const completePayload = JSON.stringify({ message: 'Done', proposedYaml: largeYaml, action: 'apply_yaml' })

    const smallEvents = 'event: chunk\ndata: {"content":"Processing..."}\n\nevent: chunk\ndata: {"content":" Done."}\n\n'
    const largeEvent = `event: complete\ndata: ${completePayload}\n\n`

    // Small events arrive in one chunk, large event split across multiple
    const fullSSE = smallEvents + largeEvent
    const chunkSize = 8000
    const chunks: string[] = []
    for (let i = 0; i < fullSSE.length; i += chunkSize) {
      chunks.push(fullSSE.slice(i, i + chunkSize))
    }

    const stream = makeSSEStream(chunks)
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    expect(events).toHaveLength(3)
    expect(events[0]).toEqual({ type: 'chunk', data: { content: 'Processing...' } })
    expect(events[1]).toEqual({ type: 'chunk', data: { content: ' Done.' } })
    expect(events[2].type).toBe('complete')
    expect(events[2].data.proposedYaml).toBe(largeYaml)
  })

  it('handles empty/whitespace blocks between events', async () => {
    const stream = makeSSEStream([
      '\n\nevent: chunk\ndata: {"content":"hi"}\n\n\n\nevent: complete\ndata: {"message":"bye"}\n\n',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    expect(events).toHaveLength(2)
    expect(events[0]).toEqual({ type: 'chunk', data: { content: 'hi' } })
    expect(events[1]).toEqual({ type: 'complete', data: { message: 'bye' } })
  })

  it('does not emit an event for trailing incomplete data (no \\n\\n terminator)', async () => {
    // Stream ends without the final \n\n — incomplete event should be discarded
    const stream = makeSSEStream([
      'event: chunk\ndata: {"content":"ok"}\n\nevent: incomplete\ndata: {"partial":true}',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    // Only the first complete event should be emitted
    expect(events).toHaveLength(1)
    expect(events[0]).toEqual({ type: 'chunk', data: { content: 'ok' } })
    consoleErrorSpy.mockRestore()
  })

  it('logs error and continues on malformed JSON in data field', async () => {
    const stream = makeSSEStream([
      'event: chunk\ndata: {invalid json}\n\nevent: complete\ndata: {"message":"ok"}\n\n',
    ])
    globalThis.fetch = mockFetchWithStream(stream)

    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const events: Array<{ type: string; data: any }> = []
    await sendChatMessageStream(
      defaultArgs.message,
      defaultArgs.context,
      defaultArgs.currentYaml,
      defaultArgs.selectedNodes,
      defaultArgs.history,
      (type, data) => events.push({ type, data })
    )

    // Malformed event is skipped, valid event still delivered
    expect(events).toHaveLength(1)
    expect(events[0]).toEqual({ type: 'complete', data: { message: 'ok' } })
    expect(consoleErrorSpy).toHaveBeenCalledWith('Failed to parse SSE JSON:', expect.any(SyntaxError))
    consoleErrorSpy.mockRestore()
  })

  it('throws on HTTP error response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      statusText: 'Internal Server Error',
      text: () => Promise.resolve('Provider unavailable'),
    })

    await expect(
      sendChatMessageStream(
        defaultArgs.message,
        defaultArgs.context,
        defaultArgs.currentYaml,
        defaultArgs.selectedNodes,
        defaultArgs.history,
        vi.fn()
      )
    ).rejects.toThrow('Provider unavailable')
  })

  it('throws with statusText when body is empty', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      statusText: 'Bad Gateway',
      text: () => Promise.resolve(''),
    })

    await expect(
      sendChatMessageStream(
        defaultArgs.message,
        defaultArgs.context,
        defaultArgs.currentYaml,
        defaultArgs.selectedNodes,
        defaultArgs.history,
        vi.fn()
      )
    ).rejects.toThrow('Bad Gateway')
  })
})
