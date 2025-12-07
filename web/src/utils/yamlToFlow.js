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
  
  // Parse defined nodes
  if (yamlData.nodes && Array.isArray(yamlData.nodes)) {
    yamlData.nodes.forEach((node) => {
      const nodeType = NODE_TYPE_MAP[node.type] || 'llm'
      const savedPos = nodePositions[node.name]
      nodes.push({
        id: node.name,
        type: nodeType,
        position: savedPos ? { x: savedPos.x, y: savedPos.y } : { x: 0, y: 0 },
        data: {
          label: node.name,
          nodeType: node.type,
          yaml: node
        }
      })
    })
  }
  
  // Add END node
  const endPos = nodePositions['END']
  nodes.push({
    id: 'END',
    type: 'end',
    position: endPos ? { x: endPos.x, y: endPos.y } : { x: 0, y: 0 },
    data: { label: 'END' }
  })
  
  return nodes
}

/**
 * Parse waypoints from saved layout
 * @param {Object} savedLayout - Saved layout with waypoints
 * @returns {Object} { waypointNodes, waypointEdgeMap }
 */
function parseWaypoints(savedLayout) {
  const waypointNodes = []
  const waypointEdgeMap = {} // Maps "source->target" to array of waypoints
  
  if (savedLayout?.waypoints && Array.isArray(savedLayout.waypoints)) {
    savedLayout.waypoints.forEach((wp) => {
      waypointNodes.push({
        id: wp.id,
        type: 'waypoint',
        position: { x: wp.x, y: wp.y },
        data: { label: '' },
        draggable: true,
      })
      
      // Track which original edge this waypoint belongs to
      const edgeKey = `${wp.source}->${wp.target}`
      if (!waypointEdgeMap[edgeKey]) {
        waypointEdgeMap[edgeKey] = []
      }
      waypointEdgeMap[edgeKey].push(wp)
    })
  }
  
  return { waypointNodes, waypointEdgeMap }
}

/**
 * Parse edges from YAML flow config, considering waypoints
 * @param {Object} yamlData - Parsed YAML object
 * @param {Object} waypointEdgeMap - Map of edges that have waypoints
 * @returns {Array} React Flow edges
 */
