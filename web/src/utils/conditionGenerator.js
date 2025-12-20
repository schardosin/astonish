/**
 * Condition Generator Utility
 * Converts visual condition rules to Python lambda expressions and vice versa.
 */

// Supported operators
export const OPERATORS = [
  { value: '==', label: 'equals', python: '==' },
  { value: '!=', label: 'not equals', python: '!=' },
  { value: '>', label: 'greater than', python: '>' },
  { value: '<', label: 'less than', python: '<' },
  { value: '>=', label: 'greater or equal', python: '>=' },
  { value: '<=', label: 'less or equal', python: '<=' },
  { value: 'contains', label: 'contains', python: 'in' },
  { value: 'starts_with', label: 'starts with', python: 'startswith' },
  { value: 'ends_with', label: 'ends with', python: 'endswith' },
]

// Logic operators for combining rules
export const LOGIC_OPERATORS = [
  { value: 'and', label: 'AND' },
  { value: 'or', label: 'OR' },
]

/**
 * Generate a Python lambda expression from visual rules.
 * @param {Array} rules - Array of rule objects: { variable, operator, value }
 * @param {string} logic - 'and' or 'or' for combining rules
 * @returns {string} Python lambda expression
 */
export function generateLambda(rules, logic = 'and') {
  if (!rules || rules.length === 0) {
    return ''
  }

  const conditions = rules
    .filter(rule => rule.variable && rule.operator && rule.value !== undefined)
    .map(rule => {
      // Use x["variable"] format for cleaner Python
      const varAccess = `x["${rule.variable}"]`
      // Always quote the value as a string
      const quotedValue = `"${rule.value}"`

      switch (rule.operator) {
        case 'contains':
          return `"${rule.value}" in str(${varAccess})`
        case 'starts_with':
          return `str(${varAccess}).startswith("${rule.value}")`
        case 'ends_with':
          return `str(${varAccess}).endswith("${rule.value}")`
        default:
          // Simple comparison with quoted value
          return `${varAccess} ${rule.operator} ${quotedValue}`
      }
    })

  if (conditions.length === 0) {
    return ''
  }

  const joined = conditions.join(` ${logic} `)
  return `lambda x: ${joined}`
}

/**
 * Attempt to parse a Python lambda expression back into visual rules.
 * This is best-effort - complex expressions will return null.
 * @param {string} lambdaStr - Python lambda string
 * @returns {Object|null} { rules: [...], logic: 'and'|'or' } or null if unparseable
 */
export function parseLambda(lambdaStr) {
  if (!lambdaStr || typeof lambdaStr !== 'string') {
    return null
  }

  // Remove "lambda x: " prefix
  const match = lambdaStr.match(/^lambda\s+x:\s*(.+)$/)
  if (!match) {
    return null
  }

  const body = match[1].trim()

  // Detect logic operator
  let logic = 'and'
  let parts = []

  if (body.includes(' or ')) {
    logic = 'or'
    parts = body.split(' or ').map(p => p.trim())
  } else if (body.includes(' and ')) {
    logic = 'and'
    parts = body.split(' and ').map(p => p.trim())
  } else {
    parts = [body]
  }

  const rules = []

  for (const part of parts) {
    const rule = parseConditionPart(part)
    if (rule) {
      rules.push(rule)
    } else {
      // If any part is unparseable, return null (fallback to advanced mode)
      return null
    }
  }

  return { rules, logic }
}

/**
 * Parse a single condition part.
 * @param {string} part - Single condition like "str(x.get('var')) == 'val'"
 * @returns {Object|null} { variable, operator, value } or null
 */
function parseConditionPart(part) {
  // Pattern: str(x.get('variable')) == 'value' (quoted)
  const strEqualityQuotedMatch = part.match(/str\(x\.get\('([^']+)'\)\)\s*(==|!=)\s*'([^']*)'/)
  if (strEqualityQuotedMatch) {
    return {
      variable: strEqualityQuotedMatch[1],
      operator: strEqualityQuotedMatch[2],
      value: strEqualityQuotedMatch[3]
    }
  }

  // Pattern: str(x.get('variable')) == value (unquoted - numeric)
  const strEqualityUnquotedMatch = part.match(/str\(x\.get\('([^']+)'\)\)\s*(==|!=|>|<|>=|<=)\s*(\d+)/)
  if (strEqualityUnquotedMatch) {
    return {
      variable: strEqualityUnquotedMatch[1],
      operator: strEqualityUnquotedMatch[2],
      value: strEqualityUnquotedMatch[3]
    }
  }

  // Pattern: x.get('variable') == 'value' (quoted)
  const simpleQuotedMatch = part.match(/x\.get\('([^']+)'\)\s*(==|!=|>|<|>=|<=)\s*'([^']*)'/)
  if (simpleQuotedMatch) {
    return {
      variable: simpleQuotedMatch[1],
      operator: simpleQuotedMatch[2],
      value: simpleQuotedMatch[3]
    }
  }

  // Pattern: x.get('variable') == value (unquoted - numeric)
  const simpleUnquotedMatch = part.match(/x\.get\('([^']+)'\)\s*(==|!=|>|<|>=|<=)\s*(\d+)/)
  if (simpleUnquotedMatch) {
    return {
      variable: simpleUnquotedMatch[1],
      operator: simpleUnquotedMatch[2],
      value: simpleUnquotedMatch[3]
    }
  }

  // Pattern: 'value' in str(x.get('variable'))
  const containsMatch = part.match(/'([^']+)'\s+in\s+str\(x\.get\('([^']+)'\)\)/)
  if (containsMatch) {
    return {
      variable: containsMatch[2],
      operator: 'contains',
      value: containsMatch[1]
    }
  }

  // Pattern: str(x.get('variable')).startswith('value')
  const startsWithMatch = part.match(/str\(x\.get\('([^']+)'\)\)\.startswith\('([^']+)'\)/)
  if (startsWithMatch) {
    return {
      variable: startsWithMatch[1],
      operator: 'starts_with',
      value: startsWithMatch[2]
    }
  }

  // Pattern: str(x.get('variable')).endswith('value')
  const endsWithMatch = part.match(/str\(x\.get\('([^']+)'\)\)\.endswith\('([^']+)'\)/)
  if (endsWithMatch) {
    return {
      variable: endsWithMatch[1],
      operator: 'ends_with',
      value: endsWithMatch[2]
    }
  }

  // Pattern: x["variable"] == "value" (new format with double quotes)
  const doubleQuoteMatch = part.match(/x\["([^"]+)"\]\s*(==|!=|>|<|>=|<=)\s*"([^"]*)"/)
  if (doubleQuoteMatch) {
    return {
      variable: doubleQuoteMatch[1],
      operator: doubleQuoteMatch[2],
      value: doubleQuoteMatch[3]
    }
  }

  // Legacy pattern: x['variable'] == 'value' (single quotes)
  const legacyMatch = part.match(/x\['([^']+)'\]\s*(==|!=)\s*'([^']*)'/)
  if (legacyMatch) {
    return {
      variable: legacyMatch[1],
      operator: legacyMatch[2],
      value: legacyMatch[3]
    }
  }

  return null
}

/**
 * Create an empty rule object.
 */
export function createEmptyRule() {
  return {
    variable: '',
    operator: '==',
    value: ''
  }
}
