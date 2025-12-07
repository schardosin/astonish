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
  'elk.layered.spacing.nodeNodeBetweenLayers': '100', // Vertical gap between layers
  'elk.spacing.nodeNode': '60', // Horizontal gap between parallel nodes
  'elk.edgeRouting': 'ORTHOGONAL', // Clean 90° edges
  'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF', // Better for linear flows
  'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
}

/**
 * Parse nodes from YAML agent config
 * @param {Object} yamlData - Parsed YAML object
 * @returns {Array} React Flow nodes
 */
export function parseNodes(yamlData) {
  const nodes = []
  
  // Add START node
  nodes.push({
    id: 'START',
    type: 'start',
    position: { x: 0, y: 0 },
    data: { label: 'START' }
  })
  
  // Parse defined nodes
  if (yamlData.nodes && Array.isArray(yamlData.nodes)) {
    yamlData.nodes.forEach((node) => {
      const nodeType = NODE_TYPE_MAP[node.type] || 'llm'
      nodes.push({
        id: node.name,
        type: nodeType,
        position: { x: 0, y: 0 }, // Will be set by auto-layout
        data: {
          label: node.name,
          nodeType: node.type,
          // Store original YAML data for later use
          yaml: node
        }
      })
    })
  }
  
  // Add END node
  nodes.push({
    id: 'END',
    type: 'end',
    position: { x: 0, y: 0 },
    data: { label: 'END' }
  })
  
  return nodes
}

/**
 * Parse edges from YAML flow config
 * @param {Object} yamlData - Parsed YAML object
 * @returns {Array} React Flow edges
 */
export function parseEdges(yamlData) {
  const edges = []
  
  if (yamlData.flow && Array.isArray(yamlData.flow)) {
    yamlData.flow.forEach((flowItem, index) => {
      const from = flowItem.from
      
      if (flowItem.to) {
        // Simple edge: from -> to
        edges.push({
          id: `e-${from}-${flowItem.to}-${index}`,
          source: from,
          target: flowItem.to,
          animated: true,
          style: { stroke: '#805AD5', strokeWidth: 2 },
          type: 'smoothstep', // Works well with orthogonal routing
        })
      } else if (flowItem.edges && Array.isArray(flowItem.edges)) {
        // Conditional edges
        flowItem.edges.forEach((edge, edgeIndex) => {
          // Extract condition label (simplify lambda expressions)
          let label = ''
          if (edge.condition) {
            // Try to extract the readable part from lambda
            const match = edge.condition.match(/x\['(\w+)'\]\s*==\s*'([^']+)'/)
            if (match) {
              label = `${match[1]} = "${match[2]}"`
            } else {
              label = edge.condition.replace(/lambda\s+x:\s*/, '').slice(0, 30)
            }
          }
          
          edges.push({
            id: `e-${from}-${edge.to}-${index}-${edgeIndex}`,
            source: from,
            target: edge.to,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'smoothstep',
            label: label,
            labelStyle: { fill: '#9CA3AF', fontSize: 10 },
            labelBgStyle: { fill: '#1a1a2e', fillOpacity: 0.8 },
            labelBgPadding: [4, 2],
          })
        })
      }
    })
  }
  
  return edges
}

/**
 * Calculate node dimensions based on label
 */
function getNodeDimensions(label) {
  const baseWidth = 120
  const charWidth = 8
  const width = Math.max(baseWidth, label.length * charWidth + 60)
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
  // Build ELK graph structure
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
    
    // Map layout positions back to React Flow nodes
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
    // Fallback: return nodes with basic vertical positioning
    return nodes.map((node, index) => ({
      ...node,
      position: { x: 100, y: index * 100 },
    }))
  }
}

/**
 * Synchronous auto-layout (fallback, uses simple vertical stacking)
 * @param {Array} nodes - React Flow nodes
 * @param {Array} edges - React Flow edges
 * @returns {Array} Nodes with updated positions
 */
export function autoLayout(nodes, edges) {
  // Simple vertical layout as fallback for sync usage
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
 * @param {Object} yamlData - Parsed YAML object
 * @returns {Promise<Object>} { nodes, edges }
 */
export async function yamlToFlowAsync(yamlData) {
  if (!yamlData) {
    return { nodes: [], edges: [] }
  }
  
  const nodes = parseNodes(yamlData)
  const edges = parseEdges(yamlData)
  const layoutedNodes = await autoLayoutAsync(nodes, edges)
  
  return {
    nodes: layoutedNodes,
    edges,
  }
}

/**
 * Convert YAML to React Flow nodes and edges with auto-layout (sync version with basic layout)
 * @param {Object} yamlData - Parsed YAML object
 * @returns {Object} { nodes, edges }
 */
export function yamlToFlow(yamlData) {
  if (!yamlData) {
    return { nodes: [], edges: [] }
  }
  
  const nodes = parseNodes(yamlData)
  const edges = parseEdges(yamlData)
  const layoutedNodes = autoLayout(nodes, edges)
  
  return {
    nodes: layoutedNodes,
    edges,
  }
}
