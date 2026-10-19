import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ToolActivityBlock from '../ToolActivityBlock'
import type { ActivityEntry, ToolActivityStep } from '../toolActivity'

const tool = (index: number, overrides: Partial<ToolActivityStep> = {}): ActivityEntry => ({
  kind: 'tool', index, step: { toolName: 'http_request', args: { url: 'https://example.com' }, result: { status: 200 }, status: 'complete', callIndex: index, ...overrides },
})
const message = (index: number, content: string): ActivityEntry => ({ kind: 'message', index, message: { type: 'agent', content } })
const renderMessage = (msg: { content: string }, index: number) => <article data-testid={`message-${index}`}>{msg.content}</article>
const props = { blockId: 'activity-0', renderMessage }

describe('ToolActivityBlock', () => {
  it('renders a compact categorized summary and tool-only badge', () => {
    render(<ToolActivityBlock {...props} entries={[tool(0, { toolName: 'search_tools' }), message(1, 'Hidden narration'), tool(2)]} />)
    expect(screen.getByRole('button', { name: /1 search, 1 request/ })).toHaveClass('inline-flex')
    expect(screen.getByTestId('activity-badge')).toHaveTextContent('2')
    expect(screen.getByTestId('activity-badge')).toHaveStyle({ color: 'var(--brand)' })
    expect(screen.getByTestId('activity-chevron')).toHaveClass('opacity-0', 'group-hover:opacity-100')
    expect(screen.queryByText('Hidden narration')).not.toBeInTheDocument()
    expect(screen.queryByTestId('activity-process-text')).not.toBeInTheDocument()
  })

  it('interleaves complete untruncated messages, tools and process rows', () => {
    const long = 'Full intermediate message. '.repeat(30)
    render(<ToolActivityBlock {...props} entries={[message(0, long), tool(1), message(3, 'Next message'), { kind: 'process', index: 4, note: { kind: 'retry', text: 'Retry 1/3' } }, tool(5)]} />)
    fireEvent.click(screen.getByRole('button', { name: /2 requests/ }))
    const block = screen.getByTestId('tool-activity-block')
    expect([...block.querySelectorAll('[data-activity-index]')].map(el => el.getAttribute('data-activity-index'))).toEqual(['0', '1', '3', '4', '5'])
    expect(screen.getByTestId('message-0').textContent).toBe(long)
    expect(screen.queryByText('Notes')).not.toBeInTheDocument()
  })

  it('retains expanded payloads when intermediate entries arrive', () => {
    const { rerender } = render(<ToolActivityBlock {...props} entries={[tool(2)]} />)
    fireEvent.click(screen.getByRole('button', { name: '1 request' }))
    fireEvent.click(screen.getByText('http_request'))
    expect(screen.getByText('Arguments')).toBeVisible()
    expect(screen.getByText('Result')).toBeVisible()
    rerender(<ToolActivityBlock {...props} entries={[message(0, 'Earlier message'), tool(2), message(4, 'Later message'), tool(5)]} />)
    expect(screen.getByText('Arguments')).toBeVisible()
    expect(screen.getByText('Result')).toBeVisible()
  })

  it('supports keyboard disclosure and nested tool disclosure', async () => {
    const user = userEvent.setup()
    render(<ToolActivityBlock {...props} entries={[tool(0)]} />)
    const header = screen.getByRole('button', { name: '1 request' })
    header.focus()
    await user.keyboard('{Enter}')
    expect(header).toHaveAttribute('aria-expanded', 'true')
    await user.tab()
    await user.keyboard(' ')
    expect(screen.getByText('Arguments')).toBeVisible()
    header.focus()
    await user.keyboard(' ')
    expect(header).toHaveAttribute('aria-expanded', 'false')
  })

  it('shows live running hints and failed status without success fabrication', () => {
    const { rerender } = render(<ToolActivityBlock {...props} streaming entries={[tool(0, { status: 'running', result: undefined })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Fetching https:\/\/example.com with http_request/ }))
    fireEvent.click(screen.getByText('http_request'))
    expect(screen.getByText('Waiting for result…')).toBeVisible()
    rerender(<ToolActivityBlock {...props} entries={[tool(0, { status: 'error', result: { error: 'timeout' } })]} />)
    expect(screen.getByRole('button', { name: /http_request failed/ })).toBeVisible()
    expect(screen.getByText('Result')).toBeVisible()
  })

  it('shows changed-line diff metrics, excluding assistant messages', () => {
    render(<ToolActivityBlock {...props} entries={[message(0, 'many\nlines\nhere'), tool(1, { toolName: 'edit_file', args: { old_string: 'a\nb', new_string: 'a\nb\nc\nd' } })]} />)
    expect(screen.getByTestId('activity-diff')).toHaveTextContent('+2')
    expect(screen.getByTestId('activity-diff')).not.toHaveTextContent('-2')
  })

  it('renders nothing for an empty projection', () => {
    render(<ToolActivityBlock {...props} entries={[]} />)
    expect(screen.queryByTestId('tool-activity-block')).not.toBeInTheDocument()
  })
})
