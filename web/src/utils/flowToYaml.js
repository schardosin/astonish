import yaml from 'js-yaml'

/**
 * Default YAML content templates for each node type
 */
const NODE_TEMPLATES = {
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
  
  output: (name) => ({
    name,
    type: 'output',
    value: '${response}'
  }),
}

/**
 * Generate a unique node name
 */
export function generateNodeName(type, existingNodes) {
  const baseName = type.replace(/([A-Z])/g, '_$1').toLowerCase()
  let counter = 1
  let name = `${baseName}_${counter}`
  
  const existingNames = new Set(existingNodes.map(n => n.id))
  while (existingNames.has(name)) {
    counter++
    name = `${baseName}_${counter}`
  }
  
  return name
}

/**
 * Create a new node with default YAML data
 */
export function createNewNode(type, existingNodes) {
  const name = generateNodeName(type, existingNodes)
  const templateFn = NODE_TEMPLATES[type] || NODE_TEMPLATES.llm
  
  return {
    id: name,
    type,
    position: { x: 0, y: 0 }, // Will be set by layout
    data: {
      label: name,
      nodeType: type,
      yaml: templateFn(name)
    }
  }
}

/**
 * Convert React Flow nodes and edges back to YAML string
 */
export function flowToYaml(nodes, edges, description = 'Agent Flow') {
  // Filter out START and END nodes - they are implicit
  const agentNodes = nodes.filter(n => n.type !== 'start' && n.type !== 'end')
  
  // Build nodes array
  const yamlNodes = agentNodes.map(node => {
    // If node has original YAML data, use it
    if (node.data?.yaml) {
      return node.data.yaml
    }
    
    // Otherwise, create from template
    const type = node.data?.nodeType || node.type
    const templateFn = NODE_TEMPLATES[type] || NODE_TEMPLATES.llm
    return templateFn(node.id)
  })
  
  // Build flow array from edges
  const yamlFlow = []
  
  // Group edges by source to handle conditional edges
  const edgesBySource = {}
  edges.forEach(edge => {
    if (!edgesBySource[edge.source]) {
      edgesBySource[edge.source] = []
    }
    edgesBySource[edge.source].push(edge)
  })
  
  // Convert edges to flow items
  Object.entries(edgesBySource).forEach(([source, sourceEdges]) => {
    if (sourceEdges.length === 1) {
      // Simple edge
      yamlFlow.push({
        from: source,
        to: sourceEdges[0].target
      })
    } else {
      // Multiple edges from same source = conditional
      yamlFlow.push({
        from: source,
        edges: sourceEdges.map(edge => ({
          to: edge.target,
          condition: edge.data?.condition || `lambda x: True`
        }))
      })
    }
  })
  
  // Build final YAML structure
  const yamlData = {
    description,
    nodes: yamlNodes,
    flow: yamlFlow
  }
  
  return yaml.dump(yamlData, { 
    lineWidth: -1, // Don't wrap lines
    noRefs: true,
    quotingType: '"',
    forceQuotes: false
  })
}

/**
 * Add a new node to the flow after a specified node
 * Returns updated YAML string
 */
export function addNodeToFlow(yamlContent, nodeType, afterNodeId) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    
    // Generate unique name
    const existingNames = new Set((yamlData.nodes || []).map(n => n.name))
    existingNames.add('START')
    existingNames.add('END')
    
    const baseName = nodeType === 'updateState' ? 'update_state' : nodeType
    let counter = 1
    let newName = `${baseName}_${counter}`
    while (existingNames.has(newName)) {
      counter++
      newName = `${baseName}_${counter}`
    }
    
    // Create new node
    const templateFn = NODE_TEMPLATES[nodeType] || NODE_TEMPLATES.llm
    const newNode = templateFn(newName)
    
    // Add to nodes array
    yamlData.nodes = yamlData.nodes || []
    yamlData.nodes.push(newNode)
    
    // Update flow: insert new node after afterNodeId
    yamlData.flow = yamlData.flow || []
    
    // Find edge from afterNodeId
    const edgeIndex = yamlData.flow.findIndex(f => f.from === afterNodeId)
    
    if (edgeIndex !== -1) {
      const existingEdge = yamlData.flow[edgeIndex]
      const originalTarget = existingEdge.to
      
      // Update existing edge to point to new node
      existingEdge.to = newName
      
      // Add new edge from new node to original target
      yamlData.flow.splice(edgeIndex + 1, 0, {
        from: newName,
        to: originalTarget
      })
    } else {
      // No existing edge, add one from afterNodeId to new node
      yamlData.flow.push({
        from: afterNodeId,
        to: newName
      })
      
      // If afterNodeId was connected to END, reconnect
      const endEdgeIndex = yamlData.flow.findIndex(f => f.from === afterNodeId && f.to === 'END')
      if (endEdgeIndex === -1) {
        // Add edge to END
        yamlData.flow.push({
          from: newName,
          to: 'END'
        })
      }
    }
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
    })
  } catch (e) {
    console.error('Error adding node to flow:', e)
    return yamlContent
  }
}
