import { describe, it, expect } from 'vitest'
import type { ChatMsg } from '../chatTypes'
import {
  activityStats,
  activitySummary,
  buildActivityRenderIndex,
  categorizeTool,
  deriveLiveStreamStatus,
  extractPathHint,
  groupToolActivity,
  hasAppFence,
  isAppProgressAgent,
  isHardBreakType,
  isSoftNoteType,
  isToolResultError,
  latestProcessText,
  liveActivityHint,
  previewValue,
  splitActivitySummary,
} from '../toolActivity'

describe('isToolResultError', () => {
  it('detects success: false and error fields', () => {
    expect(isToolResultError({ success: false })).toBe(true)
    expect(isToolResultError({ error: 'boom' })).toBe(true)
    expect(isToolResultError({ ok: false })).toBe(true)
    expect(isToolResultError({ status: 'failed' })).toBe(true)
  })

  it('does not flag successful results', () => {
    expect(isToolResultError({ success: true, data: 1 })).toBe(false)
    expect(isToolResultError('ok')).toBe(false)
    expect(isToolResultError(null)).toBe(false)
  })
})

describe('previewValue', () => {
  it('stringifies and truncates', () => {
    expect(previewValue({ url: 'https://example.com' })).toContain('example.com')
    expect(previewValue('a'.repeat(100), 20).endsWith('…')).toBe(true)
  })
})

