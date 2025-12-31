import ELK from 'elkjs/lib/elk.bundled.js'

const elk = new ELK()

/**
 * Node type mapping from YAML to React Flow
 */
const NODE_TYPE_MAP = {
  input: 'input',
  llm: 'llm',
  tool: 'tool',
  output: 'output',
  update_state: 'updateState',
}

// ELKjs layout options for clean, engineered look
const elkOptions = {
  'elk.algorithm': 'layered',
  'elk.direction': 'DOWN', // Top-to-Bottom
  
  // SPACING - Generous space for readability
  'elk.layered.spacing.nodeNodeBetweenLayers': '100', // Vertical gap between layers
  'elk.spacing.nodeNode': '100', // Horizontal gap between parallel nodes
  'elk.layered.spacing.edgeEdgeBetweenLayers': '30', // Space edges apart vertically
  'elk.layered.spacing.edgeNodeBetweenLayers': '50', // Keep edges away from nodes
  
  // EDGE ROUTING - Clean orthogonal paths
  'elk.edgeRouting': 'ORTHOGONAL', // Clean 90° edges
  'elk.layered.edgeRouting.selfLoopDistribution': 'EQUALLY', // Distribute self-loops
  
  // NODE PLACEMENT - Better vertical alignment
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF', // Better for clean vertical alignment
  'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED', // Center nodes on main axis
  'elk.layered.nodePlacement.favorStraightEdges': 'true', // Prefer straight vertical edges
  
  // CROSSING MINIMIZATION - Fewer edge crossings
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
  'elk.layered.crossingMinimization.thoroughness': '10', // Higher = fewer edge crossings
  
  // MODEL ORDER - Keep nodes in logical order
  'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
  'elk.separateConnectedComponents': 'false', // Keep subgraphs together
}

/**
 * Parse nodes from YAML agent config
 * @param {Object} yamlData - Parsed YAML object
 * @param {Object} savedLayout - Optional saved layout positions
 * @returns {Array} React Flow nodes
 */
export function parseNodes(yamlData, savedLayout = null) {
  const nodes = []
  const nodePositions = savedLayout?.nodes || {}
  
  // Add START node
  const startPos = nodePositions['START']
  nodes.push({
    id: 'START',
    type: 'start',
    position: startPos ? { x: startPos.x, y: startPos.y } : { x: 0, y: 0 },
    data: { label: 'START' }
  })
  
  // Parse defined nodes with error handling per-node
  if (yamlData.nodes && Array.isArray(yamlData.nodes)) {
    yamlData.nodes.forEach((node, index) => {
      try {
        // Validate required fields
        if (!node || typeof node !== 'object') {
          throw new Error('Node must be an object')
        }
        if (!node.name) {
          throw new Error('Node missing required "name" field')
        }
        
        // Semantic validation for common field errors
        const validationErrors = []
        const VALID_NODE_TYPES = ['input', 'llm', 'tool', 'output', 'update_state']
        
        // type is required and must be valid
        if (!node.type) {
          validationErrors.push(`'type' is required. Valid types: ${VALID_NODE_TYPES.join(', ')}`)
        } else if (!VALID_NODE_TYPES.includes(node.type)) {
          validationErrors.push(`'type' must be one of: ${VALID_NODE_TYPES.join(', ')} (got '${node.type}')`)
        }
        
        // options must be an array
        if (node.options !== undefined && !Array.isArray(node.options)) {
          validationErrors.push(`'options' must be an array (got ${typeof node.options}). Use: options: [value1, value2] or options: [variable_name]`)
        }
        
        // output_model should be an object when present
        if (node.output_model !== undefined && typeof node.output_model !== 'object') {
          validationErrors.push(`'output_model' must be an object (got ${typeof node.output_model})`)
        }
        
        // output_model is required for input, llm, and tool types
        const TYPES_REQUIRING_OUTPUT_MODEL = ['input', 'llm', 'tool']
        if (TYPES_REQUIRING_OUTPUT_MODEL.includes(node.type) && !node.output_model) {
          validationErrors.push(`'output_model' is required for '${node.type}' nodes to store results in state`)
        }
        
        
        // system and prompt are required for llm nodes
        if (node.type === 'llm') {
          if (!node.system) {
            validationErrors.push(`'system' is required for llm nodes to set the AI's behavior`)
          }
          if (!node.prompt) {
            validationErrors.push(`'prompt' is required for llm nodes to provide the input to the AI`)
          }
        }
        
        // user_message is required for output nodes and should be an array
        if (node.type === 'output') {
          if (!node.user_message) {
            validationErrors.push(`'user_message' is required for output nodes to display content to the user`)
          } else if (!Array.isArray(node.user_message)) {
            validationErrors.push(`'user_message' must be an array (got ${typeof node.user_message})`)
          }
        } else if (node.user_message !== undefined && !Array.isArray(node.user_message)) {
          // For other node types, user_message is optional but must be an array if present
          validationErrors.push(`'user_message' must be an array (got ${typeof node.user_message})`)
        }
        
        const nodeType = NODE_TYPE_MAP[node.type] || 'llm'
        const savedPos = nodePositions[node.name]
        
        nodes.push({
          id: node.name,
          type: nodeType,
          position: savedPos ? { x: savedPos.x, y: savedPos.y } : { x: 0, y: 0 },
          data: {
            label: node.name,
            nodeType: node.type,
            yaml: node,
            hasError: validationErrors.length > 0,
            errorMessage: validationErrors.length > 0 ? validationErrors.join('; ') : undefined
          }
        })
      } catch (error) {
        // Create error node placeholder so flow doesn't break
        const nodeName = node?.name || `error_node_${index}`
        console.error(`Error parsing node at index ${index}:`, error)
        nodes.push({
          id: nodeName,
          type: 'llm', // Default fallback type
          position: { x: 0, y: 0 },
          data: {
            label: nodeName,
            nodeType: node?.type || 'unknown',
            yaml: node || {},
            hasError: true,
            errorMessage: error.message
          }
        })
      }
    })
  }
  
  // Add END node
  const endPos = nodePositions['END']
  if (!endPos) {
    // Silent default
  }
    
  nodes.push({
    id: 'END',
    type: 'end',
    // Default Y to 500 to give plenty of space from START for adding nodes
    position: endPos ? { x: endPos.x, y: endPos.y } : { x: 0, y: 500 },
    data: { label: 'END' }
  })
  
  return nodes
}

