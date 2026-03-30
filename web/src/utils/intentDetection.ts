// Intent detection utility functions for AI Chat

export interface ToolOffer {
  requirement: string
  isAskingUser: boolean
}

// Detect if AI response offers to help find tools (stores pending requirement)
// Returns the tool requirement if AI is offering help, null otherwise
export function detectToolOfferFromAI(text: string): ToolOffer | null {
  if (!text) return null
  const lower = text.toLowerCase()
  
  // Check if AI is offering to help find/install tools
  const offerPatterns = [
    'would you like me to help you find',
    'would you like me to help you install',
    'would you like me to find and install',
    'shall i search for',
    'shall i help you find',
    'want me to search',
    'want me to find',
  ]
  
  // Check if AI mentions missing tools AND offers help
  const missingPatterns = [
    'tools that are not currently installed',
    'tools that are not installed',
    'would need the following tools',
    'would need a tool for',
    'need a tool for',
    'not currently installed',
    'i would need',
  ]
  
  const isMissingMentioned = missingPatterns.some(p => lower.includes(p))
  const isOfferingHelp = offerPatterns.some(p => lower.includes(p))
  
  if (isMissingMentioned) {
    // Extract the tool requirement description
    const toolMatches: string[] = []
    
    // Pattern 1: Bullet points (- tool: for reason)
    const bulletPattern = /[-•*]\s*\*?\*?([^:\n*]{5,100})\*?\*?(?::\s*(?:for|to)\s+([^\n]+))?/gi
    let match
    while ((match = bulletPattern.exec(text)) !== null) {
      let desc = (match[1] + (match[2] ? ' ' + match[2] : '')).trim()
      const excludePatterns = ['would you', 'let me', 'i can', 'you can', 'please', 'strictly following']
      if (!excludePatterns.some(p => desc.toLowerCase().includes(p)) && desc.length > 5) {
        toolMatches.push(desc)
      }
    }
    
    // Pattern 2: Inline mentions like "I would need a tool for X"
    if (toolMatches.length === 0) {
      const inlinePatterns = [
        /(?:would need|need)\s+(?:a\s+)?tool\s+for\s+([^.(]+)/gi,
        /(?:would need|need)\s+(?:a\s+)?([^.]+?)\s+tool/gi,
        /tool\s+(?:for\s+)?(?:general\s+)?([^.(,]+?)(?:\s+that|\s+is|\.|,)/gi,
        /`([a-z-]+(?:-[a-z]+)*)`\s*(?:or similar|tool)?/gi,  // backtick tool names like `tavily-search`
      ]
      
      for (const pattern of inlinePatterns) {
        let inlineMatch
        while ((inlineMatch = pattern.exec(text)) !== null) {
          let desc = inlineMatch[1].trim()
          // Clean up and validate
          if (desc.length > 3 && desc.length < 100 && !desc.includes('available tools')) {
            toolMatches.push(desc)
          }
        }
      }
    }
    
    if (toolMatches.length > 0) {
      // Deduplicate and join
      const unique = [...new Set(toolMatches.map(t => t.toLowerCase()))]
      return {
        requirement: unique.join('; '),
        isAskingUser: isOfferingHelp
      }
    }
  }
  
  return null
}

// Detect if user is confirming they want help finding tools
// Now also detects late requests like "search for it" even without pendingToolRequirement
export function isUserConfirmingToolSearch(text: string) {
  const lower = text.toLowerCase().trim()
  const confirmPatterns = [
    'yes', 'yep', 'yeah', 'sure', 'ok', 'okay', 'please', 
    'go ahead', 'do it', 'yes please', 'find them', 'search for them',
    'install them', 'help me find', 'yes, help',
    // Late requests - user changed their mind
    'search for it', 'find it', 'check the store', 'check store', 
    'look for it', 'search the store', 'search store', 'install it',
    'changed my mind', 'i changed my mind', 'do it now', 'let\'s do it'
  ]
  return confirmPatterns.some(p => lower.startsWith(p) || lower === p || lower.includes(p))
}

// Detect if user is rejecting store results and wants internet search
export function isUserRequestingInternetSearch(text: string) {
  const lower = text.toLowerCase().trim()
  const rejectionPatterns = [
    // Direct rejection of results
    'not what i need', 'won\'t work', 'don\'t work', 'not right',
    'none of these', 'not any of', 'not the right', 'wrong tools',
    'not useful', 'not helpful', 'need something else',
    // Direct internet search requests
    'search online', 'search the internet', 'search internet',
    'look online', 'find online', 'search the web', 'web search',
    'search github', 'check online', 'check the internet'
  ]
  return rejectionPatterns.some(p => lower.includes(p))
}

// Detect if user is providing feedback to refine search results
// Returns the refinement hint if detected, null otherwise
export function detectSearchRefinementFeedback(text: string) {
  const lower = text.toLowerCase().trim()
  
  // Patterns that indicate user is providing refinement feedback
  const refinementPatterns = [
    /(?:try|look for|search for|find|check)\s+(?:the\s+)?(.+?)\s+(?:repo|repository|server|package|instead)/i,
    /(?:i believe|i think|maybe|try)\s+(.+?)\s+(?:has|have|is|might)/i,
    /(?:modelcontextprotocol|anthropic|github)\s*[\/]?\s*(.+)/i,
    /(?:better option|better server|the right one)\s+(?:is|called|named)\s+(.+)/i,
    /(?:search.*for|look.*for|try.*finding)\s+(.+)/i,
    /(.+?)\s+(?:is what i need|is the one|is better)/i
  ]
  
  for (const pattern of refinementPatterns) {
    const match = text.match(pattern)
    if (match && match[1]) {
      return match[1].trim()
    }
  }
  
  // If message contains specific terms and internet results are shown, use it as refinement
  const containsRefinementHints = 
    lower.includes('sqlite') || 
    lower.includes('modelcontextprotocol') ||
    lower.includes('anthropic') ||
    lower.includes('official') ||
    lower.includes('local') ||
    lower.includes('not cloud') ||
    lower.includes('better option')
  
  if (containsRefinementHints) {
    // Extract the main nouns/identifiers from the message
    return text.replace(/[^a-zA-Z0-9\s-]/g, ' ').trim()
  }
  
  return null
}

// Helper to detect GitHub/MCP URLs in text
export function detectMCPURL(text: string) {
  // Match GitHub URLs and npm package URLs
  const urlPatterns = [
    /https?:\/\/github\.com\/[\w-]+\/[\w.-]+/gi,
    /https?:\/\/www\.npmjs\.com\/package\/[@\w/-]+/gi,
  ]
  for (const pattern of urlPatterns) {
    const match = text.match(pattern)
    if (match) return match[0]
  }
  return null
}
