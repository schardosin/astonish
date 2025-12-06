import dagre from 'dagre'

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
          type: 'smoothstep',
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
 * Auto-layout nodes using Dagre (left-to-right)
 * @param {Array} nodes - React Flow nodes
 * @param {Array} edges - React Flow edges
 * @returns {Array} Nodes with updated positions
 */
export function autoLayout(nodes, edges) {
  const dagreGraph = new dagre.graphlib.Graph()
  dagreGraph.setDefaultEdgeLabel(() => ({}))
  
  // Calculate max node width based on label lengths
  const getNodeWidth = (label) => {
    const baseWidth = 120
    const charWidth = 8 // approximate pixels per character
    return Math.max(baseWidth, label.length * charWidth + 60)
  }
  
  // Configure for left-to-right layout
  dagreGraph.setGraph({
    rankdir: 'LR', // Left to Right
    nodesep: 60,   // Vertical spacing between nodes in same rank
    ranksep: 100,  // Horizontal spacing between ranks
    marginx: 50,
    marginy: 50,
  })
  
  // Node height
  const nodeHeight = 60
  
  // Add nodes to Dagre with dynamic widths
  nodes.forEach((node) => {
    const nodeWidth = getNodeWidth(node.data?.label || node.id)
    dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight })
  })
  
  // Add edges to Dagre
  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target)
  })
  
  // Run layout algorithm
  dagre.layout(dagreGraph)
  
  // Update node positions
  return nodes.map((node) => {
    const dagreNode = dagreGraph.node(node.id)
    const nodeWidth = getNodeWidth(node.data?.label || node.id)
    return {
      ...node,
      position: {
        x: dagreNode.x - nodeWidth / 2,
        y: dagreNode.y - nodeHeight / 2,
      },
    }
  })
}

/**
 * Convert YAML to React Flow nodes and edges with auto-layout
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
