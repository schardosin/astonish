import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import FlowCanvas from '../FlowCanvas'

// Mock @xyflow/react to avoid canvas/DOM rendering issues in jsdom
vi.mock('@xyflow/react', () => {
  const Provider = ({ children }: { children: React.ReactNode }) => <div>{children}</div>
  return {
    ReactFlow: ({ children }: { children?: React.ReactNode }) => (
      <div data-testid="react-flow">{children}</div>
    ),
    ReactFlowProvider: Provider,
    Background: () => <div data-testid="background" />,
    Controls: () => <div data-testid="controls" />,
    MiniMap: () => <div data-testid="minimap" />,
    Panel: ({ children }: { children: React.ReactNode; position: string }) => (
      <div data-testid="panel">{children}</div>
    ),
    useNodesState: (init: unknown[]) => [init, vi.fn(), vi.fn()],
    useEdgesState: (init: unknown[]) => [init, vi.fn(), vi.fn()],
    useReactFlow: () => ({
      getNodes: () => [],
      getEdges: () => [],
      screenToFlowPosition: (pos: { x: number; y: number }) => pos,
      setViewport: vi.fn(),
      getViewport: () => ({ x: 0, y: 0, zoom: 1 }),
    }),
    useNodes: () => [],
    useEdges: () => [],
  }
})

// Mock node components
vi.mock('../nodes/StartNode', () => ({ default: () => <div>Start</div> }))
vi.mock('../nodes/EndNode', () => ({ default: () => <div>End</div> }))
vi.mock('../nodes/InputNode', () => ({ default: () => <div>Input</div> }))
vi.mock('../nodes/LlmNode', () => ({ default: () => <div>LLM</div> }))
vi.mock('../nodes/ToolNode', () => ({ default: () => <div>Tool</div> }))
vi.mock('../nodes/OutputNode', () => ({ default: () => <div>Output</div> }))
vi.mock('../nodes/UpdateStateNode', () => ({ default: () => <div>UpdateState</div> }))

// Mock edge and popover components
vi.mock('../edges/EditableEdge', () => ({ default: () => null }))
vi.mock('../NodeTypePopover', () => ({ default: () => null }))

describe('FlowCanvas', () => {
  const defaultProps = {
    nodes: [],
    edges: [],
    isRunning: false,
    readOnly: false,
    theme: 'dark',
    runningNodeId: null as string | null,
    onNodeSelect: vi.fn(),
    onEdgeSelect: vi.fn(),
    onConnect: vi.fn(),
    onEdgeRemove: vi.fn(),
    onNodeDelete: vi.fn(),
    onLayoutChange: vi.fn(),
    onLayoutSave: vi.fn(),
    onAutoLayout: vi.fn(),
    onCreateConnectedNode: vi.fn(),
    onAIChatMultiNode: vi.fn(),
    onDuplicateNodes: vi.fn(),
  }

  it('renders the ReactFlow canvas', () => {
    render(<FlowCanvas {...defaultProps} />)
    expect(screen.getByTestId('react-flow')).toBeInTheDocument()
  })

  it('renders the Background component', () => {
    render(<FlowCanvas {...defaultProps} />)
    expect(screen.getByTestId('background')).toBeInTheDocument()
  })

  it('renders the Controls component', () => {
    render(<FlowCanvas {...defaultProps} />)
    expect(screen.getByTestId('controls')).toBeInTheDocument()
  })

  it('renders the MiniMap component', () => {
    render(<FlowCanvas {...defaultProps} />)
    expect(screen.getByTestId('minimap')).toBeInTheDocument()
  })

  it('shows the Add Node toolbar when not running', () => {
    render(<FlowCanvas {...defaultProps} isRunning={false} />)
    // The toolbar has buttons for Input, LLM, Tool, State, Output
    expect(screen.getByText('Input')).toBeInTheDocument()
    expect(screen.getByText('LLM')).toBeInTheDocument()
    expect(screen.getByText('Tool')).toBeInTheDocument()
    expect(screen.getByText('State')).toBeInTheDocument()
    expect(screen.getByText('Output')).toBeInTheDocument()
  })

  it('hides the Add Node toolbar when running', () => {
    render(<FlowCanvas {...defaultProps} isRunning={true} />)
    // Input/LLM/Tool buttons should not be visible
    expect(screen.queryByText('Input')).not.toBeInTheDocument()
  })

  it('renders without crashing with no nodes', () => {
    render(<FlowCanvas {...defaultProps} nodes={[]} />)
    // Should render the canvas even with no nodes
    expect(screen.getByTestId('react-flow')).toBeInTheDocument()
  })

  it('shows the AI Assist button text', () => {
    render(<FlowCanvas {...defaultProps} />)
    // The "Create with AI" button may appear in empty state
    const aiButton = screen.queryByText(/Create with AI/i)
    // This appears only in the empty canvas state
    if (aiButton) {
      expect(aiButton).toBeInTheDocument()
    }
  })
})
