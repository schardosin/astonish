import yaml from 'js-yaml'

// --- Types ---

/* eslint-disable @typescript-eslint/no-explicit-any */
type YamlData = Record<string, any>
type YamlNode = Record<string, any>
/* eslint-enable @typescript-eslint/no-explicit-any */

interface LayoutPosition {
  x: number
  y: number
}

interface FlowItem {
  from: string
  to?: string
  edges?: FlowEdge[]
}

interface FlowEdge {
  to: string
  condition?: string
}

// --- Functions ---

export function orderYamlKeys(data: YamlData): YamlData {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return data
  }
  
  const keyOrder = ['name', 'description', 'nodes', 'flow', 'mcp_dependencies', 'layout']
  const excludedKeys = ['model']
  
  const ordered: YamlData = {}
  
  for (const key of keyOrder) {
    if (key in data) {
      ordered[key] = data[key]
    }
  }
  
  const remainingKeys = Object.keys(data)
    .filter(k => !keyOrder.includes(k) && !excludedKeys.includes(k))
    .sort()
  
  for (const key of remainingKeys) {
    ordered[key] = data[key]
  }
  
  return ordered
}

type NodeTemplateFn = (name: string) => YamlNode

const NODE_TEMPLATES: Record<string, NodeTemplateFn> = {
  input: (name) => ({
    name,
    type: 'input',
    prompt: 'Enter your input:',
    output_model: {
      value: 'str'
    }
  }),
  
  llm: (name) => ({
    name,
    type: 'llm',
    system: 'You are a helpful assistant.',
    prompt: 'Enter your prompt here',
    output_model: {
      response: 'str'
    }
  }),
  
  tool: (name) => ({
    name,
    type: 'tool',
    tool_name: 'tool_name_here',
    output_model: {
      result: 'str'
    }
  }),
  
  updateState: (name) => ({
    name,
    type: 'update_state',
    updates: {
      key: 'value'
    }
  }),
  
  update_state: (name) => ({
    name,
    type: 'update_state',
    updates: {
      key: 'value'
    }
  }),
  
  output: (name) => ({
    name,
    type: 'output',
    user_message: ['output_here']
  }),
}

export function generateNodeName(type: string, existingNames: Set<string>): string {
  const baseName = type === 'updateState' ? 'update_state' : type
  let counter = 1
  let name = `${baseName}_${counter}`
  
  while (existingNames.has(name)) {
    counter++
    name = `${baseName}_${counter}`
  }
  
  return name
}

export function addStandaloneNode(yamlContent: string, nodeType: string): string {
  try {
    const yamlData = (yaml.load(yamlContent) as YamlData) || { nodes: [], flow: [] }
    
    const existingNames = new Set<string>((yamlData.nodes || []).map((n: YamlNode) => n.name as string))
    existingNames.add('START')
    existingNames.add('END')
    
    const newName = generateNodeName(nodeType, existingNames)
    
    const templateFn = NODE_TEMPLATES[nodeType] || NODE_TEMPLATES.llm
    const newNode = templateFn(newName)
    
    yamlData.nodes = yamlData.nodes || []
    yamlData.nodes.push(newNode)
    
    yamlData.layout = yamlData.layout || { nodes: {}, edges: {} }
    yamlData.layout.nodes = yamlData.layout.nodes || {}
    
    let maxX = 200
    let sumY = 150
    let nodeCount = 0
    Object.values(yamlData.layout.nodes as Record<string, LayoutPosition>).forEach((pos) => {
      if (pos.x > maxX) maxX = pos.x
      sumY += pos.y
      nodeCount++
    })
    const avgY = nodeCount > 0 ? sumY / nodeCount : 150
    
    yamlData.layout.nodes[newName] = {
      x: Math.round(maxX + 200),
      y: Math.round(avgY)
    }
    
    return yaml.dump(orderYamlKeys(yamlData), { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false,
      styles: { '!!str': 'literal' }
    })
  } catch (e) {
    console.error('Error adding standalone node:', e)
    return yamlContent
  }
}

