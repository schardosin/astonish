/**
 * SSE Simulator — creates ReadableStreams from fixture event arrays.
 *
 * Matches the backend wire format:
 *   event: <type>\ndata: <json>\n\n
 *
 * The real connectChat() / connectChatStream() in studioChat.ts read from
 * response.body via getReader(), split on "\n\n", and parse each block.
 * This simulator produces identical byte sequences.
 */

export interface FixtureEvent {
  type: string
  data: Record<string, unknown>
  delayMs?: number
}

export interface ScenarioFixture {
  name: string
  description: string
  category: string
  events: FixtureEvent[]
}

/**
 * Encode a single event as an SSE text block.
 */
function encodeSSEBlock(event: FixtureEvent): string {
  const json = JSON.stringify(event.data)
  return `event: ${event.type}\ndata: ${json}\n\n`
}

/**
 * Creates a ReadableStream<Uint8Array> that emits SSE-formatted text chunks.
 *
 * Options:
 * - instant: emit all events without delays (default: true for tests)
 * - chunkSplit: if true, randomly split some events across chunk boundaries
 *   to exercise the parser's buffer reassembly logic
 */
export function createSSEStream(
  events: FixtureEvent[],
  options?: { instant?: boolean; chunkSplit?: boolean }
): ReadableStream<Uint8Array> {
  const instant = options?.instant !== false
  const chunkSplit = options?.chunkSplit || false
  const encoder = new TextEncoder()

  return new ReadableStream({
    async start(controller) {
      for (const event of events) {
        if (!instant && event.delayMs && event.delayMs > 0) {
          await new Promise(resolve => setTimeout(resolve, event.delayMs))
        }

        const block = encodeSSEBlock(event)

        if (chunkSplit && block.length > 20) {
          // Split the block at a random point to test buffer reassembly
          const splitPoint = Math.floor(block.length / 2)
          controller.enqueue(encoder.encode(block.slice(0, splitPoint)))
          // Yield control between chunks
          await new Promise(resolve => setTimeout(resolve, 0))
          controller.enqueue(encoder.encode(block.slice(splitPoint)))
        } else {
          controller.enqueue(encoder.encode(block))
        }

        // Yield control between events so React can process
        if (instant) {
          await new Promise(resolve => setTimeout(resolve, 0))
        }
      }
      controller.close()
    },
  })
}

/**
 * Creates a Response-like object suitable for mocking fetch() calls
 * to /api/studio/chat or /api/studio/sessions/:id/stream.
 */
export function createSSEResponse(
  events: FixtureEvent[],
  options?: { status?: number; instant?: boolean; chunkSplit?: boolean }
): Response {
  const status = options?.status || 200
  const stream = createSSEStream(events, options)

  return new Response(stream, {
    status,
    statusText: status === 200 ? 'OK' : 'Error',
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
    },
  })
}

/**
 * Loads and parses a fixture JSON file.
 * In vitest, JSON imports are handled natively, but this provides
 * a consistent interface for dynamic loading.
 */
export function loadFixture(fixture: ScenarioFixture): FixtureEvent[] {
  if (!fixture.events || !Array.isArray(fixture.events)) {
    throw new Error(`Invalid fixture: missing events array`)
  }
  // Validate that the fixture ends with a terminal event
  const lastEvent = fixture.events[fixture.events.length - 1]
  if (lastEvent && !['done', 'fleet_done'].includes(lastEvent.type)) {
    console.warn(`Fixture "${fixture.name}" does not end with a terminal event (done/fleet_done)`)
  }
  return fixture.events
}