/**
 * Parse edges from YAML flow config
 * @param {Object} yamlData - Parsed YAML object
 * @param {Object} nodePositions - Map of node ID to position { x, y }
 * @returns {Array} React Flow edges
 */
export function parseEdges(yamlData, nodePositions = {}, savedEdges = {}) {
  const edges = []
  
  if (yamlData.flow && Array.isArray(yamlData.flow)) {
    // Check if we have real logic (more than just START->END)
    const hasOtherLogic = yamlData.flow.length > 1

    yamlData.flow.forEach((flowItem, index) => {
      // Filter out START->END if we have other logic (it causes visual clutter and is usually a legacy artifact)
      if (hasOtherLogic && flowItem.from === 'START' && flowItem.to === 'END') {
          return 
      }
      const from = flowItem.from
      
      if (flowItem.to) {
        // Simple connection
        edges.push({
          id: `e-${from}-${flowItem.to}-${index}`,
          source: from,
          target: flowItem.to,
          animated: true,
          style: { stroke: '#805AD5', strokeWidth: 2 },
          type: 'editable',
          data: { points: savedEdges[`${from}->${flowItem.to}`] }
        })
      } else if (flowItem.edges && Array.isArray(flowItem.edges)) {
        // Conditional edges
        flowItem.edges.forEach((edge, edgeIndex) => {
          let label = ''
          if (edge.condition) {
            const match = edge.condition.match(/x\['(\w+)'\]\s*==\s*'([^']+)'/)
            if (match) {
              label = `${match[1]} == "${match[2]}"`
            } else {
              // Try newer format: str(x.get('var')) == 'val'
              const newMatch = edge.condition.match(/x\.get\('([^']+)'\).*?==\s*'([^']+)'/)
              if (newMatch) {
                label = `${newMatch[1]} == "${newMatch[2]}"`
              } else {
                label = edge.condition.replace(/lambda\s+x:\s*/, '').slice(0, 30)
              }
            }
          }
          
          edges.push({
            id: `e-${from}-${edge.to}-${index}-${edgeIndex}`,
            source: from,
            target: edge.to,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'editable',
            label: label,
            labelStyle: { fill: '#9CA3AF', fontSize: 10 },
            labelBgStyle: { fill: '#1a1a2e', fillOpacity: 0.8 },
            labelBgPadding: [4, 2],
            data: { 
              points: savedEdges[`${from}->${edge.to}`],
              condition: edge.condition || ''
            }
          })
        })
      }
    })
  }
  
  return edges
}

/**
 * Calculate node dimensions for ELK layout
 * Using fixed width for consistent vertical alignment
 */
function getNodeDimensions(label) {
  // Fixed width for consistent vertical alignment
  // Nodes in React Flow can still display full labels with CSS overflow
  const width = 180
  const height = 50
  return { width, height }
}

