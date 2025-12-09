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
    
    // Note: We don't add to flow - the node is standalone until connected
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
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
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
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
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
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
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
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
    
    return yaml.dump(yamlData, { 
      lineWidth: -1,
      noRefs: true,
      quotingType: '"',
      forceQuotes: false
    })
  } catch (e) {
    console.error('Error updating node:', e)
    return yamlContent
  }
}
