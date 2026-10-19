import { afterEach, describe, expect, it } from 'vitest'
import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { renderChat, type RenderChatResult } from '../../test/helpers/renderChat'
import { mockJsonResponse } from '../../test/helpers/mockFetch'
import '../../test/scenarios/scenarioSetup'

/** Keep the real fetch/SSE parser and reducers, but release each event explicitly.
 * Unlike sendMessage(), this must not wait for completion before inspecting UI.
 */
function controlledStream() {
  let controller!: ReadableStreamDefaultController<Uint8Array>
  let closed = false
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(value) { controller = value },
  })
  return {
    response: new Response(body, { headers: { 'Content-Type': 'text/event-stream' } }),
    emit(type: string, data: Record<string, unknown>) {
      controller.enqueue(encoder.encode(`event: ${type}\ndata: ${JSON.stringify(data)}\n\n`))
    },
    close() {
      if (!closed) {
        closed = true
        controller.close()
      }
    },
  }
}

describe('StudioChat streaming generation progress', () => {
  let result: RenderChatResult | undefined
  let stream: ReturnType<typeof controlledStream> | undefined

  afterEach(async () => {
    await act(async () => { stream?.close() })
    result?.cleanup()
    result = undefined
    stream = undefined
  })

  for (const theme of ['light', 'dark']) {
    it.each(['send', 'reconnect'] as const)(`shows thinking and incremental text before completion on %s (${theme})`, async mode => {
      const sessionId = 'streaming-progress-session'
      const pendingStream = controlledStream()
      stream = pendingStream
      let streamRequested = false
      const streamPath = mode === 'send'
        ? '/api/studio/chat'
        : `/api/studio/sessions/${sessionId}/stream`

      result = renderChat({
        theme,
        initialSessionId: mode === 'reconnect' ? sessionId : undefined,
        sessions: mode === 'reconnect' ? [{ id: sessionId, title: 'In-flight generation' }] : [],
        sessionStatus: { sessionId, running: mode === 'reconnect', eventCount: 0 },
        mockConfig: {
          customHandlers: {
            '/api/settings/providers/effective': () => mockJsonResponse([]),
            '/model-status': () => mockJsonResponse(null),
            [streamPath]: () => {
              streamRequested = true
              return pendingStream.response
            },
          },
        },
      })

      if (mode === 'send') {
        const input = screen.getByTestId('chat-input')
        fireEvent.change(input, { target: { value: 'Explain the result' } })
        fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
      }
      await waitFor(() => expect(streamRequested).toBe(true))

      const expectStillStreaming = () => {
        expect(screen.getByTitle('Stop')).toBeVisible()
        expect(screen.getByTestId('chat-input')).toHaveAttribute('placeholder', 'Agent is responding...')
      }
      expectStillStreaming()

      // ChatRunner.processStateDelta maps _spinner_text to thinking.data.text.
      // No done event or EOF is released until all intermediate assertions pass.
      for (const text of ['Model is thinking…', 'Preparing tool arguments…']) {
        await act(async () => { pendingStream.emit('thinking', { text }) })
        expect(await screen.findByText(text)).toBeVisible()
        expect(screen.queryByText('An incremental answer.')).not.toBeInTheDocument()
        expectStillStreaming()
      }

      await act(async () => { pendingStream.emit('text', { text: 'An incremental', invocationId: 'progress-turn' }) })
      expect(await screen.findByText('An incremental')).toBeVisible()
      expect(screen.queryByText('An incremental answer.')).not.toBeInTheDocument()
      expectStillStreaming()

      expect(screen.queryByText('Preparing tool arguments…')).not.toBeInTheDocument()
      await act(async () => { pendingStream.emit('thinking', { text: 'Preparing tool arguments…' }) })
      expect(screen.getByTestId('thinking-indicator')).toHaveTextContent('Preparing tool arguments…')
      expect(screen.queryByTestId('activity-notes')).not.toBeInTheDocument()
      await act(async () => { pendingStream.emit('text', { text: ' answer.', invocationId: 'progress-turn' }) })
      expect(await screen.findByText('An incremental answer.')).toBeVisible()
      expect(screen.queryByText('An incremental', { exact: true })).not.toBeInTheDocument()
      expectStillStreaming()

      await act(async () => { pendingStream.emit('tool_call', { name: 'first_tool', args: {} }) })
      const block = screen.getByTestId('tool-activity-block')
      expect(screen.queryByText('An incremental answer.')).not.toBeInTheDocument()
      fireEvent.click(block.querySelector('button')!)
      expect(screen.getByText('An incremental answer.').closest('[data-testid="tool-activity-block"]')).toBe(block)
      await act(async () => {
        pendingStream.emit('tool_result', { name: 'first_tool', result: { ok: true } })
        pendingStream.emit('text', { text: 'Middle message.', invocationId: 'progress-turn' })
      })
      expect(screen.getByText('Middle message.').closest('[data-testid="tool-activity-block"]')).toBeNull()
      await act(async () => {
        pendingStream.emit('tool_call', { name: 'second_tool', args: {} })
        pendingStream.emit('tool_result', { name: 'second_tool', result: { ok: true } })
        pendingStream.emit('text', { text: 'Final answer.', invocationId: 'progress-turn' })
      })
      expect(screen.getByText('Middle message.').closest('[data-testid="tool-activity-block"]')).toBe(block)
      expect(screen.getByText('Final answer.').closest('[data-testid="tool-activity-block"]')).toBeNull()
      expect(Array.from(block.querySelectorAll('[data-activity-index]')).map(el => el.textContent)).toEqual([
        expect.stringContaining('An incremental answer.'), expect.stringContaining('first_tool'),
        expect.stringContaining('Middle message.'), expect.stringContaining('second_tool'),
      ])
      expect(block.querySelector('button')).toHaveAttribute('aria-expanded', 'true')
      await act(async () => {
        pendingStream.emit('done', { status: 'complete', invocationId: 'progress-turn' })
        pendingStream.close()
      })
      await waitFor(() => expect(screen.queryByTitle('Stop')).not.toBeInTheDocument())
      expect(screen.getByTestId('chat-input')).not.toHaveAttribute('placeholder', 'Agent is responding...')
      expect(screen.getAllByText('An incremental answer.')).toHaveLength(1)
    })
  }
})