describe('groupToolActivity', () => {
  it('folds interstitial agents between tools; keeps final agent outside', () => {
    const messages: ChatMsg[] = [
      { type: 'user', content: 'hi' },
      { type: 'tool_call', toolName: 'search_tools', toolArgs: { q: 'x' } },
      { type: 'tool_result', toolName: 'search_tools', toolResult: { ok: true } },
      { type: 'agent', content: 'checking next step' },
      { type: 'tool_call', toolName: 'http_request', toolArgs: { url: 'https://a' } },
      { type: 'tool_result', toolName: 'http_request', toolResult: 'body' },
      { type: 'agent', content: 'done' },
    ]

    const segs = groupToolActivity(messages)
    expect(segs.map(s => s.kind)).toEqual([
      'passthrough', // user
      'activity',
      'passthrough', // final agent
    ])

    const activity = segs[1]
    expect(activity.kind).toBe('activity')
    if (activity.kind !== 'activity') return
    expect(activity.steps).toHaveLength(2)
    expect(activity.notes).toHaveLength(1)
    expect(activity.notes[0].text).toBe('checking next step')
    expect(activity.coveredIndices).toContain(3)
    expect(activity.coveredIndices).not.toContain(6)
  })

  it('marks unpaired call as running', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'http_request', toolArgs: { url: 'https://a' } },
    ]
    const segs = groupToolActivity(messages)
    expect(segs).toHaveLength(1)
    expect(segs[0].kind).toBe('activity')
    if (segs[0].kind !== 'activity') return
    expect(segs[0].steps[0].status).toBe('running')
  })

  it('pairs parallel call/call/result/result by tool name FIFO', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'web_search', toolArgs: { query: 'a' } },
      { type: 'tool_call', toolName: 'read_file', toolArgs: { path: '/x' } },
      { type: 'tool_result', toolName: 'web_search', toolResult: 'hits' },
      { type: 'tool_result', toolName: 'read_file', toolResult: 'body' },
    ]
    const segs = groupToolActivity(messages)
    expect(segs).toHaveLength(1)
    if (segs[0].kind !== 'activity') throw new Error('expected activity')
    expect(segs[0].steps).toHaveLength(2)
    expect(segs[0].steps[0]).toMatchObject({
      toolName: 'web_search',
      status: 'complete',
      result: 'hits',
    })
    expect(segs[0].steps[1]).toMatchObject({
      toolName: 'read_file',
      status: 'complete',
      result: 'body',
    })
  })

  it('breaks fold on subtask_execution and keeps panel outside', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'shell_command', toolArgs: { command: 'a' } },
      { type: 'tool_result', toolName: 'shell_command', toolResult: 'ok' },
      {
        type: 'subtask_execution',
        tasks: [{ name: 't1', description: 'd' }],
        events: [],
        status: 'running',
      },
      { type: 'tool_call', toolName: 'read_file', toolArgs: { path: 'x' } },
      { type: 'tool_result', toolName: 'read_file', toolResult: 'y' },
    ]
    const segs = groupToolActivity(messages)
    expect(segs.map(s => s.kind)).toEqual(['activity', 'passthrough', 'activity'])
    const mid = segs[1]
    expect(mid.kind).toBe('passthrough')
    if (mid.kind === 'passthrough') expect(mid.index).toBe(2)
    if (segs[0].kind === 'activity') {
      expect(segs[0].coveredIndices).not.toContain(2)
    }
  })

  it('keeps approval and artifact outside the fold', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'shell_command', toolArgs: { command: 'ls' } },
      { type: 'tool_result', toolName: 'shell_command', toolResult: 'ok' },
      { type: 'approval', toolName: 'shell_command', options: ['Allow', 'Deny'] },
      { type: 'tool_call', toolName: 'write_file', toolArgs: { path: 'a.md', content: 'x' } },
      { type: 'tool_result', toolName: 'write_file', toolResult: { ok: true } },
      { type: 'artifact', path: 'a.md', toolName: 'write_file' },
    ]
    const segs = groupToolActivity(messages)
    expect(segs.map(s => s.kind)).toEqual([
      'activity',
      'passthrough', // approval
      'activity',
      'passthrough', // artifact
    ])
  })

  it('breaks on browser_handoff', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'browser_request_human', toolArgs: {} },
      {
        type: 'browser_handoff',
        vncProxyUrl: 'http://vnc',
        pageUrl: 'http://app',
        pageTitle: 'App',
        reason: 'captcha',
      },
    ]
    const segs = groupToolActivity(messages)
    expect(segs.map(s => s.kind)).toEqual(['activity', 'passthrough'])
  })

  it('does not create activity for soft-only or text-only turns', () => {
    expect(groupToolActivity([
      { type: 'user', content: 'hi' },
      { type: 'agent', content: 'hello' },
    ]).every(s => s.kind === 'passthrough')).toBe(true)

    expect(groupToolActivity([
      { type: 'thinking', content: '…' },
      { type: 'auto_approved', toolName: 'shell_command' },
    ]).every(s => s.kind === 'passthrough')).toBe(true)
  })

  it('defaults unknown types to hard break', () => {
    expect(isHardBreakType('brand_new_panel')).toBe(true)
    expect(isSoftNoteType('brand_new_panel')).toBe(false)
  })

  it('absorbs trailing soft notes while streaming; keeps them passthrough when finished', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'shell_command', toolArgs: { command: 'ls' } },
      { type: 'tool_result', toolName: 'shell_command', toolResult: 'ok' },
      { type: 'agent', content: 'checking credentials next' },
    ]

    const finished = groupToolActivity(messages)
    expect(finished.map(s => s.kind)).toEqual(['activity', 'passthrough'])
    if (finished[0].kind === 'activity') {
      expect(finished[0].coveredIndices).not.toContain(2)
      expect(finished[0].notes).toHaveLength(0)
    }

    const live = groupToolActivity(messages, { absorbTrailingSoft: true })
    expect(live.map(s => s.kind)).toEqual(['activity'])
    if (live[0].kind !== 'activity') throw new Error('expected activity')
    expect(live[0].coveredIndices).toContain(2)
    expect(live[0].notes).toHaveLength(1)
    expect(live[0].notes[0].text).toBe('checking credentials next')
    expect(latestProcessText(live[0].notes)).toBe('checking credentials next')
  })

  it('never absorbs astonish-app agent messages (AppCodeIndicator must stay visible)', () => {
    const trailing: ChatMsg[] = [
      { type: 'tool_call', toolName: 'search_tools', toolArgs: {} },
      { type: 'tool_result', toolName: 'search_tools', toolResult: {} },
      {
        type: 'agent',
        content: 'Building it now.\n\n```astonish-app\nfunction App() { return <div/> }\n',
        _streaming: true,
      },
    ]
    const live = groupToolActivity(trailing, { absorbTrailingSoft: true })
    expect(live.map(s => s.kind)).toEqual(['activity', 'passthrough'])
    if (live[0].kind !== 'activity') throw new Error('expected activity')
    expect(live[0].coveredIndices).not.toContain(2)
    expect(live[0].notes).toHaveLength(0)
    if (live[1].kind === 'passthrough') expect(live[1].index).toBe(2)

    const between: ChatMsg[] = [
      { type: 'tool_call', toolName: 'read_file', toolArgs: { path: 'a' } },
      { type: 'tool_result', toolName: 'read_file', toolResult: 'x' },
      {
        type: 'agent',
        content: '```astonish-app\nconst App = () => null\n```',
      },
      { type: 'tool_call', toolName: 'write_file', toolArgs: { path: 'b' } },
      { type: 'tool_result', toolName: 'write_file', toolResult: { ok: true } },
    ]
    const mid = groupToolActivity(between, { absorbTrailingSoft: true })
    expect(mid.filter(s => s.kind === 'activity')).toHaveLength(1)
    const activity = mid.find(s => s.kind === 'activity')
    if (!activity || activity.kind !== 'activity') throw new Error('expected activity')
    expect(activity.coveredIndices).not.toContain(2)
    expect(activity.notes).toHaveLength(0)
    expect(isAppProgressAgent(between[2])).toBe(true)
    expect(hasAppFence('plain text')).toBe(false)
  })

  it('marks error results', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'http_request', toolArgs: {} },
      { type: 'tool_result', toolName: 'http_request', toolResult: { error: 'timeout' } },
    ]
    const segs = groupToolActivity(messages)
    if (segs[0].kind !== 'activity') throw new Error('expected activity')
    expect(segs[0].steps[0].status).toBe('error')
  })
})