/**
 * Auto-layout nodes using ELKjs (top-to-bottom, orthogonal routing)
 * @param {Array} nodes - React Flow nodes
 * @param {Array} edges - React Flow edges
 * @returns {Promise<Array>} Nodes with updated positions
 */
export async function autoLayoutAsync(nodes, edges) {
  const graph = {
    id: 'root',
    layoutOptions: elkOptions,
    children: nodes.map((node) => {
      const { width, height } = getNodeDimensions(node.data?.label || node.id)
      return {
        id: node.id,
        width,
        height,
      }
    }),
    edges: edges.map((edge) => ({
      id: edge.id,
      sources: [edge.source],
      targets: [edge.target],
    })),
  }

  try {
    const layoutedGraph = await elk.layout(graph)
    
    return nodes.map((node) => {
      const elkNode = layoutedGraph.children.find((n) => n.id === node.id)
      if (elkNode) {
        return {
          ...node,
          position: { x: elkNode.x, y: elkNode.y },
        }
      }
      return node
    })
  } catch (error) {
    console.error('ELK layout error:', error)
    return nodes.map((node, index) => ({
      ...node,
      position: { x: 100, y: index * 100 },
    }))
  }
}

/**
 * Synchronous auto-layout (fallback, uses simple vertical stacking)
 */
export function autoLayout(nodes, edges) {
  let y = 0
  const spacing = 100
  
  return nodes.map((node) => {
    const { height } = getNodeDimensions(node.data?.label || node.id)
    const position = { x: 200, y }
    y += height + spacing
    return { ...node, position }
  })
}

/**
 * Convert YAML to React Flow nodes and edges with auto-layout (async version)
 * Respects saved layout if present in YAML
 * @param {Object} yamlData - Parsed YAML object
 * @returns {Promise<Object>} { nodes, edges }
 */
export async function yamlToFlowAsync(yamlData) {
  if (!yamlData) {
    return { nodes: [], edges: [] }
  }
  
  const savedLayout = yamlData.layout || null
  const hasLayout = savedLayout && savedLayout.nodes && Object.keys(savedLayout.nodes).length > 0
  
  // Parse nodes with saved positions if available
  const nodes = parseNodes(yamlData, savedLayout)
  
  // Build nodePositions map for handle calculation
  const nodePositions = {}
  nodes.forEach(n => { nodePositions[n.id] = n.position })
  
  // Parse edges
  const edges = parseEdges(yamlData, nodePositions, savedLayout?.edges)
  
  let finalNodes
  if (hasLayout) {
    // Use saved positions - no auto-layout needed
    finalNodes = [...nodes]
  } else {
    // No saved layout - use ELKjs auto-layout
    finalNodes = await autoLayoutAsync(nodes, edges)
  }
  
  return {
    nodes: finalNodes,
    edges,
  }
}

/**
 * Convert YAML to React Flow nodes and edges (sync version)
 */
export function yamlToFlow(yamlData) {
  if (!yamlData) {
    return { nodes: [], edges: [] }
  }
  
  const savedLayout = yamlData.layout || null
  const hasLayout = savedLayout && savedLayout.nodes && Object.keys(savedLayout.nodes).length > 0
  
  const nodes = parseNodes(yamlData, savedLayout)
  
  // Build nodePositions map for handle calculation
  const nodePositions = {}
  nodes.forEach(n => { nodePositions[n.id] = n.position })
  
  const edges = parseEdges(yamlData, nodePositions, savedLayout?.edges)
  
  let finalNodes
  if (hasLayout) {
    finalNodes = [...nodes]
  } else {
    finalNodes = autoLayout(nodes, edges)
  }
  
  return {
    nodes: finalNodes,
    edges,
  }
}

/**
 * Extract layout from nodes and edges for saving
 */
export function extractLayout(nodes, edges) {
  const layout = {
    nodes: {},
    edges: {}
  }
  
  // Extract regular node positions
  nodes.forEach((node) => {
    if (node.type !== 'waypoint') {
      layout.nodes[node.id] = {
        x: Math.round(node.position.x),
        y: Math.round(node.position.y)
      }
    }
  })

  // Extract edge points
  edges.forEach((edge) => {
    if (edge.data?.points && Array.isArray(edge.data.points) && edge.data.points.length > 0) {
      layout.edges[`${edge.source}->${edge.target}`] = edge.data.points.map(p => ({
        x: Math.round(p.x),
        y: Math.round(p.y)
      }))
    }
  })
  
  return layout
}
