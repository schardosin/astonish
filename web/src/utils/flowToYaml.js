import yaml from 'js-yaml'

/**
 * Enforce consistent YAML key ordering
 * Order: name, description, nodes, flow, layout, then any remaining keys alphabetically
 */
export function orderYamlKeys(data) {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return data
  }
  
  // Define the preferred key order (model is NOT included - engine uses global config)
  // mcp_dependencies goes before layout, layout is always last
  const keyOrder = ['name', 'description', 'nodes', 'flow', 'mcp_dependencies', 'layout']
  
  // Keys to explicitly exclude from output (deprecated or invalid fields)
  const excludedKeys = ['model']
  
  const ordered = {}
  
  // First, add keys in preferred order
  for (const key of keyOrder) {
    if (key in data) {
      ordered[key] = data[key]
    }
  }
  
  // Then add any remaining keys alphabetically (excluding deprecated keys)
  const remainingKeys = Object.keys(data)
    .filter(k => !keyOrder.includes(k) && !excludedKeys.includes(k))
    .sort()
  
  for (const key of remainingKeys) {
    ordered[key] = data[key]
  }
  
  return ordered
}

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
  
  // Alias for update_state (popover uses snake_case)
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

/**
 * Generate a unique node name
 */
export function generateNodeName(type, existingNames) {
  const baseName = type === 'updateState' ? 'update_state' : type
  let counter = 1
  let name = `${baseName}_${counter}`
  
  while (existingNames.has(name)) {
    counter++
    name = `${baseName}_${counter}`
  }
  
  return name
}

/**
 * Add a new standalone node (not connected to flow)
 * Returns updated YAML string
 */
export function addStandaloneNode(yamlContent, nodeType) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    
    // Get existing node names
    const existingNames = new Set((yamlData.nodes || []).map(n => n.name))
    existingNames.add('START')
    existingNames.add('END')
    
    // Generate unique name
    const newName = generateNodeName(nodeType, existingNames)
    
    // Create new node from template
    const templateFn = NODE_TEMPLATES[nodeType] || NODE_TEMPLATES.llm
    const newNode = templateFn(newName)
    
    // Add to nodes array
    yamlData.nodes = yamlData.nodes || []
    yamlData.nodes.push(newNode)
    
    // Calculate a default position for the new node
    // Place it to the right of existing nodes
    yamlData.layout = yamlData.layout || { nodes: {}, edges: {} }
    yamlData.layout.nodes = yamlData.layout.nodes || {}
    
    // Find the rightmost node position
    let maxX = 200 // Default if no nodes
    let sumY = 150
    let nodeCount = 0
    Object.values(yamlData.layout.nodes || {}).forEach(pos => {
      if (pos.x > maxX) maxX = pos.x
      sumY += pos.y
      nodeCount++
    })
    const avgY = nodeCount > 0 ? sumY / nodeCount : 150
    
    // Add position for new node (200px to the right)
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

/**
 * Add a connection (edge) to the flow
 * Returns updated YAML string
 */
export function addConnection(yamlContent, sourceId, targetId) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    yamlData.flow = yamlData.flow || []
    
    // Check if this exact connection already exists
    const exists = yamlData.flow.some(f => 
      f.from === sourceId && f.to === targetId
    )
    
    if (exists) {
      return yamlContent // Connection already exists
    }
    
    // Check if source already has a "to" connection (simple edge)
    const existingEdgeIndex = yamlData.flow.findIndex(f => 
      f.from === sourceId && f.to && !f.edges
    )
    
    if (existingEdgeIndex !== -1) {
      // Convert to conditional edges array
      const existingTarget = yamlData.flow[existingEdgeIndex].to
      yamlData.flow[existingEdgeIndex] = {
        from: sourceId,
        edges: [
          { to: existingTarget },
          { to: targetId }
        ]
      }
    } else {
      // Check if source already has edges array
      const existingEdgesIndex = yamlData.flow.findIndex(f => 
        f.from === sourceId && f.edges
      )
      
      if (existingEdgesIndex !== -1) {
        // Add to existing edges array
        yamlData.flow[existingEdgesIndex].edges.push({ to: targetId })
      } else {
        // Add new simple connection
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

/**
 * Remove a connection (edge) from the flow
 * Returns updated YAML string
 */
export function removeConnection(yamlContent, sourceId, targetId) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    yamlData.flow = yamlData.flow || []
    
    // Find and remove the connection
    for (let i = yamlData.flow.length - 1; i >= 0; i--) {
      const flowItem = yamlData.flow[i]
      
      if (flowItem.from === sourceId) {
        if (flowItem.to === targetId) {
          // Simple edge - remove entirely
          yamlData.flow.splice(i, 1)
        } else if (flowItem.edges) {
          // Edges array - remove specific target
          flowItem.edges = flowItem.edges.filter(e => e.to !== targetId)
          
          // If only one edge left, convert back to simple
          if (flowItem.edges.length === 1) {
            yamlData.flow[i] = {
              from: sourceId,
              to: flowItem.edges[0].to
            }
          } else if (flowItem.edges.length === 0) {
            // No edges left, remove entirely
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

/**
 * Remove a node and all its connections from the YAML
 * Returns updated YAML string
 */
export function removeNode(yamlContent, nodeId) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    
    // Remove node from nodes array
    yamlData.nodes = (yamlData.nodes || []).filter(n => n.name !== nodeId)
    
    // Remove all flow entries involving this node
    yamlData.flow = (yamlData.flow || []).filter(f => {
      if (f.from === nodeId) return false
      if (f.to === nodeId) return false
      if (f.edges) {
        f.edges = f.edges.filter(e => e.to !== nodeId)
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

/**
 * Update an existing node's data in the YAML
 * Returns updated YAML string
 */
export function updateNode(yamlContent, nodeId, newNodeData) {
  try {
    const yamlData = yaml.load(yamlContent) || { nodes: [], flow: [] }
    yamlData.nodes = yamlData.nodes || []
    
    // Find and update the node
    const nodeIndex = yamlData.nodes.findIndex(n => n.name === nodeId)
    
    if (nodeIndex !== -1) {
      // If name changed, update flow references too
      const oldName = yamlData.nodes[nodeIndex].name
      const newName = newNodeData.name
      
      if (oldName !== newName) {
        // Update flow references
        yamlData.flow = (yamlData.flow || []).map(f => {
          const updated = { ...f }
          if (updated.from === oldName) updated.from = newName
          if (updated.to === oldName) updated.to = newName
          if (updated.edges) {
            updated.edges = updated.edges.map(e => ({
              ...e,
              to: e.to === oldName ? newName : e.to
            }))
          }
          return updated
        })
      }
      
      // Update the node data
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
