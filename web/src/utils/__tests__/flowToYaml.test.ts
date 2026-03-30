import { describe, it, expect } from 'vitest'
import {
  orderYamlKeys,
  generateNodeName,
  addStandaloneNode,
  addConnection,
  removeConnection,
  removeNode,
  updateNode,
} from '../flowToYaml'
import yaml from 'js-yaml'

describe('orderYamlKeys', () => {
  it('orders keys in canonical order', () => {
    const data = { flow: [], description: 'desc', name: 'test', nodes: [] }
    const ordered = orderYamlKeys(data)
    const keys = Object.keys(ordered)
    expect(keys).toEqual(['name', 'description', 'nodes', 'flow'])
  })

  it('excludes model key', () => {
    const data = { name: 'test', model: 'gpt-4', description: 'desc' }
    const ordered = orderYamlKeys(data)
    expect(ordered).not.toHaveProperty('model')
  })

  it('appends unknown keys alphabetically after canonical keys', () => {
    const data = { name: 'test', zebra: 1, alpha: 2 }
    const ordered = orderYamlKeys(data)
    const keys = Object.keys(ordered)
    expect(keys).toEqual(['name', 'alpha', 'zebra'])
  })

  it('returns non-object input as-is', () => {
    expect(orderYamlKeys(null as any)).toBeNull()
    expect(orderYamlKeys([] as any)).toEqual([])
  })
})

describe('generateNodeName', () => {
  it('generates llm_1 for llm type with no existing names', () => {
    expect(generateNodeName('llm', new Set())).toBe('llm_1')
  })

  it('increments counter when name exists', () => {
    expect(generateNodeName('llm', new Set(['llm_1']))).toBe('llm_2')
  })

  it('skips multiple existing names', () => {
    expect(generateNodeName('llm', new Set(['llm_1', 'llm_2', 'llm_3']))).toBe('llm_4')
  })

  it('handles updateState type conversion', () => {
    expect(generateNodeName('updateState', new Set())).toBe('update_state_1')
  })

  it('handles tool type', () => {
    expect(generateNodeName('tool', new Set())).toBe('tool_1')
  })
})

describe('addStandaloneNode', () => {
  const baseYaml = yaml.dump({ name: 'test', nodes: [], flow: [] })

  it('adds an LLM node to empty flow', () => {
    const result = addStandaloneNode(baseYaml, 'llm')
    const data = yaml.load(result) as any
    expect(data.nodes).toHaveLength(1)
    expect(data.nodes[0].name).toBe('llm_1')
    expect(data.nodes[0].type).toBe('llm')
    expect(data.nodes[0].system).toBeDefined()
    expect(data.nodes[0].prompt).toBeDefined()
  })

  it('adds an input node', () => {
    const result = addStandaloneNode(baseYaml, 'input')
    const data = yaml.load(result) as any
    expect(data.nodes[0].type).toBe('input')
    expect(data.nodes[0].prompt).toBeDefined()
  })

  it('adds a tool node', () => {
    const result = addStandaloneNode(baseYaml, 'tool')
    const data = yaml.load(result) as any
    expect(data.nodes[0].type).toBe('tool')
    expect(data.nodes[0].tool_name).toBe('tool_name_here')
  })

  it('adds an output node', () => {
    const result = addStandaloneNode(baseYaml, 'output')
    const data = yaml.load(result) as any
    expect(data.nodes[0].type).toBe('output')
    expect(data.nodes[0].user_message).toEqual(['output_here'])
  })

  it('generates unique names when nodes exist', () => {
    const existing = yaml.dump({
      name: 'test',
      nodes: [{ name: 'llm_1', type: 'llm' }],
      flow: [],
    })
    const result = addStandaloneNode(existing, 'llm')
    const data = yaml.load(result) as any
    expect(data.nodes).toHaveLength(2)
    expect(data.nodes[1].name).toBe('llm_2')
  })

  it('creates layout positions', () => {
    const result = addStandaloneNode(baseYaml, 'llm')
    const data = yaml.load(result) as any
    expect(data.layout).toBeDefined()
    expect(data.layout.nodes.llm_1).toBeDefined()
    expect(data.layout.nodes.llm_1.x).toBeGreaterThan(0)
  })
})

