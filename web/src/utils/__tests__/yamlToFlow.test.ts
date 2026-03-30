import { describe, it, expect } from 'vitest'
import { parseNodes, parseEdges, autoLayout, yamlToFlow, extractLayout } from '../yamlToFlow'

describe('parseNodes', () => {
  it('returns START and END nodes for empty YAML', () => {
    const nodes = parseNodes({ nodes: [] })
    expect(nodes).toHaveLength(2)
    expect(nodes[0].id).toBe('START')
    expect(nodes[0].type).toBe('start')
    expect(nodes[1].id).toBe('END')
    expect(nodes[1].type).toBe('end')
  })

  it('parses LLM node', () => {
    const yamlData = {
      nodes: [{ name: 'my_llm', type: 'llm', system: 'sys', prompt: 'p', output_model: { r: 'str' } }],
    }
    const nodes = parseNodes(yamlData)
    expect(nodes).toHaveLength(3) // START + my_llm + END
    expect(nodes[1].id).toBe('my_llm')
    expect(nodes[1].type).toBe('llm')
    expect(nodes[1].data.label).toBe('my_llm')
    expect(nodes[1].data.hasError).toBe(false)
  })

  it('maps update_state to updateState type', () => {
    const yamlData = {
      nodes: [{ name: 'us', type: 'update_state', updates: { k: 'v' } }],
    }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].type).toBe('updateState')
  })

  it('uses saved layout positions', () => {
    const yamlData = {
      nodes: [{ name: 'n1', type: 'llm', system: 's', prompt: 'p', output_model: { r: 'str' } }],
    }
    const layout = { nodes: { START: { x: 10, y: 20 }, n1: { x: 100, y: 200 }, END: { x: 10, y: 500 } }, edges: {} }
    const nodes = parseNodes(yamlData, layout)
    expect(nodes[0].position).toEqual({ x: 10, y: 20 })
    expect(nodes[1].position).toEqual({ x: 100, y: 200 })
    expect(nodes[2].position).toEqual({ x: 10, y: 500 })
  })

  it('flags validation errors for missing type', () => {
    const yamlData = { nodes: [{ name: 'bad' }] }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].data.hasError).toBe(true)
    expect(nodes[1].data.errorMessage).toContain('type')
  })

  it('flags validation errors for invalid type', () => {
    const yamlData = { nodes: [{ name: 'bad', type: 'unknown_type' }] }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].data.hasError).toBe(true)
    expect(nodes[1].data.errorMessage).toContain('must be one of')
  })

  it('flags missing output_model for llm/input/tool types', () => {
    const yamlData = { nodes: [{ name: 'n', type: 'llm', system: 's', prompt: 'p' }] }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].data.hasError).toBe(true)
    expect((nodes[1].data.errorMessage as string)).toContain('output_model')
  })

  it('flags missing system and prompt for llm nodes', () => {
    const yamlData = { nodes: [{ name: 'n', type: 'llm', output_model: { r: 'str' } }] }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].data.hasError).toBe(true)
    expect((nodes[1].data.errorMessage as string)).toContain('system')
    expect((nodes[1].data.errorMessage as string)).toContain('prompt')
  })

  it('flags missing user_message for output nodes', () => {
    const yamlData = { nodes: [{ name: 'n', type: 'output' }] }
    const nodes = parseNodes(yamlData)
    expect(nodes[1].data.hasError).toBe(true)
    expect((nodes[1].data.errorMessage as string)).toContain('user_message')
  })

  it('handles node without name gracefully', () => {
    const yamlData = { nodes: [{ type: 'llm' }] }
    const nodes = parseNodes(yamlData)
    expect(nodes).toHaveLength(3)
    expect(nodes[1].id).toBe('error_node_0')
    expect(nodes[1].data.hasError).toBe(true)
  })
})

describe('parseEdges', () => {
  it('returns empty array for no flow', () => {
    expect(parseEdges({})).toEqual([])
    expect(parseEdges({ flow: [] })).toEqual([])
  })

  it('parses simple flow connections', () => {
    const yamlData = {
      flow: [
        { from: 'START', to: 'a' },
        { from: 'a', to: 'END' },
      ],
    }
    const edges = parseEdges(yamlData)
    expect(edges).toHaveLength(2)
    expect(edges[0].source).toBe('START')
    expect(edges[0].target).toBe('a')
    expect(edges[1].source).toBe('a')
    expect(edges[1].target).toBe('END')
  })

  it('parses conditional edges', () => {
    const yamlData = {
      flow: [
        {
          from: 'check',
          edges: [
            { to: 'yes', condition: "lambda x: x['result'] == 'true'" },
            { to: 'no', condition: "lambda x: x['result'] == 'false'" },
          ],
        },
      ],
    }
    const edges = parseEdges(yamlData)
    expect(edges).toHaveLength(2)
    expect(edges[0].target).toBe('yes')
    expect(edges[0].label).toBe('result == "true"')
    expect(edges[1].target).toBe('no')
    expect(edges[1].label).toBe('result == "false"')
  })

  it('handles x.get() condition format', () => {
    const yamlData = {
      flow: [
        {
          from: 'check',
          edges: [
            { to: 'yes', condition: "lambda x: x.get('status') == 'done'" },
          ],
        },
      ],
    }
    const edges = parseEdges(yamlData)
    expect(edges[0].label).toBe('status == "done"')
  })

  it('truncates long unmatched conditions', () => {
    const yamlData = {
      flow: [
        {
          from: 'check',
          edges: [
            { to: 'a', condition: 'lambda x: some_really_long_complex_expression_that_does_not_match' },
          ],
        },
      ],
    }
    const edges = parseEdges(yamlData)
    expect(edges[0].label!.length).toBeLessThanOrEqual(30)
  })

  it('skips START->END edge when other logic exists', () => {
    const yamlData = {
      flow: [
        { from: 'START', to: 'END' },
        { from: 'START', to: 'a' },
      ],
    }
    const edges = parseEdges(yamlData)
    expect(edges).toHaveLength(1)
    expect(edges[0].target).toBe('a')
  })

  it('uses saved edge points', () => {
    const yamlData = { flow: [{ from: 'a', to: 'b' }] }
    const savedEdges = { 'a->b': [{ x: 10, y: 20 }, { x: 30, y: 40 }] }
    const edges = parseEdges(yamlData, {}, savedEdges)
    expect(edges[0].data?.points).toEqual(savedEdges['a->b'])
  })
})