export function addConnection(yamlContent: string, sourceId: string, targetId: string): string {
  try {
    const yamlData = (yaml.load(yamlContent) as YamlData) || { nodes: [], flow: [] }
    yamlData.flow = yamlData.flow || []
    
    const exists = yamlData.flow.some((f: FlowItem) => 
      f.from === sourceId && f.to === targetId
    )
    
    if (exists) {
      return yamlContent
    }
    
    const existingEdgeIndex = yamlData.flow.findIndex((f: FlowItem) => 
      f.from === sourceId && f.to && !f.edges
    )
    
    if (existingEdgeIndex !== -1) {
      const existingTarget = yamlData.flow[existingEdgeIndex].to
      yamlData.flow[existingEdgeIndex] = {
        from: sourceId,
        edges: [
          { to: existingTarget },
          { to: targetId }
        ]
      }
    } else {
      const existingEdgesIndex = yamlData.flow.findIndex((f: FlowItem) => 
        f.from === sourceId && f.edges
      )
      
      if (existingEdgesIndex !== -1) {
        yamlData.flow[existingEdgesIndex].edges.push({ to: targetId })
      } else {
        yamlData.flow.push({
          from: sourceId,
          to: targetId
        })
      }
    }
    
    return yaml.dump(orderYamlKeys(yamlData), { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false,
      styles: { '!!str': 'literal' }
    })
  } catch (e) {
    console.error('Error adding connection:', e)
    return yamlContent
  }
}

export function removeConnection(yamlContent: string, sourceId: string, targetId: string): string {
  try {
    const yamlData = (yaml.load(yamlContent) as YamlData) || { nodes: [], flow: [] }
    yamlData.flow = yamlData.flow || []
    
    for (let i = yamlData.flow.length - 1; i >= 0; i--) {
      const flowItem: FlowItem = yamlData.flow[i]
      
      if (flowItem.from === sourceId) {
        if (flowItem.to === targetId) {
          yamlData.flow.splice(i, 1)
        } else if (flowItem.edges) {
          flowItem.edges = flowItem.edges.filter((e: FlowEdge) => e.to !== targetId)
          
          if (flowItem.edges.length === 1) {
            yamlData.flow[i] = {
              from: sourceId,
              to: flowItem.edges[0].to
            }
          } else if (flowItem.edges.length === 0) {
            yamlData.flow.splice(i, 1)
          }
        }
      }
    }
    
    return yaml.dump(orderYamlKeys(yamlData), { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false,
      styles: { '!!str': 'literal' }
    })
  } catch (e) {
    console.error('Error removing connection:', e)
    return yamlContent
  }
}

export function removeNode(yamlContent: string, nodeId: string): string {
  try {
    const yamlData = (yaml.load(yamlContent) as YamlData) || { nodes: [], flow: [] }
    
    yamlData.nodes = (yamlData.nodes || []).filter((n: YamlNode) => n.name !== nodeId)
    
    yamlData.flow = (yamlData.flow || []).filter((f: FlowItem) => {
      if (f.from === nodeId) return false
      if (f.to === nodeId) return false
      if (f.edges) {
        f.edges = f.edges.filter((e: FlowEdge) => e.to !== nodeId)
        return f.edges.length > 0
      }
      return true
    })
    
    return yaml.dump(orderYamlKeys(yamlData), { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false,
      styles: { '!!str': 'literal' }
    })
  } catch (e) {
    console.error('Error removing node:', e)
    return yamlContent
  }
}

export function updateNode(yamlContent: string, nodeId: string, newNodeData: YamlNode): string {
  try {
    const yamlData = (yaml.load(yamlContent) as YamlData) || { nodes: [], flow: [] }
    yamlData.nodes = yamlData.nodes || []
    
    const nodeIndex = yamlData.nodes.findIndex((n: YamlNode) => n.name === nodeId)
    
    if (nodeIndex !== -1) {
      const oldName = yamlData.nodes[nodeIndex].name as string
      const newName = newNodeData.name as string
      
      if (oldName !== newName) {
        yamlData.flow = (yamlData.flow || []).map((f: FlowItem) => {
          const updated = { ...f }
          if (updated.from === oldName) updated.from = newName
          if (updated.to === oldName) updated.to = newName
          if (updated.edges) {
            updated.edges = updated.edges.map((e: FlowEdge) => ({
              ...e,
              to: e.to === oldName ? newName : e.to
            }))
          }
          return updated
        })
      }
      
      yamlData.nodes[nodeIndex] = newNodeData
    }
    
    return yaml.dump(orderYamlKeys(yamlData), { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false,
      styles: { '!!str': 'literal' }
    })
  } catch (e) {
    console.error('Error updating node:', e)
    return yamlContent
  }
}