describe('categorizeTool', () => {
  it('maps built-ins and prefixes', () => {
    expect(categorizeTool('write_file')).toBe('edit')
    expect(categorizeTool('read_file')).toBe('explore')
    expect(categorizeTool('grep_search')).toBe('search')
    expect(categorizeTool('web_search')).toBe('search')
    expect(categorizeTool('http_request')).toBe('request')
    expect(categorizeTool('shell_command')).toBe('command')
    expect(categorizeTool('browser_navigate')).toBe('browser')
    expect(categorizeTool('memory_get')).toBe('memory')
    expect(categorizeTool('custom_mcp_tool')).toBe('other')
  })
})

describe('extractPathHint', () => {
  it('reads common path-like args', () => {
    expect(extractPathHint({ path: 'a.md' })).toBe('a.md')
    expect(extractPathHint({ url: 'https://x' })).toBe('https://x')
    expect(extractPathHint({ query: 'x' })).toBeUndefined()
  })
})

describe('activitySummary', () => {
  it('shows rich running hint while streaming', () => {
    const summary = activitySummary(
      [{ toolName: 'http_request', status: 'running', callIndex: 0, args: { url: 'https://example.com/api' } }],
      { streaming: true },
    )
    expect(summary.variant).toBe('running')
    expect(summary.text).toBe('Fetching https://example.com/api with http_request')
    expect(summary.lead).toBe('Fetching')
  })

  it('summarizes completed tools with human categories', () => {
    const summary = activitySummary([
      { toolName: 'search_tools', status: 'complete', callIndex: 0 },
      { toolName: 'http_request', status: 'complete', callIndex: 1 },
      { toolName: 'write_file', status: 'complete', callIndex: 2, args: { path: 'a.md' } },
      { toolName: 'write_file', status: 'complete', callIndex: 3, args: { path: 'b.md' } },
      { toolName: 'read_file', status: 'complete', callIndex: 4, args: { path: 'a.md' } },
      { toolName: 'read_file', status: 'complete', callIndex: 5, args: { path: 'c.md' } },
      { toolName: 'grep_search', status: 'complete', callIndex: 6 },
    ])
    expect(summary.variant).toBe('complete')
    expect(summary.text).toBe('Edited 2 files, explored 2 files, 2 searches, 1 request')
  })

  it('folds unknown tools into other', () => {
    const summary = activitySummary([
      { toolName: 'a', status: 'complete', callIndex: 0 },
      { toolName: 'b', status: 'complete', callIndex: 1 },
      { toolName: 'c', status: 'complete', callIndex: 2 },
      { toolName: 'd', status: 'complete', callIndex: 3 },
    ])
    expect(summary.text).toBe('Used 4 other tools')
  })

  it('highlights failures after categorized body', () => {
    const summary = activitySummary([
      { toolName: 'write_file', status: 'complete', callIndex: 0, args: { path: 'a.md' } },
      { toolName: 'http_request', status: 'error', callIndex: 1 },
    ])
    expect(summary.variant).toBe('error')
    expect(summary.text).toBe('Edited 1 file, 1 request · http_request failed')
    expect(summary.lead).toBe('Edited')
    expect(summary.rest).toBe(' 1 file, 1 request')
    expect(summary.errorSuffix).toBe(' · http_request failed')
  })

  it('splits lead/rest for mixed categories', () => {
    const summary = activitySummary([
      { toolName: 'write_file', status: 'complete', callIndex: 0, args: { path: 'a.md' } },
      { toolName: 'grep_search', status: 'complete', callIndex: 1 },
    ])
    expect(summary.lead).toBe('Edited')
    expect(summary.rest).toBe(' 1 file, 1 search')
  })

  it('splits live hints into accent lead + muted rest', () => {
    const parts = splitActivitySummary(
      'Running `ls -lah` with shell_command',
      'running',
    )
    expect(parts.lead).toBe('Running')
    expect(parts.rest).toBe(' `ls -lah` with shell_command')
  })
})