export function parseEdges(yamlData, waypointEdgeMap = {}) {
  const edges = []
  
  if (yamlData.flow && Array.isArray(yamlData.flow)) {
    yamlData.flow.forEach((flowItem, index) => {
      const from = flowItem.from
      
      if (flowItem.to) {
        const edgeKey = `${from}->${flowItem.to}`
        const waypoints = waypointEdgeMap[edgeKey]
        
        if (waypoints && waypoints.length > 0) {
          // Edge has waypoints - create split edges
          // Sort waypoints by their order (assuming single waypoint for now)
          const sortedWps = [...waypoints].sort((a, b) => a.y - b.y)
          
          // Create edges: source -> wp1 -> wp2 -> ... -> target
          let prevNodeId = from
          sortedWps.forEach((wp, wpIndex) => {
            edges.push({
              id: `e-${prevNodeId}-${wp.id}-${index}-${wpIndex}`,
              source: prevNodeId,
              target: wp.id,
              animated: true,
              style: { stroke: '#805AD5', strokeWidth: 2 },
              type: 'smoothstep',
            })
            prevNodeId = wp.id
          })
          
          // Final edge from last waypoint to target
          edges.push({
            id: `e-${prevNodeId}-${flowItem.to}-${index}-final`,
            source: prevNodeId,
            target: flowItem.to,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'smoothstep',
          })
        } else {
          // No waypoints - normal edge
          edges.push({
            id: `e-${from}-${flowItem.to}-${index}`,
            source: from,
            target: flowItem.to,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'smoothstep',
          })
        }
      } else if (flowItem.edges && Array.isArray(flowItem.edges)) {
        // Conditional edges
        flowItem.edges.forEach((edge, edgeIndex) => {
          let label = ''
          if (edge.condition) {
            const match = edge.condition.match(/x\['(\w+)'\]\s*==\s*'([^']+)'/)
            if (match) {
              label = `${match[1]} = "${match[2]}"`
            } else {
              label = edge.condition.replace(/lambda\s+x:\s*/, '').slice(0, 30)
            }
          }
          
          const edgeKey = `${from}->${edge.to}`
          const waypoints = waypointEdgeMap[edgeKey]
          
          if (waypoints && waypoints.length > 0) {
            // Handle waypoints for conditional edges
            const sortedWps = [...waypoints].sort((a, b) => a.y - b.y)
            let prevNodeId = from
            
            sortedWps.forEach((wp, wpIndex) => {
              edges.push({
                id: `e-${prevNodeId}-${wp.id}-${index}-${edgeIndex}-${wpIndex}`,
                source: prevNodeId,
                target: wp.id,
                animated: true,
                style: { stroke: '#805AD5', strokeWidth: 2 },
                type: 'smoothstep',
                label: wpIndex === 0 ? label : '',
                labelStyle: { fill: '#9CA3AF', fontSize: 10 },
                labelBgStyle: { fill: '#1a1a2e', fillOpacity: 0.8 },
                labelBgPadding: [4, 2],
              })
              prevNodeId = wp.id
            })
            
            edges.push({
              id: `e-${prevNodeId}-${edge.to}-${index}-${edgeIndex}-final`,
              source: prevNodeId,
              target: edge.to,
              animated: true,
              style: { stroke: '#805AD5', strokeWidth: 2 },
              type: 'smoothstep',
            })
          } else {
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
          }
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
  
  // Parse waypoints from layout
  const { waypointNodes, waypointEdgeMap } = parseWaypoints(savedLayout)
  
  // Parse nodes with saved positions if available
  const nodes = parseNodes(yamlData, savedLayout)
  
  // Parse edges considering waypoints
  const edges = parseEdges(yamlData, waypointEdgeMap)
  
  let finalNodes
  if (hasLayout) {
    // Use saved positions - no auto-layout needed
    finalNodes = [...nodes, ...waypointNodes]
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
  
  const { waypointNodes, waypointEdgeMap } = parseWaypoints(savedLayout)
  const nodes = parseNodes(yamlData, savedLayout)
  const edges = parseEdges(yamlData, waypointEdgeMap)
  
  let finalNodes
  if (hasLayout) {
    finalNodes = [...nodes, ...waypointNodes]
  } else {
    finalNodes = autoLayout(nodes, edges)
  }
  
  return {
    nodes: finalNodes,
    edges,
  }
}

/**
 * Extract layout from current nodes and edges for saving to YAML
 * @param {Array} nodes - Current React Flow nodes
 * @param {Array} edges - Current React Flow edges
 * @returns {Object} Layout object for YAML
 */
export function extractLayout(nodes, edges) {
  const layout = {
    nodes: {},
    waypoints: []
  }
  
  // Extract node positions
  nodes.forEach((node) => {
    if (node.type === 'waypoint') {
      // Find the edges connected to this waypoint to determine original source/target
      const incomingEdge = edges.find(e => e.target === node.id)
      const outgoingEdge = edges.find(e => e.source === node.id)
      
      if (incomingEdge && outgoingEdge) {
        // Trace back to find original source (non-waypoint)
        let source = incomingEdge.source
        let prevEdge = edges.find(e => e.target === source)
        while (prevEdge && nodes.find(n => n.id === source)?.type === 'waypoint') {
          source = prevEdge.source
          prevEdge = edges.find(e => e.target === source)
        }
        
        // Trace forward to find original target (non-waypoint)
        let target = outgoingEdge.target
        let nextEdge = edges.find(e => e.source === target)
        while (nextEdge && nodes.find(n => n.id === target)?.type === 'waypoint') {
          target = nextEdge.target
          nextEdge = edges.find(e => e.source === target)
        }
        
        layout.waypoints.push({
          id: node.id,
          x: Math.round(node.position.x),
          y: Math.round(node.position.y),
          source: source,
          target: target
        })
      }
    } else {
      // Regular node - save position
      layout.nodes[node.id] = {
        x: Math.round(node.position.x),
        y: Math.round(node.position.y)
      }
    }
  })
  
  return layout
}
