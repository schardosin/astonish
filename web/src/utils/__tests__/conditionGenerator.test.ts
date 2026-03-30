import { describe, it, expect } from 'vitest'
import {
  generateLambda,
  parseLambda,
  createEmptyRule,
  OPERATORS,
  LOGIC_OPERATORS,
  type ConditionRule,
} from '../conditionGenerator'

describe('OPERATORS', () => {
  it('exports 9 operators', () => {
    expect(OPERATORS).toHaveLength(9)
  })

  it('includes standard comparison operators', () => {
    const values = OPERATORS.map(o => o.value)
    expect(values).toContain('==')
    expect(values).toContain('!=')
    expect(values).toContain('>')
    expect(values).toContain('<')
    expect(values).toContain('>=')
    expect(values).toContain('<=')
  })

  it('includes string operators', () => {
    const values = OPERATORS.map(o => o.value)
    expect(values).toContain('contains')
    expect(values).toContain('starts_with')
    expect(values).toContain('ends_with')
  })
})

describe('LOGIC_OPERATORS', () => {
  it('exports and/or', () => {
    expect(LOGIC_OPERATORS).toHaveLength(2)
    expect(LOGIC_OPERATORS.map(o => o.value)).toEqual(['and', 'or'])
  })
})

describe('createEmptyRule', () => {
  it('returns a rule with empty variable, == operator, and empty value', () => {
    const rule = createEmptyRule()
    expect(rule).toEqual({ variable: '', operator: '==', value: '' })
  })

  it('returns a new object each time', () => {
    const a = createEmptyRule()
    const b = createEmptyRule()
    expect(a).not.toBe(b)
  })
})

describe('generateLambda', () => {
  it('returns empty string for empty rules', () => {
    expect(generateLambda([])).toBe('')
  })

  it('returns empty string for null/undefined rules', () => {
    expect(generateLambda(null as unknown as ConditionRule[])).toBe('')
  })

  it('generates simple equality condition', () => {
    const rules: ConditionRule[] = [{ variable: 'status', operator: '==', value: 'done' }]
    expect(generateLambda(rules)).toBe('lambda x: x["status"] == "done"')
  })

  it('generates not-equals condition', () => {
    const rules: ConditionRule[] = [{ variable: 'status', operator: '!=', value: 'error' }]
    expect(generateLambda(rules)).toBe('lambda x: x["status"] != "error"')
  })

  it('generates comparison operators', () => {
    const rules: ConditionRule[] = [{ variable: 'count', operator: '>', value: '5' }]
    expect(generateLambda(rules)).toBe('lambda x: x["count"] > "5"')
  })

  it('generates contains condition', () => {
    const rules: ConditionRule[] = [{ variable: 'text', operator: 'contains', value: 'hello' }]
    expect(generateLambda(rules)).toBe('lambda x: "hello" in str(x["text"])')
  })

  it('generates starts_with condition', () => {
    const rules: ConditionRule[] = [{ variable: 'name', operator: 'starts_with', value: 'pre' }]
    expect(generateLambda(rules)).toBe('lambda x: str(x["name"]).startswith("pre")')
  })

  it('generates ends_with condition', () => {
    const rules: ConditionRule[] = [{ variable: 'file', operator: 'ends_with', value: '.txt' }]
    expect(generateLambda(rules)).toBe('lambda x: str(x["file"]).endswith(".txt")')
  })

  it('joins multiple rules with AND by default', () => {
    const rules: ConditionRule[] = [
      { variable: 'a', operator: '==', value: '1' },
      { variable: 'b', operator: '!=', value: '2' },
    ]
    expect(generateLambda(rules)).toBe('lambda x: x["a"] == "1" and x["b"] != "2"')
  })

  it('joins multiple rules with OR when specified', () => {
    const rules: ConditionRule[] = [
      { variable: 'a', operator: '==', value: '1' },
      { variable: 'b', operator: '==', value: '2' },
    ]
    expect(generateLambda(rules, 'or')).toBe('lambda x: x["a"] == "1" or x["b"] == "2"')
  })

  it('skips rules with missing variable', () => {
    const rules: ConditionRule[] = [
      { variable: '', operator: '==', value: '1' },
      { variable: 'b', operator: '==', value: '2' },
    ]
    expect(generateLambda(rules)).toBe('lambda x: x["b"] == "2"')
  })

  it('returns empty string when all rules are invalid', () => {
    const rules: ConditionRule[] = [
      { variable: '', operator: '==', value: '1' },
      { variable: '', operator: '!=', value: '2' },
    ]
    expect(generateLambda(rules)).toBe('')
  })
})

