/**
 * Condition Generator Utility
 * Converts visual condition rules to Python lambda expressions and vice versa.
 */

// --- Types ---

export interface Operator {
  value: string
  label: string
  python: string
}

export interface LogicOperator {
  value: string
  label: string
}

export interface ConditionRule {
  variable: string
  operator: string
  value: string
}

export interface ParsedLambda {
  rules: ConditionRule[]
  logic: string
}

// --- Constants ---

export const OPERATORS: Operator[] = [
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

export const LOGIC_OPERATORS: LogicOperator[] = [
  { value: 'and', label: 'AND' },
  { value: 'or', label: 'OR' },
]

// --- Functions ---

export function generateLambda(rules: ConditionRule[], logic: string = 'and'): string {
  if (!rules || rules.length === 0) {
    return ''
  }

  const conditions = rules
    .filter(rule => rule.variable && rule.operator && rule.value !== undefined)
    .map(rule => {
      const varAccess = `x["${rule.variable}"]`
      const quotedValue = `"${rule.value}"`

      switch (rule.operator) {
        case 'contains':
          return `"${rule.value}" in str(${varAccess})`
        case 'starts_with':
          return `str(${varAccess}).startswith("${rule.value}")`
        case 'ends_with':
          return `str(${varAccess}).endswith("${rule.value}")`
        default:
          return `${varAccess} ${rule.operator} ${quotedValue}`
      }
    })

  if (conditions.length === 0) {
    return ''
  }

  const joined = conditions.join(` ${logic} `)
  return `lambda x: ${joined}`
}

export function parseLambda(lambdaStr: string): ParsedLambda | null {
  if (!lambdaStr || typeof lambdaStr !== 'string') {
    return null
  }

  const match = lambdaStr.match(/^lambda\s+x:\s*(.+)$/)
  if (!match) {
    return null
  }

  const body = match[1].trim()

  let logic = 'and'
  let parts: string[] = []

  if (body.includes(' or ')) {
    logic = 'or'
    parts = body.split(' or ').map(p => p.trim())
  } else if (body.includes(' and ')) {
    logic = 'and'
    parts = body.split(' and ').map(p => p.trim())
  } else {
    parts = [body]
  }

  const rules: ConditionRule[] = []

  for (const part of parts) {
    const rule = parseConditionPart(part)
    if (rule) {
      rules.push(rule)
    } else {
      return null
    }
  }

  return { rules, logic }
}

function parseConditionPart(part: string): ConditionRule | null {
  const strEqualityQuotedMatch = part.match(/str\(x\.get\('([^']+)'\)\)\s*(==|!=)\s*'([^']*)'/)
  if (strEqualityQuotedMatch) {
    return {
      variable: strEqualityQuotedMatch[1],
      operator: strEqualityQuotedMatch[2],
      value: strEqualityQuotedMatch[3]
    }
  }

  const strEqualityUnquotedMatch = part.match(/str\(x\.get\('([^']+)'\)\)\s*(==|!=|>|<|>=|<=)\s*(\d+)/)
  if (strEqualityUnquotedMatch) {
    return {
      variable: strEqualityUnquotedMatch[1],
      operator: strEqualityUnquotedMatch[2],
      value: strEqualityUnquotedMatch[3]
    }
  }

  const simpleQuotedMatch = part.match(/x\.get\('([^']+)'\)\s*(==|!=|>|<|>=|<=)\s*'([^']*)'/)
  if (simpleQuotedMatch) {
    return {
      variable: simpleQuotedMatch[1],
      operator: simpleQuotedMatch[2],
      value: simpleQuotedMatch[3]
    }
  }

  const simpleUnquotedMatch = part.match(/x\.get\('([^']+)'\)\s*(==|!=|>|<|>=|<=)\s*(\d+)/)
  if (simpleUnquotedMatch) {
    return {
      variable: simpleUnquotedMatch[1],
      operator: simpleUnquotedMatch[2],
      value: simpleUnquotedMatch[3]
    }
  }

  const containsMatch = part.match(/'([^']+)'\s+in\s+str\(x\.get\('([^']+)'\)\)/)
  if (containsMatch) {
    return {
      variable: containsMatch[2],
      operator: 'contains',
      value: containsMatch[1]
    }
  }

  const startsWithMatch = part.match(/str\(x\.get\('([^']+)'\)\)\.startswith\('([^']+)'\)/)
  if (startsWithMatch) {
    return {
      variable: startsWithMatch[1],
      operator: 'starts_with',
      value: startsWithMatch[2]
    }
  }

  const endsWithMatch = part.match(/str\(x\.get\('([^']+)'\)\)\.endswith\('([^']+)'\)/)
  if (endsWithMatch) {
    return {
      variable: endsWithMatch[1],
      operator: 'ends_with',
      value: endsWithMatch[2]
    }
  }

  const doubleQuoteMatch = part.match(/x\["([^"]+)"\]\s*(==|!=|>|<|>=|<=)\s*"([^"]*)"/)
  if (doubleQuoteMatch) {
    return {
      variable: doubleQuoteMatch[1],
      operator: doubleQuoteMatch[2],
      value: doubleQuoteMatch[3]
    }
  }

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

export function createEmptyRule(): ConditionRule {
  return {
    variable: '',
    operator: '==',
    value: ''
  }
}
