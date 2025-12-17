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
        
        // prompt is required for input nodes
        if (node.type === 'input' && !node.prompt) {
          validationErrors.push(`'prompt' is required for input nodes to show the user what to enter`)
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
  
    // user_message is required for output nodes and should be an array
    // ... validation continues ...
  
  // Add END node
  const endPos = nodePositions['END']
  if (!endPos) {
    // Silent default
  }
    
  nodes.push({
    id: 'END',
    type: 'end',
    // Default Y to 300 to avoid overlapping START if layout missing
    position: endPos ? { x: endPos.x, y: endPos.y } : { x: 0, y: 300 },
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
    
    // For each edge, order waypoints by chain topology using immediateSource/immediateTarget
    // This preserves the actual routing order instead of just Y-sorting
    Object.keys(waypointEdgeMap).forEach(edgeKey => {
      const waypoints = waypointEdgeMap[edgeKey]
      if (waypoints.length <= 1) return  // No need to sort single waypoint
      
      // Check if we have immediateSource/immediateTarget info (new format)
      const hasChainInfo = waypoints.some(wp => wp.immediateSource || wp.immediateTarget)
      
      if (hasChainInfo) {
        // Build chain using topology
        const [originalSource] = edgeKey.split('->')
        const wpById = new Map(waypoints.map(wp => [wp.id, wp]))
        const sorted = []
        
        // Find first waypoint in chain (one whose immediateSource is the original source)
        let current = waypoints.find(wp => wp.immediateSource === originalSource)
        
        if (current) {
          sorted.push(current)
          // Follow the chain
          while (sorted.length < waypoints.length) {
            const next = waypoints.find(wp => wp.immediateSource === current.id)
            if (next) {
              sorted.push(next)
              current = next
            } else {
              break
            }
          }
        }
        
        // If we successfully built the chain, use it; otherwise fall back to Y-sort
        if (sorted.length === waypoints.length) {
          waypointEdgeMap[edgeKey] = sorted
        } else {
          // Fallback: sort by Y
          waypointEdgeMap[edgeKey] = [...waypoints].sort((a, b) => a.y - b.y)
        }
      } else {
        // Old format without chain info - sort by Y (legacy behavior)
        waypointEdgeMap[edgeKey] = [...waypoints].sort((a, b) => a.y - b.y)
      }
    })
  }
  
  return { waypointNodes, waypointEdgeMap }
}

/**
 * Parse edges from YAML flow config, considering waypoints
 * @param {Object} yamlData - Parsed YAML object
 * @param {Object} waypointEdgeMap - Map of edges that have waypoints
 * @param {Object} nodePositions - Map of node ID to position { x, y }
 * @returns {Array} React Flow edges
 */
export function parseEdges(yamlData, waypointEdgeMap = {}, nodePositions = {}, savedEdges = {}) {
  const edges = []
  
  // Helper to determine handles based on relative Y positions
  // Waypoints use different handle IDs than regular nodes
  const isWaypoint = (id) => id.startsWith('waypoint-')
  
  const getHandles = (sourceId, targetId) => {
    // Standard handles for all directions - simplest layout
    return { 
      sourceHandle: isWaypoint(sourceId) ? 'bottom-source' : null, 
      targetHandle: isWaypoint(targetId) ? 'top-target' : null 
    }
  }
  
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
        const edgeKey = `${from}->${flowItem.to}`
        const waypoints = waypointEdgeMap[edgeKey]
        
        if (waypoints && waypoints.length > 0) {
          // Edge has waypoints - they are pre-sorted by chain topology in parseWaypoints
          const sortedWps = waypoints  // Already sorted!
          
          // Get target node Y for handle calculation
          const targetY = nodePositions[flowItem.to]?.y ?? 0
          const sourceY = nodePositions[from]?.y ?? 0
          
          // Create edges: source -> wp1 -> wp2 -> ... -> target
          let prevNodeId = from
          let prevY = sourceY
          
          sortedWps.forEach((wp, wpIndex) => {
            const isFirstEdge = wpIndex === 0
            const wpY = wp.y
            
            // Compute handles based on relative Y positions
            // If prev is below current (prevY > wpY), it's a back-edge going UP
            let srcHandle = null
            let tgtHandle = null
            
            if (isFirstEdge) {
              // First edge: from original source to first waypoint
              if (prevY > wpY) {
                // Source below waypoint - back-edge
                srcHandle = 'top-source'
                tgtHandle = 'bottom-target'
              } else {
                // Normal flow
                srcHandle = null
                tgtHandle = 'top-target'
              }
            } else {
              // Edge from previous waypoint to this waypoint
              if (prevY > wpY) {
                // Previous WP below this WP - edge going UP
                srcHandle = 'top-source'
                tgtHandle = 'bottom-target'
              } else {
                // Normal flow
                srcHandle = 'bottom-source'
                tgtHandle = 'top-target'
              }
            }
            
            edges.push({
              id: `e-${prevNodeId}-${wp.id}-${index}-${wpIndex}`,
              source: prevNodeId,
              target: wp.id,
              sourceHandle: srcHandle,
              targetHandle: tgtHandle,
              animated: true,
              style: { stroke: '#805AD5', strokeWidth: 2 },
              type: 'editable',
            })
            prevNodeId = wp.id
            prevY = wpY
          })
          
          // Final edge from last waypoint to target (a regular node)
          const lastWp = sortedWps[sortedWps.length - 1]
          const lastWpY = lastWp.y
          
          // Compute handles for final edge
          let finalSrcHandle = 'bottom-source'
          let finalTgtHandle = null  // Regular nodes use null for top target
          
          if (lastWpY > targetY) {
            // Last waypoint is BELOW target - back-edge going UP
            finalSrcHandle = 'top-source'
            finalTgtHandle = 'left'  // Regular nodes use 'left' for back-edges
          }
          
          edges.push({
            id: `e-${prevNodeId}-${flowItem.to}-${index}-final`,
            source: prevNodeId,
            target: flowItem.to,
            sourceHandle: finalSrcHandle,
            targetHandle: finalTgtHandle,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'editable',
          })
        } else {
          // No waypoints - normal edge
          const handles = getHandles(from, flowItem.to)
          edges.push({
            id: `e-${from}-${flowItem.to}-${index}`,
            source: from,
            target: flowItem.to,
            sourceHandle: handles.sourceHandle,
            targetHandle: handles.targetHandle,
            animated: true,
            style: { stroke: '#805AD5', strokeWidth: 2 },
            type: 'editable',
            data: { points: savedEdges[`${from}-${flowItem.to}`] || savedEdges[`${from}->${flowItem.to}`] }
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
            // Handle waypoints for conditional edges - pre-sorted by chain topology
            const sortedWps = waypoints  // Already sorted!
            
            // Get Y positions for handle calculation
            const targetY = nodePositions[edge.to]?.y ?? 0
            const sourceY = nodePositions[from]?.y ?? 0
            
            let prevNodeId = from
            let prevY = sourceY
            
            sortedWps.forEach((wp, wpIndex) => {
              const isFirstEdge = wpIndex === 0
              const wpY = wp.y
              
              // Compute handles based on relative Y positions
              let srcHandle = null
              let tgtHandle = null
              
              if (isFirstEdge) {
                if (prevY > wpY) {
                  srcHandle = 'top-source'
                  tgtHandle = 'bottom-target'
                } else {
                  srcHandle = null
                  tgtHandle = 'top-target'
                }
              } else {
                if (prevY > wpY) {
                  srcHandle = 'top-source'
                  tgtHandle = 'bottom-target'
                } else {
                  srcHandle = 'bottom-source'
                  tgtHandle = 'top-target'
                }
              }
              
              edges.push({
                id: `e-${prevNodeId}-${wp.id}-${index}-${edgeIndex}-${wpIndex}`,
                source: prevNodeId,
                target: wp.id,
                sourceHandle: srcHandle,
                targetHandle: tgtHandle,
                animated: true,
                style: { stroke: '#805AD5', strokeWidth: 2 },
                type: 'editable',
                label: wpIndex === 0 ? label : '',
                labelStyle: { fill: '#9CA3AF', fontSize: 10 },
                labelBgStyle: { fill: '#1a1a2e', fillOpacity: 0.8 },
                labelBgPadding: [4, 2],
              })
              prevNodeId = wp.id
              prevY = wpY
            })
            
            // Final edge from last waypoint to target
            const lastWp = sortedWps[sortedWps.length - 1]
            const lastWpY = lastWp.y
            
            let finalSrcHandle = 'bottom-source'
            let finalTgtHandle = null
            
            if (lastWpY > targetY) {
              finalSrcHandle = 'top-source'
              finalTgtHandle = 'left'
            }
            
            edges.push({
              id: `e-${prevNodeId}-${edge.to}-${index}-${edgeIndex}-final`,
              source: prevNodeId,
              target: edge.to,
              sourceHandle: finalSrcHandle,
              targetHandle: finalTgtHandle,
              animated: true,
              style: { stroke: '#805AD5', strokeWidth: 2 },
              type: 'editable',
            })
          } else {
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
              data: { points: savedEdges[`${from}->${edge.to}`] }
            })
          }
        })
      }
    })
  }
  
  if (edges.length === 0) {
      console.log('[yamlToFlow] No edges generated')
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
  
  // Build nodePositions map for handle calculation
  const nodePositions = {}
  nodes.forEach(n => { nodePositions[n.id] = n.position })
  waypointNodes.forEach(wp => { nodePositions[wp.id] = wp.position })
  
  // Parse edges considering waypoints and positions for handles
  const edges = parseEdges(yamlData, waypointEdgeMap, nodePositions, savedLayout?.edges)
  
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
  
  // Build nodePositions map for handle calculation
  const nodePositions = {}
  nodes.forEach(n => { nodePositions[n.id] = n.position })
  waypointNodes.forEach(wp => { nodePositions[wp.id] = wp.position })
  
  const edges = parseEdges(yamlData, waypointEdgeMap, nodePositions, savedLayout?.edges)
  
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
 * Extract layout from nodes and edges for saving
 */
export function extractLayout(nodes, edges) {
  const layout = {
    nodes: {},
    waypoints: []
  }
  
  // First, collect all waypoints to understand chains
  const waypointNodes = nodes.filter(n => n.type === 'waypoint')
  
  // For each waypoint, find its position in the chain
  waypointNodes.forEach((node) => {
    const incomingEdge = edges.find(e => e.target === node.id)
    const outgoingEdge = edges.find(e => e.source === node.id)
    
    if (incomingEdge && outgoingEdge) {
      // Find original source (first non-waypoint going backwards)
      let originalSource = incomingEdge.source
      let tempEdge = edges.find(e => e.target === originalSource)
      while (tempEdge && nodes.find(n => n.id === originalSource)?.type === 'waypoint') {
        originalSource = tempEdge.source
        tempEdge = edges.find(e => e.target === originalSource)
      }
      
      // Find original target (first non-waypoint going forwards)
      let originalTarget = outgoingEdge.target
      let tempEdge2 = edges.find(e => e.source === originalTarget)
      while (tempEdge2 && nodes.find(n => n.id === originalTarget)?.type === 'waypoint') {
        originalTarget = tempEdge2.target
        tempEdge2 = edges.find(e => e.source === originalTarget)
      }
      
      // Calculate sequence order based on Y position (lower Y = earlier in chain for top-down flows)
      // We'll compute this relative to other waypoints on the same original edge
      
      layout.waypoints.push({
        id: node.id,
        x: Math.round(node.position.x),
        y: Math.round(node.position.y),
        // Original edge endpoints (for grouping waypoints on the same edge)
        source: originalSource,
        target: originalTarget,
        // Actual immediate neighbors (for precise chain reconstruction)
        immediateSource: incomingEdge.source,
        immediateTarget: outgoingEdge.target,
        // Save edge handle info for exact restoration
        incomingSourceHandle: incomingEdge.sourceHandle || null,
        incomingTargetHandle: incomingEdge.targetHandle || null,
        outgoingSourceHandle: outgoingEdge.sourceHandle || null,
        outgoingTargetHandle: outgoingEdge.targetHandle || null,
      })
    }
  })
  
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
  layout.edges = {}
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