describe('parseLambda', () => {
  it('returns null for empty string', () => {
    expect(parseLambda('')).toBeNull()
  })

  it('returns null for non-lambda string', () => {
    expect(parseLambda('not a lambda')).toBeNull()
  })

  it('returns null for null/undefined', () => {
    expect(parseLambda(null as unknown as string)).toBeNull()
    expect(parseLambda(undefined as unknown as string)).toBeNull()
  })

  it('parses x["var"] == "value" format', () => {
    const result = parseLambda('lambda x: x["status"] == "done"')
    expect(result).toEqual({
      rules: [{ variable: 'status', operator: '==', value: 'done' }],
      logic: 'and',
    })
  })

  it('parses x.get() format with str()', () => {
    const result = parseLambda("lambda x: str(x.get('status')) == 'done'")
    expect(result).toEqual({
      rules: [{ variable: 'status', operator: '==', value: 'done' }],
      logic: 'and',
    })
  })

  it('parses x.get() simple format', () => {
    const result = parseLambda("lambda x: x.get('count') > 5")
    expect(result).toEqual({
      rules: [{ variable: 'count', operator: '>', value: '5' }],
      logic: 'and',
    })
  })

  it('parses contains pattern', () => {
    const result = parseLambda("lambda x: 'hello' in str(x.get('text'))")
    expect(result).toEqual({
      rules: [{ variable: 'text', operator: 'contains', value: 'hello' }],
      logic: 'and',
    })
  })

  it('parses startswith pattern', () => {
    const result = parseLambda("lambda x: str(x.get('name')).startswith('pre')")
    expect(result).toEqual({
      rules: [{ variable: 'name', operator: 'starts_with', value: 'pre' }],
      logic: 'and',
    })
  })

  it('parses endswith pattern', () => {
    const result = parseLambda("lambda x: str(x.get('file')).endswith('.txt')")
    expect(result).toEqual({
      rules: [{ variable: 'file', operator: 'ends_with', value: '.txt' }],
      logic: 'and',
    })
  })

  it('parses multiple AND conditions', () => {
    const result = parseLambda("lambda x: x.get('a') == 'x' and x.get('b') != 'y'")
    expect(result).not.toBeNull()
    expect(result!.logic).toBe('and')
    expect(result!.rules).toHaveLength(2)
  })

  it('parses multiple OR conditions', () => {
    const result = parseLambda("lambda x: x.get('a') == 'x' or x.get('b') == 'y'")
    expect(result).not.toBeNull()
    expect(result!.logic).toBe('or')
    expect(result!.rules).toHaveLength(2)
  })

  it('parses legacy x["key"] format', () => {
    const result = parseLambda("lambda x: x['status'] == 'active'")
    expect(result).toEqual({
      rules: [{ variable: 'status', operator: '==', value: 'active' }],
      logic: 'and',
    })
  })

  it('returns null for unparsable body', () => {
    expect(parseLambda('lambda x: some_random_function(x)')).toBeNull()
  })
})

describe('generateLambda + parseLambda roundtrip', () => {
  it('roundtrips a simple equality rule', () => {
    const rules: ConditionRule[] = [{ variable: 'status', operator: '==', value: 'done' }]
    const lambda = generateLambda(rules)
    const parsed = parseLambda(lambda)
    expect(parsed).not.toBeNull()
    expect(parsed!.rules).toHaveLength(1)
    expect(parsed!.rules[0].variable).toBe('status')
    expect(parsed!.rules[0].operator).toBe('==')
    expect(parsed!.rules[0].value).toBe('done')
  })
})