describe('autoLayout', () => {
  it('stacks nodes vertically', () => {
    const nodes = [
      { id: 'START', type: 'start', position: { x: 0, y: 0 }, data: { label: 'START' } },
      { id: 'a', type: 'llm', position: { x: 0, y: 0 }, data: { label: 'a' } },
      { id: 'END', type: 'end', position: { x: 0, y: 0 }, data: { label: 'END' } },
    ]
    const laid = autoLayout(nodes, [])
    expect(laid[0].position.x).toBe(200)
    expect(laid[0].position.y).toBe(0)
    expect(laid[1].position.y).toBeGreaterThan(0)
    expect(laid[2].position.y).toBeGreaterThan(laid[1].position.y)
  })
})

describe('yamlToFlow', () => {
  it('returns empty nodes and edges for null input', () => {
    const result = yamlToFlow(null as any)
    expect(result.nodes).toEqual([])
    expect(result.edges).toEqual([])
  })

  it('parses a complete flow', () => {
    const yamlData = {
      name: 'test',
      nodes: [
        { name: 'greet', type: 'llm', system: 'sys', prompt: 'hi', output_model: { msg: 'str' } },
      ],
      flow: [
        { from: 'START', to: 'greet' },
        { from: 'greet', to: 'END' },
      ],
    }
    const result = yamlToFlow(yamlData)
    expect(result.nodes).toHaveLength(3) // START, greet, END
    expect(result.edges).toHaveLength(2)
  })

  it('uses saved layout when present', () => {
    const yamlData = {
      nodes: [{ name: 'a', type: 'llm', system: 's', prompt: 'p', output_model: { r: 'str' } }],
      flow: [],
      layout: { nodes: { START: { x: 10, y: 20 }, a: { x: 100, y: 200 }, END: { x: 10, y: 400 } }, edges: {} },
    }
    const result = yamlToFlow(yamlData)
    expect(result.nodes[1].position).toEqual({ x: 100, y: 200 })
  })

  it('uses autoLayout when no saved layout', () => {
    const yamlData = {
      nodes: [{ name: 'a', type: 'llm', system: 's', prompt: 'p', output_model: { r: 'str' } }],
      flow: [],
    }
    const result = yamlToFlow(yamlData)
    // autoLayout places at x=200
    expect(result.nodes[0].position.x).toBe(200)
  })
})

describe('extractLayout', () => {
  it('extracts node positions', () => {
    const nodes = [
      { id: 'START', type: 'start', position: { x: 10.6, y: 20.3 }, data: {} },
      { id: 'a', type: 'llm', position: { x: 100.9, y: 200.1 }, data: {} },
    ]
    const layout = extractLayout(nodes, [])
    expect(layout.nodes.START).toEqual({ x: 11, y: 20 })
    expect(layout.nodes.a).toEqual({ x: 101, y: 200 })
  })

  it('excludes waypoint nodes', () => {
    const nodes = [
      { id: 'w1', type: 'waypoint', position: { x: 0, y: 0 }, data: {} },
      { id: 'a', type: 'llm', position: { x: 100, y: 200 }, data: {} },
    ]
    const layout = extractLayout(nodes, [])
    expect(layout.nodes).not.toHaveProperty('w1')
    expect(layout.nodes).toHaveProperty('a')
  })

  it('extracts edge points', () => {
    const edges = [
      {
        id: 'e1',
        source: 'a',
        target: 'b',
        animated: true,
        style: {},
        type: 'editable',
        data: { points: [{ x: 10.6, y: 20.3 }, { x: 30.9, y: 40.1 }] },
      },
    ]
    const layout = extractLayout([], edges)
    expect(layout.edges['a->b']).toEqual([{ x: 11, y: 20 }, { x: 31, y: 40 }])
  })

  it('skips edges without points', () => {
    const edges = [
      { id: 'e1', source: 'a', target: 'b', animated: true, style: {}, type: 'editable', data: {} },
    ]
    const layout = extractLayout([], edges)
    expect(layout.edges).toEqual({})
  })
})