describe('addConnection', () => {
  const baseYaml = yaml.dump({
    name: 'test',
    nodes: [{ name: 'a', type: 'llm' }, { name: 'b', type: 'llm' }],
    flow: [],
  })

  it('adds a simple connection', () => {
    const result = addConnection(baseYaml, 'START', 'a')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(1)
    expect(data.flow[0]).toEqual({ from: 'START', to: 'a' })
  })

  it('does not add duplicate connection', () => {
    const withConn = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm' }],
      flow: [{ from: 'START', to: 'a' }],
    })
    const result = addConnection(withConn, 'START', 'a')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(1)
  })

  it('converts simple to edges array when adding second target', () => {
    const withConn = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm' }, { name: 'b', type: 'llm' }],
      flow: [{ from: 'START', to: 'a' }],
    })
    const result = addConnection(withConn, 'START', 'b')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(1)
    expect(data.flow[0].edges).toHaveLength(2)
    expect(data.flow[0].edges[0].to).toBe('a')
    expect(data.flow[0].edges[1].to).toBe('b')
  })

  it('appends to existing edges array', () => {
    const withEdges = yaml.dump({
      name: 'test',
      nodes: [],
      flow: [{ from: 'START', edges: [{ to: 'a' }, { to: 'b' }] }],
    })
    const result = addConnection(withEdges, 'START', 'c')
    const data = yaml.load(result) as any
    expect(data.flow[0].edges).toHaveLength(3)
    expect(data.flow[0].edges[2].to).toBe('c')
  })
})

describe('removeConnection', () => {
  it('removes a simple connection', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [],
      flow: [{ from: 'START', to: 'a' }],
    })
    const result = removeConnection(yamlStr, 'START', 'a')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(0)
  })

  it('removes from edges array and simplifies to simple when one remains', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [],
      flow: [{ from: 'START', edges: [{ to: 'a' }, { to: 'b' }] }],
    })
    const result = removeConnection(yamlStr, 'START', 'a')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(1)
    expect(data.flow[0].to).toBe('b')
    expect(data.flow[0].edges).toBeUndefined()
  })

  it('removes flow item when all edges removed', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [],
      flow: [{ from: 'START', edges: [{ to: 'a' }] }],
    })
    const result = removeConnection(yamlStr, 'START', 'a')
    const data = yaml.load(result) as any
    expect(data.flow).toHaveLength(0)
  })
})

describe('removeNode', () => {
  it('removes node and its flow references', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm' }, { name: 'b', type: 'llm' }],
      flow: [
        { from: 'START', to: 'a' },
        { from: 'a', to: 'b' },
        { from: 'b', to: 'END' },
      ],
    })
    const result = removeNode(yamlStr, 'a')
    const data = yaml.load(result) as any
    expect(data.nodes).toHaveLength(1)
    expect(data.nodes[0].name).toBe('b')
    expect(data.flow).toHaveLength(1)
    expect(data.flow[0]).toEqual({ from: 'b', to: 'END' })
  })

  it('removes node from edges array', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm' }, { name: 'b', type: 'llm' }],
      flow: [{ from: 'START', edges: [{ to: 'a' }, { to: 'b' }] }],
    })
    const result = removeNode(yamlStr, 'a')
    const data = yaml.load(result) as any
    expect(data.nodes).toHaveLength(1)
    expect(data.flow[0].edges).toHaveLength(1)
    expect(data.flow[0].edges[0].to).toBe('b')
  })
})

describe('updateNode', () => {
  it('updates node data', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm', system: 'old' }],
      flow: [],
    })
    const result = updateNode(yamlStr, 'a', { name: 'a', type: 'llm', system: 'new' })
    const data = yaml.load(result) as any
    expect(data.nodes[0].system).toBe('new')
  })

  it('updates flow references when node is renamed', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [{ name: 'old_name', type: 'llm' }],
      flow: [
        { from: 'START', to: 'old_name' },
        { from: 'old_name', to: 'END' },
      ],
    })
    const result = updateNode(yamlStr, 'old_name', { name: 'new_name', type: 'llm' })
    const data = yaml.load(result) as any
    expect(data.nodes[0].name).toBe('new_name')
    expect(data.flow[0].to).toBe('new_name')
    expect(data.flow[1].from).toBe('new_name')
  })

  it('updates edge targets when node is renamed', () => {
    const yamlStr = yaml.dump({
      name: 'test',
      nodes: [{ name: 'a', type: 'llm' }],
      flow: [{ from: 'START', edges: [{ to: 'a' }, { to: 'END' }] }],
    })
    const result = updateNode(yamlStr, 'a', { name: 'b', type: 'llm' })
    const data = yaml.load(result) as any
    expect(data.flow[0].edges[0].to).toBe('b')
  })
})
