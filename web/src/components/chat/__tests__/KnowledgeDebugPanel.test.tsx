import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import type { KnowledgeDebugResponse } from '@/api/studioChat'

import { fetchKnowledgeDebug } from '@/api/studioChat'
import KnowledgeDebugPanel from '../KnowledgeDebugPanel'

vi.mock('@/api/studioChat', () => ({
  fetchKnowledgeDebug: vi.fn(),
}))

const baseResult = {
  path: 'Deploy K8s pods with kubectl',
  score: 0.85,
  category: 'knowledge',
  id: 'mem-123',
  scope: 'team',
  created_by: 'user-1',
  created_at: '2025-01-15T10:00:00Z',
  session_id: 'sess-abc',
}

const injectedText = '## Memory\n<system>literal & safe</system>\n  indented\n```sh\necho "hello"\n```\n'

function capturedResponse(): KnowledgeDebugResponse {
  return {
    sessionId: 's1', invocationId: 'inv1', tools: null,
    knowledge: {
      type: 'knowledge', query: 'deploy pods', bm25_query_len: 11,
      results: [], result_count: 2, estimated_tokens: 150,
      version: 1, invocation_id: 'inv1', dispatched: true, capture_status: 'captured',
      snapshot: {
        text: injectedText, capture_bytes: 200, original_bytes: 200, truncated: false, redacted: false,
        snippets: [
          { ...baseResult, path: 'First memory', snippet: 'first\n  <b>not HTML</b>' },
          { ...baseResult, path: 'Second memory', category: 'guidance', scope: 'personal', snippet: 'second\nline' },
        ],
      },
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('KnowledgeDebugPanel', () => {
  beforeEach(() => {
    vi.mocked(fetchKnowledgeDebug).mockReset()
  })

  it('renders loading then an empty state', async () => {
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue({
      sessionId: 's1',
      invocationId: 'inv1',
      knowledge: null,
      tools: null,
    })

    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    expect(screen.getByText('Loading knowledge debug…')).toBeTruthy()
    expect(await screen.findByText(/No knowledge or tool injection data/)).toBeTruthy()
  })

  it('renders knowledge results with score, category, and scope', async () => {
    const guidanceResult = {
      ...baseResult,
      path: 'Always use --dry-run for kubectl apply',
      score: 0.92,
      category: 'guidance',
      scope: 'personal',
    }

    vi.mocked(fetchKnowledgeDebug).mockResolvedValue({
      sessionId: 's1',
      invocationId: 'inv1',
      knowledge: {
        type: 'knowledge',
        query: 'deploy kubernetes pods',
        bm25_query_len: 42,
        results: [guidanceResult, baseResult],
        result_count: 2,
        estimated_tokens: 150,
      },
      tools: null,
    })

    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)

    // Wait for content to load
    expect(await screen.findByText('Knowledge injection')).toBeTruthy()

    // Verify type badge (appears as type badge and as category on second result)
    expect(screen.getAllByText('knowledge').length).toBeGreaterThanOrEqual(1)

    // Verify query display
    expect(screen.getByText('deploy kubernetes pods')).toBeTruthy()

    // Verify token estimate
    expect(screen.getByText('~150 tokens')).toBeTruthy()

    // Verify result count
    expect(screen.getByText('2 results')).toBeTruthy()

    // Verify result paths
    expect(screen.getByText('Always use --dry-run for kubectl apply')).toBeTruthy()
    expect(screen.getByText('Deploy K8s pods with kubectl')).toBeTruthy()

    // Verify scores
    expect(screen.getByText('92%')).toBeTruthy()
    expect(screen.getByText('85%')).toBeTruthy()

    // Verify categories
    expect(screen.getByText('guidance')).toBeTruthy()

    // Verify scopes
    expect(screen.getByText('personal')).toBeTruthy()
    expect(screen.getByText('team')).toBeTruthy()
  })

  it('renders tool discovery section when tools are present', async () => {
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue({
      sessionId: 's1',
      invocationId: 'inv1',
      knowledge: null,
      tools: {
        query: 'search kubernetes',
        results: [
          { name: 'kubectl_apply', group: 'k8s', score: 0.9 },
          { name: 'helm_install', group: 'k8s', score: 0.7 },
        ],
        result_count: 2,
      },
    })

    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)

    expect(await screen.findByText('Tool discovery')).toBeTruthy()
    expect(screen.getByText('kubectl_apply')).toBeTruthy()
    expect(screen.getByText('helm_install')).toBeTruthy()
    expect(screen.getByText('2 tools')).toBeTruthy()
  })

  it('preserves exact multiline context and ordered escaped snippets with provenance', async () => {
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue(capturedResponse())
    const { container } = render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    expect(await screen.findByText('Current turn only')).toBeInTheDocument()
    expect(fetchKnowledgeDebug).toHaveBeenCalledWith('s1', 'inv1')
    const snippets = container.querySelectorAll('pre:not([data-testid])')
    expect(Array.from(snippets, node => node.textContent)).toEqual(['first\n  <b>not HTML</b>', 'second\nline'])
    expect(container.querySelector('b, system')).toBeNull()
    expect(screen.getByText('First memory').compareDocumentPosition(screen.getByText('Second memory')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getAllByText('user-1')).toHaveLength(2)
    expect(screen.getAllByTitle('sess-abc')).toHaveLength(2)
    expect(screen.getAllByText(new Date(baseResult.created_at).toLocaleDateString())).toHaveLength(2)
    expect(screen.getByText('team')).toBeInTheDocument()
    expect(screen.getByText('personal')).toBeInTheDocument()
    expect(screen.getAllByText('85%')).toHaveLength(2)
    const disclosure = screen.getByText('Injected memory context').closest('details')!
    expect(disclosure.open).toBe(false)
    fireEvent.click(screen.getByText('Injected memory context'))
    expect(disclosure.open).toBe(true)
    expect(screen.getByTestId('injected-memory-text').textContent).toBe(injectedText)
  })

  it.each([null, []])('renders captured empty memory safely with snippets=%j', async snippets => {
    const response = capturedResponse()
    if (response.knowledge?.capture_status !== 'captured') throw new Error('fixture')
    response.knowledge.result_count = 0
    response.knowledge.type = 'none'
    response.knowledge.snapshot = { ...response.knowledge.snapshot, snippets, text: '' }
    response.tools = { query: '', results: [], result_count: 0 }
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue(response)
    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    expect(await screen.findByText('No automatic memories injected for this turn.')).toBeInTheDocument()
    expect(screen.getByText('No tools discovered for this turn.')).toBeInTheDocument()
    expect(screen.queryByText('Injected memory context')).not.toBeInTheDocument()
  })

  it('distinguishes capture disabled from legacy unavailable text', async () => {
    const response = capturedResponse()
    response.knowledge = {
      type: 'knowledge', query: '', bm25_query_len: 0, results: [baseResult], result_count: 1, estimated_tokens: 10,
      version: 1, invocation_id: 'inv1', dispatched: true, capture_status: 'disabled',
    }
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue(response)
    const { rerender } = render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    expect(await screen.findByText(/Enable Debug before sending/)).toBeInTheDocument()
    expect(screen.queryByText(/older turn/)).not.toBeInTheDocument()
    response.knowledge = {
      type: 'knowledge', query: '', bm25_query_len: 0, results: [baseResult], result_count: 1, estimated_tokens: 10,
      capture_status: 'unavailable',
    }
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue({ ...response, invocationId: 'legacy' })
    rerender(<KnowledgeDebugPanel sessionId="s1" invocationId="legacy" />)
    expect(await screen.findByText('Injected text was not recorded for this older turn.')).toBeInTheDocument()
    expect(screen.getByText(baseResult.path)).toBeInTheDocument()
    expect(screen.queryByText(/Enable Debug before sending/)).not.toBeInTheDocument()
  })

  it('labels prepared, truncated and redacted capture without claiming dispatch or model truncation', async () => {
    const response = capturedResponse()
    if (response.knowledge?.capture_status !== 'captured') throw new Error('fixture')
    response.knowledge.dispatched = false
    response.knowledge.snapshot.truncated = true
    response.knowledge.snapshot.redacted = true
    vi.mocked(fetchKnowledgeDebug).mockResolvedValue(response)
    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    expect(await screen.findByText('Memory prepared; provider dispatch was not recorded.')).toBeInTheDocument()
    expect(screen.getByText('Snapshot truncated: the saved debug text is incomplete. Model input was not truncated.')).toBeInTheDocument()
    expect(screen.getByText('Credential values were redacted before injection and capture.')).toBeInTheDocument()
    expect(screen.queryByText('No automatic memories injected for this turn.')).not.toBeInTheDocument()
  })

  it.each(['invocation', 'session'])('ignores stale responses after a %s switch and resets expansion', async change => {
    const stale = deferred<KnowledgeDebugResponse>()
    vi.mocked(fetchKnowledgeDebug).mockReturnValueOnce(stale.promise).mockResolvedValue(capturedResponse())
    const { rerender } = render(<KnowledgeDebugPanel sessionId="old" invocationId="old" />)
    rerender(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)
    await screen.findByText('Injected memory context')
    fireEvent.click(screen.getByText('Injected memory context'))
    expect(screen.getByText('Injected memory context').closest('details')!.open).toBe(true)
    await act(async () => { stale.resolve({ sessionId: 'old', invocationId: 'old', knowledge: null, tools: null }) })
    expect(screen.getByTestId('injected-memory-text').textContent).toBe(injectedText)
    rerender(<KnowledgeDebugPanel sessionId={change === 'session' ? 's2' : 's1'} invocationId={change === 'invocation' ? 'inv2' : 'inv1'} />)
    await screen.findByText('Injected memory context')
    expect(screen.getByText('Injected memory context').closest('details')!.open).toBe(false)
    expect(fetchKnowledgeDebug).toHaveBeenLastCalledWith(change === 'session' ? 's2' : 's1', change === 'invocation' ? 'inv2' : 'inv1')
  })

  it('ignores stale errors and clears current errors when selecting another turn', async () => {
    const stale = deferred<KnowledgeDebugResponse>()
    vi.mocked(fetchKnowledgeDebug).mockReturnValueOnce(stale.promise).mockRejectedValueOnce(new Error('Current failure')).mockResolvedValue(capturedResponse())
    const { rerender } = render(<KnowledgeDebugPanel sessionId="s1" invocationId="old" />)
    rerender(<KnowledgeDebugPanel sessionId="s1" invocationId="current" />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Current failure')
    rerender(<KnowledgeDebugPanel sessionId="s2" invocationId="inv1" />)
    await screen.findByText('Injected memory context')
    await act(async () => { stale.reject(new Error('Stale failure')) })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('renders error state', async () => {
    vi.mocked(fetchKnowledgeDebug).mockRejectedValue(new Error('Network failure'))

    render(<KnowledgeDebugPanel sessionId="s1" invocationId="inv1" />)

    expect(await screen.findByText('Network failure')).toBeTruthy()
  })
})
