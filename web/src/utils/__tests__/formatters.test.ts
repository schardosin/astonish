import { describe, it, expect } from 'vitest'
import { snakeToTitleCase } from '../formatters'

describe('snakeToTitleCase', () => {
  it('converts simple snake_case to Title Case', () => {
    expect(snakeToTitleCase('github_pr_generator')).toBe('Github Pr Generator')
  })

  it('handles single word', () => {
    expect(snakeToTitleCase('hello')).toBe('Hello')
  })

  it('handles empty string', () => {
    expect(snakeToTitleCase('')).toBe('')
  })

  it('handles uppercase input', () => {
    expect(snakeToTitleCase('HELLO_WORLD')).toBe('Hello World')
  })

  it('handles mixed case input', () => {
    expect(snakeToTitleCase('hElLo_WoRlD')).toBe('Hello World')
  })

  it('handles multiple underscores', () => {
    expect(snakeToTitleCase('one_two_three_four')).toBe('One Two Three Four')
  })
})