describe('liveActivityHint', () => {
  it('includes shell command text', () => {
    expect(liveActivityHint({
      toolName: 'shell_command',
      status: 'running',
      callIndex: 0,
      args: { command: 'ls -lah' },
    })).toBe('Running `ls -lah` with shell_command')
  })

  it('includes edit path', () => {
    expect(liveActivityHint({
      toolName: 'edit_file',
      status: 'running',
      callIndex: 0,
      args: { path: 'web/src/App.tsx' },
    })).toBe('Editing web/src/App.tsx with edit_file')
  })

  it('describes credential tools', () => {
    expect(liveActivityHint({
      toolName: 'list_credentials',
      status: 'running',
      callIndex: 0,
      args: {},
    })).toBe('Listing credentials with list_credentials')
  })

  it('truncates long commands', () => {
    const long = 'echo ' + 'x'.repeat(80)
    const hint = liveActivityHint({
      toolName: 'shell_command',
      status: 'running',
      callIndex: 0,
      args: { command: long },
    })
    expect(hint.startsWith('Running `')).toBe(true)
    expect(hint.includes('…')).toBe(true)
    expect(hint.endsWith('with shell_command')).toBe(true)
    expect(hint.length).toBeLessThan(long.length + 40)
  })

  it('falls back to tool name', () => {
    expect(liveActivityHint({
      toolName: 'custom_mcp_tool',
      status: 'running',
      callIndex: 0,
    })).toBe('Running custom_mcp_tool')
  })
})

describe('deriveLiveStreamStatus', () => {
  it('returns live hint when last message is a running tool', () => {
    const messages: ChatMsg[] = [
      { type: 'user', content: 'hi' },
      { type: 'tool_call', toolName: 'shell_command', toolArgs: { command: 'pwd' } },
    ]
    expect(deriveLiveStreamStatus(messages)).toBe('Running `pwd` with shell_command')
  })

  it('returns Thinking… when streaming without an open tool', () => {
    const messages: ChatMsg[] = [
      { type: 'user', content: 'hi' },
      { type: 'agent', content: 'working', _streaming: true },
    ]
    expect(deriveLiveStreamStatus(messages)).toBe('Thinking…')
  })
})

describe('activityStats', () => {
  it('infers +/- lines from edit_file args', () => {
    const stats = activityStats([
      {
        toolName: 'edit_file',
        status: 'complete',
        callIndex: 0,
        args: {
          path: 'a.ts',
          old_string: 'a\nb\nc',
          new_string: 'a\nb\nc\nd\ne',
        },
      },
    ])
    expect(stats).toEqual({ kind: 'diff', added: 5, removed: 3 })
  })

  it('falls back to a step badge when no edit content', () => {
    const stats = activityStats([
      { toolName: 'shell_command', status: 'complete', callIndex: 0 },
      { toolName: 'http_request', status: 'complete', callIndex: 1 },
    ])
    expect(stats).toEqual({ kind: 'badge', count: 2 })
  })
})

describe('buildActivityRenderIndex', () => {
  it('maps start indices and skips covered ones', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'a', toolArgs: {} },
      { type: 'tool_result', toolName: 'a', toolResult: {} },
      { type: 'agent', content: 'x' },
    ]
    const { activityByStart, skipIndices, lastActivityStart } = buildActivityRenderIndex(messages)
    expect(activityByStart.has(0)).toBe(true)
    expect(skipIndices.has(1)).toBe(true)
    expect(skipIndices.has(0)).toBe(false)
    expect(skipIndices.has(2)).toBe(false)
    expect(lastActivityStart).toBe(0)
  })

  it('skips trailing soft notes when absorbTrailingSoft is set', () => {
    const messages: ChatMsg[] = [
      { type: 'tool_call', toolName: 'a', toolArgs: {} },
      { type: 'tool_result', toolName: 'a', toolResult: {} },
      { type: 'agent', content: 'provisional' },
    ]
    const { skipIndices, activityByStart } = buildActivityRenderIndex(messages, {
      absorbTrailingSoft: true,
    })
    expect(skipIndices.has(2)).toBe(true)
    const activity = activityByStart.get(0)
    expect(activity?.notes.map(n => n.text)).toEqual(['provisional'])
  })
})
